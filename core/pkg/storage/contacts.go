package storage

import (
	"context"
	"database/sql"

	"github.com/cestila/ConduitOmniChat/core/pkg/domain"
)

// SaveContact inserta o actualiza un contacto.
func (d *DB) SaveContact(ctx context.Context, c *domain.Contact) error {
	query := `
	INSERT INTO contacts (id, display_name, avatar_url, email, phone, created_at, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		display_name=excluded.display_name,
		avatar_url=excluded.avatar_url,
		email=excluded.email,
		phone=excluded.phone,
		updated_at=excluded.updated_at;
	`
	_, err := d.conn.ExecContext(ctx, query, c.ID, c.DisplayName, c.AvatarURL, nullableIdentity(c.Email), nullableIdentity(c.Phone), c.CreatedAt, c.UpdatedAt)
	return err
}

// nullableIdentity convierte una identidad vacía en NULL para que los índices únicos parciales la ignoren.
func nullableIdentity(value string) any {
	if value == "" {
		return nil
	}
	return value
}

// scanContactIdentity rellena email y teléfono desde columnas anulables.
func scanContactIdentity(email, phone sql.NullString, c *domain.Contact) {
	if email.Valid {
		c.Email = email.String
	}
	if phone.Valid {
		c.Phone = phone.String
	}
}

// GetContactByID obtiene un contacto con sus canales asociados.
func (d *DB) GetContactByID(ctx context.Context, id string) (*domain.Contact, error) {
	query := `SELECT id, display_name, avatar_url, email, phone, created_at, updated_at FROM contacts WHERE id = ?`
	row := d.conn.QueryRowContext(ctx, query, id)

	var c domain.Contact
	var avatar, email, phone sql.NullString
	if err := row.Scan(&c.ID, &c.DisplayName, &avatar, &email, &phone, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return nil, err
	}
	if avatar.Valid {
		c.AvatarURL = avatar.String
	}
	scanContactIdentity(email, phone, &c)

	channels, err := d.GetChannelsByContactID(ctx, c.ID)
	if err == nil {
		c.Channels = channels
	}
	return &c, nil
}

