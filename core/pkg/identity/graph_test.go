package identity

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/cestila/ConduitOmniChat/core/pkg/domain"
	"github.com/cestila/ConduitOmniChat/core/pkg/storage"
)

func TestIdentityGraphAutoMatchingAndMergeSplit(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "identity_test.db")

	db, err := storage.NewDB(dbPath)
	if err != nil {
		t.Fatalf("error abriendo db: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	graph := NewGraph(db)

	// 1. Vincular canal WhatsApp (+54 9 11 1234-5678)
	c1, _, err := graph.ResolveOrLinkChannel(ctx, domain.ProviderWhatsApp, "+54 9 11 1234-5678", "+5491112345678", "Martín Gómez", "https://avatar.url/martin.jpg", Hints{})
	if err != nil {
		t.Fatalf("error creando contacto WA: %v", err)
	}
	if c1.AvatarURL != "https://avatar.url/martin.jpg" {
		t.Fatalf("se esperaba avatar_url 'https://avatar.url/martin.jpg', obtenido '%s'", c1.AvatarURL)
	}

	// 2. Vincular canal Telegram con el mismo número telefónico (debería auto-emparejar al mismo contacto c1)
	c2, _, err := graph.ResolveOrLinkChannel(ctx, domain.ProviderTelegram, "5491112345678", "@martingomez", "Martín Telegram", "", Hints{})
	if err != nil {
		t.Fatalf("error emparejando canal TG: %v", err)
	}

	if c1.ID != c2.ID {
		t.Fatalf("se esperaba auto-emparejamiento al mismo contacto, obtenido c1: %s, c2: %s", c1.ID, c2.ID)
	}

	// 3. Crear otro contacto separado (ej. Discord)
	c3, ch3, err := graph.ResolveOrLinkChannel(ctx, domain.ProviderDiscord, "discord-user-999", "marting#1234", "Martín DC", "", Hints{})
	if err != nil {
		t.Fatalf("error creando contacto DC: %v", err)
	}

	if c3.ID == c1.ID {
		t.Fatalf("c3 no debería coincidir inicialmente con c1")
	}

	// 4. Probar Merge manual (fusionar c3 dentro de c1)
	merged, err := graph.MergeContacts(ctx, c1.ID, c3.ID)
	if err != nil {
		t.Fatalf("error ejecutando merge: %v", err)
	}

	if len(merged.Channels) != 3 {
		t.Fatalf("se esperaban 3 canales en el contacto fusionado, obtenidos %d", len(merged.Channels))
	}

	// 5. Probar Split manual (desacoplar el canal de Discord a un nuevo contacto separado)
	splitContact, err := graph.SplitChannel(ctx, ch3.ID, "Martín Discord Separado")
	if err != nil {
		t.Fatalf("error ejecutando split: %v", err)
	}

	if splitContact.ID == c1.ID {
		t.Fatalf("el nuevo contacto desacoplado no debe compartir el ID de c1")
	}

	if len(splitContact.Channels) != 1 || splitContact.Channels[0].ID != ch3.ID {
		t.Fatalf("el contacto desacoplado debe contener exclusivamente el canal de Discord")
	}
}

// newTestGraph abre una base temporal aislada por prueba.
func newTestGraph(t *testing.T) *Graph {
	t.Helper()
	db, err := storage.NewDB(filepath.Join(t.TempDir(), "identity_test.db"))
	if err != nil {
		t.Fatalf("error abriendo db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewGraph(db)
}

func TestSetContactIdentityNormalizesAndPersists(t *testing.T) {
	ctx := context.Background()
	graph := newTestGraph(t)

	c, _, err := graph.ResolveOrLinkChannel(ctx, domain.ProviderDiscord, "discord-user-1", "someone#1", "Someone", "", Hints{})
	if err != nil {
		t.Fatalf("error creando contacto: %v", err)
	}

	updated, err := graph.SetContactIdentity(ctx, c.ID, "  Someone@Example.COM  ", "+54 9 11 5555-0000")
	if err != nil {
		t.Fatalf("error asignando identidad: %v", err)
	}

	if updated.Email != "someone@example.com" {
		t.Fatalf("se esperaba email normalizado 'someone@example.com', obtenido '%s'", updated.Email)
	}
	if updated.Phone != "+5491155550000" {
		t.Fatalf("se esperaba teléfono normalizado '+5491155550000', obtenido '%s'", updated.Phone)
	}

	reloaded, err := graph.db.GetContactByID(ctx, c.ID)
	if err != nil {
		t.Fatalf("error recargando contacto: %v", err)
	}
	if reloaded.Email != "someone@example.com" || reloaded.Phone != "+5491155550000" {
		t.Fatalf("la identidad no se persistió: email='%s' phone='%s'", reloaded.Email, reloaded.Phone)
	}
}

func TestAutoLinkByEmailAndPhoneAcrossProviders(t *testing.T) {
	ctx := context.Background()
	graph := newTestGraph(t)

	base, _, err := graph.ResolveOrLinkChannel(ctx, domain.ProviderDiscord, "discord-user-1", "someone#1", "Someone", "", Hints{})
	if err != nil {
		t.Fatalf("error creando contacto base: %v", err)
	}
	if _, err := graph.SetContactIdentity(ctx, base.ID, "someone@example.com", "+5491155550000"); err != nil {
		t.Fatalf("error asignando identidad: %v", err)
	}

	// Canal de otro proveedor cuyo identificador es el mismo teléfono.
	byPhone, _, err := graph.ResolveOrLinkChannel(ctx, domain.ProviderWhatsApp, "+54 9 11 5555-0000", "", "Someone WA", "", Hints{})
	if err != nil {
		t.Fatalf("error vinculando canal por teléfono: %v", err)
	}
	if byPhone.ID != base.ID {
		t.Fatalf("el canal por teléfono debió enlazarse al contacto base (%s), obtuvo %s", base.ID, byPhone.ID)
	}

	// Canal de otro proveedor cuyo handle es el mismo email.
	byEmail, _, err := graph.ResolveOrLinkChannel(ctx, domain.ProviderTelegram, "tg-user-77", "someone@example.com", "Someone TG", "", Hints{})
	if err != nil {
		t.Fatalf("error vinculando canal por email: %v", err)
	}
	if byEmail.ID != base.ID {
		t.Fatalf("el canal por email debió enlazarse al contacto base (%s), obtuvo %s", base.ID, byEmail.ID)
	}

	final, err := graph.db.GetContactByID(ctx, base.ID)
	if err != nil {
		t.Fatalf("error recargando contacto: %v", err)
	}
	if len(final.Channels) != 3 {
		t.Fatalf("se esperaban 3 canales enlazados, obtenidos %d", len(final.Channels))
	}
}

func TestSetContactIdentityRejectsDuplicateEmail(t *testing.T) {
	ctx := context.Background()
	graph := newTestGraph(t)

	first, _, err := graph.ResolveOrLinkChannel(ctx, domain.ProviderDiscord, "discord-user-1", "one#1", "One", "", Hints{})
	if err != nil {
		t.Fatalf("error creando primer contacto: %v", err)
	}
	if _, err := graph.SetContactIdentity(ctx, first.ID, "shared@example.com", ""); err != nil {
		t.Fatalf("error asignando email al primero: %v", err)
	}

	second, _, err := graph.ResolveOrLinkChannel(ctx, domain.ProviderDiscord, "discord-user-2", "two#1", "Two", "", Hints{})
	if err != nil {
		t.Fatalf("error creando segundo contacto: %v", err)
	}

	if _, err := graph.SetContactIdentity(ctx, second.ID, "shared@example.com", ""); err == nil {
		t.Fatalf("se esperaba error al reutilizar un email ya asignado")
	}

	reloaded, err := graph.db.GetContactByID(ctx, second.ID)
	if err != nil {
		t.Fatalf("error recargando segundo contacto: %v", err)
	}
	if reloaded.Email != "" {
		t.Fatalf("el segundo contacto no debió quedar con email, obtuvo '%s'", reloaded.Email)
	}
}

func TestSetContactIdentityRejectsDuplicatePhone(t *testing.T) {
	ctx := context.Background()
	graph := newTestGraph(t)

	first, _, err := graph.ResolveOrLinkChannel(ctx, domain.ProviderDiscord, "discord-user-1", "one#1", "One", "", Hints{})
	if err != nil {
		t.Fatalf("error creando primer contacto: %v", err)
	}
	if _, err := graph.SetContactIdentity(ctx, first.ID, "", "+5491100001111"); err != nil {
		t.Fatalf("error asignando teléfono al primero: %v", err)
	}

	second, _, err := graph.ResolveOrLinkChannel(ctx, domain.ProviderDiscord, "discord-user-2", "two#1", "Two", "", Hints{})
	if err != nil {
		t.Fatalf("error creando segundo contacto: %v", err)
	}
	if _, err := graph.SetContactIdentity(ctx, second.ID, "", "+54 9 11 0000-1111"); err == nil {
		t.Fatalf("se esperaba error al reutilizar un teléfono ya asignado")
	}
}

func TestFindContactsByIdentity(t *testing.T) {
	ctx := context.Background()
	graph := newTestGraph(t)

	target, _, err := graph.ResolveOrLinkChannel(ctx, domain.ProviderDiscord, "discord-user-1", "one#1", "One", "", Hints{})
	if err != nil {
		t.Fatalf("error creando contacto: %v", err)
	}
	if _, err := graph.SetContactIdentity(ctx, target.ID, "One@Example.com", "+5491100002222"); err != nil {
		t.Fatalf("error asignando identidad: %v", err)
	}

	other, _, err := graph.ResolveOrLinkChannel(ctx, domain.ProviderDiscord, "discord-user-2", "two#1", "Two", "", Hints{})
	if err != nil {
		t.Fatalf("error creando otro contacto: %v", err)
	}

	cases := []string{"one@example.com", "ONE@EXAMPLE.COM", "+54 9 11 0000-2222", "+5491100002222", "discord-user-1"}
	for _, value := range cases {
		found, err := graph.FindContactsByIdentity(ctx, value)
		if err != nil {
			t.Fatalf("error buscando '%s': %v", value, err)
		}
		if len(found) != 1 || found[0].ID != target.ID {
			t.Fatalf("búsqueda '%s' debió devolver solo el contacto %s, obtuvo %d resultados", value, target.ID, len(found))
		}
	}

	notFound, err := graph.FindContactsByIdentity(ctx, "nadie@example.com")
	if err != nil {
		t.Fatalf("error buscando identidad inexistente: %v", err)
	}
	if len(notFound) != 0 {
		t.Fatalf("no se esperaban resultados, obtuvo %d", len(notFound))
	}

	_ = other
}

// Un canal de Telegram que recién conoce el teléfono del contacto debe unirse
// al contacto que ya tenía ese teléfono, sin dejar duplicados.
func TestLatePhoneHintMergesExistingContacts(t *testing.T) {
	ctx := context.Background()
	graph := newTestGraph(t)

	wa, _, err := graph.ResolveOrLinkChannel(ctx, domain.ProviderWhatsApp, "5491100003333@s.whatsapp.net", "", "Persona WA", "", Hints{})
	if err != nil {
		t.Fatalf("error creando contacto WhatsApp: %v", err)
	}
	if wa.Phone != "+5491100003333" {
		t.Fatalf("el contacto de WhatsApp debió quedar con teléfono, obtuvo %q", wa.Phone)
	}

	tg, _, err := graph.ResolveOrLinkChannel(ctx, domain.ProviderTelegram, "u7001", "persona", "Persona TG", "", Hints{})
	if err != nil {
		t.Fatalf("error creando contacto Telegram: %v", err)
	}
	if tg.ID == wa.ID {
		t.Fatalf("los contactos no debieron unirse todavía")
	}

	linked, channel, err := graph.ResolveOrLinkChannel(ctx, domain.ProviderTelegram, "u7001", "persona", "Persona TG", "", Hints{Phone: "+54 9 11 0000-3333"})
	if err != nil {
		t.Fatalf("error uniendo identidad tardía: %v", err)
	}
	if linked.ID != wa.ID {
		t.Fatalf("la identidad tardía debió unir el canal al contacto %s, obtuvo %s", wa.ID, linked.ID)
	}
	if channel == nil || channel.ContactID != wa.ID {
		t.Fatalf("el canal de Telegram debió quedar bajo el contacto unificado")
	}

	contacts, err := graph.db.ListContacts(ctx)
	if err != nil {
		t.Fatalf("error listando contactos: %v", err)
	}
	if len(contacts) != 1 {
		t.Fatalf("se esperaba un solo contacto unificado, obtenidos %d", len(contacts))
	}
	if len(contacts[0].Channels) != 2 {
		t.Fatalf("se esperaban 2 canales unificados, obtenidos %d", len(contacts[0].Channels))
	}
}

// Un canal nuevo que llega con el teléfono del contacto se enlaza sin crear otro.
func TestPhoneHintLinksNewChannelToExistingContact(t *testing.T) {
	ctx := context.Background()
	graph := newTestGraph(t)

	wa, _, err := graph.ResolveOrLinkChannel(ctx, domain.ProviderWhatsApp, "5491100004444@s.whatsapp.net", "", "Persona WA", "", Hints{})
	if err != nil {
		t.Fatalf("error creando contacto WhatsApp: %v", err)
	}

	tg, _, err := graph.ResolveOrLinkChannel(ctx, domain.ProviderTelegram, "u8001", "persona", "Persona TG", "", Hints{Phone: "+5491100004444"})
	if err != nil {
		t.Fatalf("error enlazando canal por teléfono: %v", err)
	}
	if tg.ID != wa.ID {
		t.Fatalf("el canal debió enlazarse al contacto %s, obtuvo %s", wa.ID, tg.ID)
	}
}

// El email canónico que aporta un proveedor también une canales existentes.
func TestEmailHintLinksChannelToExistingContact(t *testing.T) {
	ctx := context.Background()
	graph := newTestGraph(t)

	base, _, err := graph.ResolveOrLinkChannel(ctx, domain.ProviderDiscord, "discord-user-1", "someone#1", "Someone", "", Hints{})
	if err != nil {
		t.Fatalf("error creando contacto base: %v", err)
	}
	if _, err := graph.SetContactIdentity(ctx, base.ID, "someone@example.com", ""); err != nil {
		t.Fatalf("error asignando email: %v", err)
	}

	linked, _, err := graph.ResolveOrLinkChannel(ctx, domain.ProviderGmail, "gmail-user-1", "", "Someone Gmail", "", Hints{Email: "  Someone@Example.COM "})
	if err != nil {
		t.Fatalf("error enlazando canal por email: %v", err)
	}
	if linked.ID != base.ID {
		t.Fatalf("el canal debió enlazarse al contacto %s, obtuvo %s", base.ID, linked.ID)
	}
}

// Fusionar contactos debe dejar un solo hilo 1 a 1 con todos los mensajes y el
// canal del último mensaje como sticky.
func TestMergeContactsConsolidatesConversations(t *testing.T) {
	ctx := context.Background()
	graph := newTestGraph(t)

	primary, primaryChannel, err := graph.ResolveOrLinkChannel(ctx, domain.ProviderWhatsApp, "5491100005555@s.whatsapp.net", "", "Persona WA", "", Hints{})
	if err != nil {
		t.Fatalf("error creando contacto WhatsApp: %v", err)
	}
	secondary, secondaryChannel, err := graph.ResolveOrLinkChannel(ctx, domain.ProviderTelegram, "u9001", "persona", "Persona TG", "", Hints{})
	if err != nil {
		t.Fatalf("error creando contacto Telegram: %v", err)
	}

	ensureDMConversation(t, graph, primary.ID, primaryChannel.ID)
	ensureDMConversation(t, graph, secondary.ID, secondaryChannel.ID)

	saveMessage(t, graph, "msg-wa", primary.ID, primaryChannel.ID, time.Unix(1000, 0))
	saveMessage(t, graph, "msg-tg", secondary.ID, secondaryChannel.ID, time.Unix(2000, 0))

	merged, err := graph.MergeContacts(ctx, primary.ID, secondary.ID)
	if err != nil {
		t.Fatalf("error fusionando contactos: %v", err)
	}
	if len(merged.Channels) != 2 {
		t.Fatalf("se esperaban 2 canales unificados, obtenidos %d", len(merged.Channels))
	}

	conversationIDs, err := graph.db.ListDMConversationIDsByContact(ctx, primary.ID)
	if err != nil {
		t.Fatalf("error listando conversaciones: %v", err)
	}
	if len(conversationIDs) != 1 || conversationIDs[0] != primary.ID {
		t.Fatalf("se esperaba un solo hilo con id %s, obtenidos %v", primary.ID, conversationIDs)
	}

	messages, err := graph.db.GetMessagesByConversation(ctx, primary.ID, 10, "")
	if err != nil {
		t.Fatalf("error leyendo mensajes del hilo unificado: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("se esperaban 2 mensajes en el hilo unificado, obtenidos %d", len(messages))
	}

	conversation, err := graph.db.GetConversationByID(ctx, primary.ID)
	if err != nil {
		t.Fatalf("error leyendo la conversación unificada: %v", err)
	}
	if conversation.ActiveChannelId != secondaryChannel.ID {
		t.Fatalf("el sticky debió quedar en el canal del último mensaje (%s), obtuvo %s", secondaryChannel.ID, conversation.ActiveChannelId)
	}
	if conversation.LastMessageAtUnix != 2000 {
		t.Fatalf("last_message_at debió ser 2000, obtuvo %d", conversation.LastMessageAtUnix)
	}
}

// ensureDMConversation crea el hilo 1 a 1 canónico (id = contact_id).
func ensureDMConversation(t *testing.T, graph *Graph, contactID, channelID string) {
	t.Helper()
	if err := graph.db.EnsureConversation(context.Background(), &domain.Conversation{
		ID:              contactID,
		ContactID:       &contactID,
		IsGroup:         false,
		Title:           contactID,
		ActiveChannelID: &channelID,
	}); err != nil {
		t.Fatalf("error creando conversación %s: %v", contactID, err)
	}
}

// saveMessage persiste un mensaje mínimo para probar la consolidación de hilos.
func saveMessage(t *testing.T, graph *Graph, id, conversationID, channelID string, sentAt time.Time) {
	t.Helper()
	if err := graph.db.SaveMessage(context.Background(), &domain.Message{
		ID:              id,
		ConversationID:  conversationID,
		ChannelID:       channelID,
		RemoteMessageID: id,
		SenderType:      domain.SenderContact,
		Body:            id,
		ContentType:     domain.ContentTypeText,
		Status:          domain.StatusDelivered,
		SentAt:          sentAt,
	}); err != nil {
		t.Fatalf("error guardando mensaje %s: %v", id, err)
	}
}

func TestNewChannelWithEmailIdentifierSetsContactEmail(t *testing.T) {
	ctx := context.Background()
	graph := newTestGraph(t)

	c, _, err := graph.ResolveOrLinkChannel(ctx, domain.ProviderGmail, "nuevo@example.com", "nuevo@example.com", "Nuevo Contacto", "", Hints{})
	if err != nil {
		t.Fatalf("error creando contacto por email: %v", err)
	}
	if c.Email != "nuevo@example.com" {
		t.Fatalf("se esperaba email 'nuevo@example.com' en el contacto, obtuvo '%s'", c.Email)
	}
}
