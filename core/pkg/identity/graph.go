package identity

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cestila/ConduitOmniChat/core/pkg/domain"
	"github.com/cestila/ConduitOmniChat/core/pkg/storage"
	"github.com/google/uuid"
)

// Graph coordina la vinculación, resolución, fusión y desacople de identidades multicanal.
type Graph struct {
	db *storage.DB
}

// NewGraph crea una instancia del motor de grafo de identidad.
func NewGraph(db *storage.DB) *Graph {
	return &Graph{db: db}
}

// Hints transporta identidades canónicas que el proveedor conoce del contacto y
// que no se pueden derivar del identificador remoto, como el teléfono de un
// usuario de Telegram. Los valores se normalizan antes de usarse.
type Hints struct {
	Phone string
	Email string
}

// ResolveOrLinkChannel busca un contacto existente con el remoteID normalizado o crea uno nuevo.
// El teléfono y el email canónicos mandan sobre el identificador del proveedor: si la
// identidad ya pertenece a otro contacto, ambos se fusionan en el dueño de la identidad.
func (g *Graph) ResolveOrLinkChannel(ctx context.Context, provider domain.Provider, rawRemoteID, handle, displayName, avatarURL string, hints Hints) (*domain.Contact, *domain.ContactChannel, error) {
	normRemoteID := NormalizeIdentifier(rawRemoteID)

	identifierEmail := NormalizeEmail(rawRemoteID)
	identifierPhone := PhoneFromIdentifier(rawRemoteID)
	handleEmail := NormalizeEmail(handle)
	hintEmail := NormalizeEmail(hints.Email)
	hintPhone := PhoneFromIdentifier(hints.Phone)
	canonicalEmail := firstNonEmpty(hintEmail, identifierEmail)
	canonicalPhone := firstNonEmpty(hintPhone, identifierPhone)

	// 1. Buscar si el canal ya está registrado
	existingChannel, err := g.db.GetChannelByProviderAndRemoteID(ctx, string(provider), normRemoteID)
	if err == nil && existingChannel != nil {
		contact, err := g.db.GetContactByID(ctx, existingChannel.ContactID)
		if err != nil {
			return nil, nil, fmt.Errorf("error obteniendo contacto existente: %w", err)
		}

		// 1.a La identidad puede aparecer después de creado el contacto: unirla ahora.
		contact, err = g.linkLateIdentity(ctx, contact, canonicalPhone, canonicalEmail)
		if err != nil {
			return nil, nil, err
		}
		existingChannel.ContactID = contact.ID

		contactUpdated := false
		// Si se recibe un mejor DisplayName que no sea solo el número de teléfono o ID, actualizarlo
		trimmedDisplay := strings.Trim(displayName, "'\" \t")
		trimmedContact := strings.Trim(contact.DisplayName, "'\" \t")
		if trimmedDisplay != "" && trimmedDisplay != rawRemoteID && trimmedDisplay != normRemoteID {
			if trimmedContact == "" || trimmedContact == normRemoteID || trimmedContact == rawRemoteID || trimmedContact == strings.Trim(rawRemoteID, "+") || trimmedContact == strings.Trim(normRemoteID, "+") {
				contact.DisplayName = trimmedDisplay
				contactUpdated = true
			}
		}
		if avatarURL != "" && contact.AvatarURL == "" {
			contact.AvatarURL = avatarURL
			contactUpdated = true
		}
		if contactUpdated {
			_ = g.db.SaveContact(ctx, contact)
		}
		if handle != "" && existingChannel.Handle == "" {
			existingChannel.Handle = handle
			_ = g.db.SaveChannel(ctx, existingChannel)
		}
		return contact, existingChannel, nil
	}

	// 2. Resolver la identidad canónica: teléfono y email mandan sobre el identificador del proveedor.
	contactID, err := g.matchExistingContact(ctx, normRemoteID, canonicalEmail, canonicalPhone, handleEmail)
	if err != nil {
		return nil, nil, err
	}

	if contactID == "" {
		// Crear nuevo contacto unificado con la identidad que ya conocemos
		contactID = uuid.New().String()
		now := time.Now()
		newContact := &domain.Contact{
			ID:          contactID,
			DisplayName: displayName,
			AvatarURL:   avatarURL,
			Email:       firstNonEmpty(canonicalEmail, handleEmail),
			Phone:       canonicalPhone,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		if err := g.db.SaveContact(ctx, newContact); err != nil {
			return nil, nil, fmt.Errorf("error creando nuevo contacto: %w", err)
		}
	} else if canonicalEmail != "" || canonicalPhone != "" {
		// Completar la identidad del contacto existente si le faltaba
		if c, err := g.db.GetContactByID(ctx, contactID); err == nil {
			changed := false
			if c.Email == "" && canonicalEmail != "" {
				c.Email = canonicalEmail
				changed = true
			}
			if c.Phone == "" && canonicalPhone != "" {
				c.Phone = canonicalPhone
				changed = true
			}
			if avatarURL != "" && c.AvatarURL == "" {
				c.AvatarURL = avatarURL
				changed = true
			}
			if changed {
				c.UpdatedAt = time.Now()
				_ = g.db.SaveContact(ctx, c)
			}
		}
	}

	// 3. Crear el nuevo punto de contacto asociado al contacto
	channel := &domain.ContactChannel{
		ID:          uuid.New().String(),
		ContactID:   contactID,
		Provider:    provider,
		RemoteID:    normRemoteID,
		Handle:      handle,
		IsPreferred: false,
	}

	if err := g.db.SaveChannel(ctx, channel); err != nil {
		return nil, nil, fmt.Errorf("error registrando canal en el contacto: %w", err)
	}

	contact, err := g.db.GetContactByID(ctx, contactID)
	if err != nil {
		return nil, nil, err
	}

	return contact, channel, nil
}

// linkLateIdentity completa la identidad de un contacto que ya tenía canal creado.
// Si la identidad pertenece a otro contacto, el dueño de la identidad pasa a ser el
// principal y el contacto actual se absorbe en él.
func (g *Graph) linkLateIdentity(ctx context.Context, contact *domain.Contact, phone, email string) (*domain.Contact, error) {
	if phone == "" && email == "" {
		return contact, nil
	}

	for {
		merged := false

		if phone != "" && contact.Phone == "" {
			if owner, err := g.db.FindContactByPhone(ctx, phone); err == nil && owner != nil && owner.ID != contact.ID {
				contact, err = g.MergeContacts(ctx, owner.ID, contact.ID)
				if err != nil {
					return nil, err
				}
				merged = true
			}
		}

		if email != "" && contact.Email == "" {
			if owner, err := g.db.FindContactByEmail(ctx, email); err == nil && owner != nil && owner.ID != contact.ID {
				contact, err = g.MergeContacts(ctx, owner.ID, contact.ID)
				if err != nil {
					return nil, err
				}
				merged = true
			}
		}

		if !merged {
			break
		}
	}

	changed := false
	if phone != "" && contact.Phone == "" {
		contact.Phone = phone
		changed = true
	}
	if email != "" && contact.Email == "" {
		contact.Email = email
		changed = true
	}
	if !changed {
		return contact, nil
	}

	contact.UpdatedAt = time.Now()
	if err := g.db.SaveContact(ctx, contact); err != nil {
		return nil, fmt.Errorf("error completando identidad del contacto: %w", err)
	}
	return g.db.GetContactByID(ctx, contact.ID)
}

// matchExistingContact busca un contacto que ya represente al mismo ser humano.
// Orden de precedencia: email canónico, teléfono canónico, email en el handle del
// canal e identificador idéntico en otro proveedor.
func (g *Graph) matchExistingContact(ctx context.Context, normRemoteID, email, phone, handleEmail string) (string, error) {
	if email != "" {
		if contact, err := g.db.FindContactByEmail(ctx, email); err == nil && contact != nil {
			return contact.ID, nil
		}
	}

	if phone != "" {
		if contact, err := g.db.FindContactByPhone(ctx, phone); err == nil && contact != nil {
			return contact.ID, nil
		}
	}

	if handleEmail != "" {
		if contact, err := g.db.FindContactByEmail(ctx, handleEmail); err == nil && contact != nil {
			return contact.ID, nil
		}
	}

	if matchingChannel, err := g.db.FindChannelByRemoteIDAcrossProviders(ctx, normRemoteID); err == nil && matchingChannel != nil {
		return matchingChannel.ContactID, nil
	}

	return "", nil
}

// firstNonEmpty devuelve el primer valor no vacío.
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// SetContactIdentity asigna email y teléfono canónicos a un contacto.
// Un email o teléfono ya asignado a otro contacto se rechaza para no duplicar identidades.
// Un valor vacío deja la identidad existente intacta.
func (g *Graph) SetContactIdentity(ctx context.Context, contactID, email, phone string) (*domain.Contact, error) {
	contact, err := g.db.GetContactByID(ctx, contactID)
	if err != nil {
		return nil, fmt.Errorf("contacto no encontrado: %w", err)
	}

	normEmail := ""
	if strings.TrimSpace(email) != "" {
		normEmail = NormalizeEmail(email)
		if normEmail == "" {
			return nil, fmt.Errorf("email inválido: %q", email)
		}
	}

	normPhone := ""
	if strings.TrimSpace(phone) != "" {
		normPhone = PhoneFromIdentifier(phone)
		if normPhone == "" {
			return nil, fmt.Errorf("teléfono inválido: %q", phone)
		}
	}

	if normEmail != "" {
		if owner, err := g.db.FindContactByEmail(ctx, normEmail); err == nil && owner != nil && owner.ID != contactID {
			return nil, fmt.Errorf("el email %s ya pertenece al contacto %q (%s)", normEmail, owner.DisplayName, owner.ID)
		}
	}

	if normPhone != "" {
		if owner, err := g.db.FindContactByPhone(ctx, normPhone); err == nil && owner != nil && owner.ID != contactID {
			return nil, fmt.Errorf("el teléfono %s ya pertenece al contacto %q (%s)", normPhone, owner.DisplayName, owner.ID)
		}
	}

	if normEmail != "" {
		contact.Email = normEmail
	}
	if normPhone != "" {
		contact.Phone = normPhone
	}
	contact.UpdatedAt = time.Now()

	if err := g.db.SaveContact(ctx, contact); err != nil {
		return nil, fmt.Errorf("error guardando identidad del contacto: %w", err)
	}

	return g.db.GetContactByID(ctx, contactID)
}

// FindContactsByIdentity busca contactos por email, teléfono o identificador de canal.
func (g *Graph) FindContactsByIdentity(ctx context.Context, value string) ([]domain.Contact, error) {
	normEmail := NormalizeEmail(value)
	normPhone := PhoneFromIdentifier(value)
	normRemoteID := NormalizeIdentifier(value)
	return g.db.SearchContactsByIdentity(ctx, normEmail, normPhone, normRemoteID)
}

// MergeContacts fusiona un contacto secundario dentro de uno principal, migrando todos sus canales.
func (g *Graph) MergeContacts(ctx context.Context, primaryID, secondaryID string) (*domain.Contact, error) {
	if primaryID == secondaryID {
		return nil, fmt.Errorf("no se puede fusionar un contacto consigo mismo")
	}

	if err := g.db.ReassignChannels(ctx, secondaryID, primaryID); err != nil {
		return nil, fmt.Errorf("error reasignando canales en merge: %w", err)
	}

	// Actualizar conversaciones vinculadas al secundario
	if err := g.db.ReassignConversations(ctx, secondaryID, primaryID); err != nil {
		return nil, fmt.Errorf("error reasignando conversaciones en merge: %w", err)
	}

	// Un solo hilo por contacto: mover los mensajes absorbidos al hilo canónico.
	if err := g.consolidateConversations(ctx, primaryID); err != nil {
		return nil, fmt.Errorf("error consolidando conversaciones en merge: %w", err)
	}

	// Rescatar la identidad canónica del secundario antes de eliminarlo
	secondary, _ := g.db.GetContactByID(ctx, secondaryID)

	// Eliminar el contacto secundario (libera sus índices únicos de email/teléfono)
	if err := g.db.DeleteContact(ctx, secondaryID); err != nil {
		return nil, fmt.Errorf("error eliminando contacto secundario: %w", err)
	}

	// Conservar email y teléfono que solo tenía el contacto absorbido
	if secondary != nil {
		if primary, err := g.db.GetContactByID(ctx, primaryID); err == nil {
			changed := false
			if primary.Email == "" && secondary.Email != "" {
				primary.Email = secondary.Email
				changed = true
			}
			if primary.Phone == "" && secondary.Phone != "" {
				primary.Phone = secondary.Phone
				changed = true
			}
			if changed {
				primary.UpdatedAt = time.Now()
				if err := g.db.SaveContact(ctx, primary); err != nil {
					return nil, fmt.Errorf("error conservando identidad en merge: %w", err)
				}
			}
		}
	}

	return g.db.GetContactByID(ctx, primaryID)
}

// consolidateConversations deja un solo hilo 1 a 1 por contacto, con todos los
// mensajes absorbidos y el canal del último mensaje como sticky.
func (g *Graph) consolidateConversations(ctx context.Context, contactID string) error {
	return g.db.ConsolidateDMConversations(ctx, contactID)
}

// SplitChannel desacopla un canal de su contacto actual y le crea un contacto individual dedicado.
func (g *Graph) SplitChannel(ctx context.Context, channelID, newContactName string) (*domain.Contact, error) {
	channel, err := g.db.GetChannelByID(ctx, channelID)
	if err != nil {
		return nil, fmt.Errorf("canal no encontrado: %w", err)
	}

	newContactID := uuid.New().String()
	now := time.Now()
	newContact := &domain.Contact{
		ID:          newContactID,
		DisplayName: newContactName,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := g.db.SaveContact(ctx, newContact); err != nil {
		return nil, fmt.Errorf("error creando nuevo contacto para desacoplar: %w", err)
	}

	channel.ContactID = newContactID
	if err := g.db.UpdateChannelContactID(ctx, channelID, newContactID); err != nil {
		return nil, fmt.Errorf("error actualizando vínculo del canal: %w", err)
	}

	return g.db.GetContactByID(ctx, newContactID)
}