// ListContacts lista todos los contactos con sus canales.
func (d *DB) ListContacts(ctx context.Context) ([]domain.Contact, error) {
	rows, err := d.conn.QueryContext(ctx, `SELECT id, display_name, avatar_url, email, phone, created_at, updated_at FROM contacts ORDER BY display_name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var contacts []domain.Contact
	for rows.Next() {
		var c domain.Contact
		var avatar, email, phone sql.NullString
		if err := rows.Scan(&c.ID, &c.DisplayName, &avatar, &email, &phone, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		if avatar.Valid {
			c.AvatarURL = avatar.String
		}
		scanContactIdentity(email, phone, &c)
		c.Channels, _ = d.GetChannelsByContactID(ctx, c.ID)
		contacts = append(contacts, c)
	}
	return contacts, nil
}

// FindContactByEmail busca el contacto dueño de un email canónico. Devuelve sql.ErrNoRows si no existe.
func (d *DB) FindContactByEmail(ctx context.Context, email string) (*domain.Contact, error) {
	var id string
	err := d.conn.QueryRowContext(ctx, `SELECT id FROM contacts WHERE email = ? LIMIT 1`, email).Scan(&id)
	if err != nil {
		return nil, err
	}
	return d.GetContactByID(ctx, id)
}

// FindContactByPhone busca el contacto dueño de un teléfono canónico. Devuelve sql.ErrNoRows si no existe.
func (d *DB) FindContactByPhone(ctx context.Context, phone string) (*domain.Contact, error) {
	var id string
	err := d.conn.QueryRowContext(ctx, `SELECT id FROM contacts WHERE phone = ? LIMIT 1`, phone).Scan(&id)
	if err != nil {
		return nil, err
	}
	return d.GetContactByID(ctx, id)
}

// SearchContactsByIdentity encuentra contactos por email, teléfono canónico o identificador de canal.
func (d *DB) SearchContactsByIdentity(ctx context.Context, email, phone, channelRemoteID string) ([]domain.Contact, error) {
	rows, err := d.conn.QueryContext(ctx, `
		SELECT DISTINCT c.id
		FROM contacts c
		LEFT JOIN contact_channels cc ON cc.contact_id = c.id
		WHERE (? <> '' AND c.email = ?)
		   OR (? <> '' AND c.phone = ?)
		   OR (? <> '' AND cc.remote_id = ?)
		ORDER BY c.display_name ASC
	`, email, email, phone, phone, channelRemoteID, channelRemoteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}

	contacts := make([]domain.Contact, 0, len(ids))
	for _, id := range ids {
		c, err := d.GetContactByID(ctx, id)
		if err != nil {
			continue
		}
		contacts = append(contacts, *c)
	}
	return contacts, nil
}

// DeleteContact elimina un contacto.
func (d *DB) DeleteContact(ctx context.Context, id string) error {
	_, err := d.conn.ExecContext(ctx, `DELETE FROM contacts WHERE id = ?`, id)
	return err
}

// SaveChannel almacena o actualiza un punto de contacto.
func (d *DB) SaveChannel(ctx context.Context, ch *domain.ContactChannel) error {
	query := `
	INSERT INTO contact_channels (id, contact_id, provider, remote_id, handle, is_preferred, metadata)
	VALUES (?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(provider, remote_id) DO UPDATE SET
		contact_id=excluded.contact_id,
		handle=excluded.handle,
		is_preferred=excluded.is_preferred;
	`
	_, err := d.conn.ExecContext(ctx, query, ch.ID, ch.ContactID, string(ch.Provider), ch.RemoteID, ch.Handle, ch.IsPreferred, ch.Metadata)
	return err
}

// GetChannelsByContactID retorna todos los canales vinculados a un contacto.
func (d *DB) GetChannelsByContactID(ctx context.Context, contactID string) ([]domain.ContactChannel, error) {
	query := `SELECT id, contact_id, provider, remote_id, handle, is_preferred, metadata FROM contact_channels WHERE contact_id = ?`
	rows, err := d.conn.QueryContext(ctx, query, contactID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []domain.ContactChannel
	for rows.Next() {
		var ch domain.ContactChannel
		var provider string
		var handle, meta sql.NullString
		if err := rows.Scan(&ch.ID, &ch.ContactID, &provider, &ch.RemoteID, &handle, &ch.IsPreferred, &meta); err != nil {
			return nil, err
		}
		ch.Provider = domain.Provider(provider)
		if handle.Valid {
			ch.Handle = handle.String
		}
		if meta.Valid {
			ch.Metadata = meta.String
		}
		list = append(list, ch)
	}
	return list, nil
}

// GetChannelByID retorna un canal por su UUID.
func (d *DB) GetChannelByID(ctx context.Context, id string) (*domain.ContactChannel, error) {
	query := `SELECT id, contact_id, provider, remote_id, handle, is_preferred, metadata FROM contact_channels WHERE id = ?`
	row := d.conn.QueryRowContext(ctx, query, id)

	var ch domain.ContactChannel
	var provider string
	var handle, meta sql.NullString
	if err := row.Scan(&ch.ID, &ch.ContactID, &provider, &ch.RemoteID, &handle, &ch.IsPreferred, &meta); err != nil {
		return nil, err
	}
	ch.Provider = domain.Provider(provider)
	if handle.Valid {
		ch.Handle = handle.String
	}
	if meta.Valid {
		ch.Metadata = meta.String
	}
	return &ch, nil
}

// GetChannelByProviderAndRemoteID busca un canal por proveedor y remote_id.
func (d *DB) GetChannelByProviderAndRemoteID(ctx context.Context, provider, remoteID string) (*domain.ContactChannel, error) {
	query := `SELECT id, contact_id, provider, remote_id, handle, is_preferred, metadata FROM contact_channels WHERE provider = ? AND remote_id = ?`
	row := d.conn.QueryRowContext(ctx, query, provider, remoteID)

	var ch domain.ContactChannel
	var prov string
	var handle, meta sql.NullString
	if err := row.Scan(&ch.ID, &ch.ContactID, &prov, &ch.RemoteID, &handle, &ch.IsPreferred, &meta); err != nil {
		return nil, err
	}
	ch.Provider = domain.Provider(prov)
	if handle.Valid {
		ch.Handle = handle.String
	}
	if meta.Valid {
		ch.Metadata = meta.String
	}
	return &ch, nil
}

// FindChannelByRemoteIDAcrossProviders busca si existe algún canal con el mismo remote_id (ej. mismo teléfono en WhatsApp y Telegram).
func (d *DB) FindChannelByRemoteIDAcrossProviders(ctx context.Context, remoteID string) (*domain.ContactChannel, error) {
	query := `SELECT id, contact_id, provider, remote_id, handle, is_preferred, metadata FROM contact_channels WHERE remote_id = ? LIMIT 1`
	row := d.conn.QueryRowContext(ctx, query, remoteID)

	var ch domain.ContactChannel
	var prov string
	var handle, meta sql.NullString
	if err := row.Scan(&ch.ID, &ch.ContactID, &prov, &ch.RemoteID, &handle, &ch.IsPreferred, &meta); err != nil {
		return nil, err
	}
	ch.Provider = domain.Provider(prov)
	if handle.Valid {
		ch.Handle = handle.String
	}
	if meta.Valid {
		ch.Metadata = meta.String
	}
	return &ch, nil
}

// ReassignChannels migra todos los canales de un contacto a otro (Merge).
func (d *DB) ReassignChannels(ctx context.Context, fromContactID, toContactID string) error {
	_, err := d.conn.ExecContext(ctx, `UPDATE contact_channels SET contact_id = ? WHERE contact_id = ?`, toContactID, fromContactID)
	return err
}

// ReassignConversations migra todas las conversaciones de un contacto a otro (Merge).
func (d *DB) ReassignConversations(ctx context.Context, fromContactID, toContactID string) error {
	_, err := d.conn.ExecContext(ctx, `UPDATE conversations SET contact_id = ? WHERE contact_id = ?`, toContactID, fromContactID)
	return err
}

// UpdateChannelContactID cambia el contacto asociado a un canal (Split).
func (d *DB) UpdateChannelContactID(ctx context.Context, channelID, newContactID string) error {
	_, err := d.conn.ExecContext(ctx, `UPDATE contact_channels SET contact_id = ? WHERE id = ?`, newContactID, channelID)
	return err
}
