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

// ListContacts lista todos los contactos con sus canales en dos consultas (sin N+1).
func (d *DB) ListContacts(ctx context.Context) ([]domain.Contact, error) {
	rows, err := d.conn.QueryContext(ctx, `SELECT id, display_name, avatar_url, email, phone, created_at, updated_at FROM contacts ORDER BY display_name ASC`)
	if err != nil {
		return nil, err
	}

	var contacts []domain.Contact
	for rows.Next() {
		var c domain.Contact
		var avatar, email, phone sql.NullString
		if err := rows.Scan(&c.ID, &c.DisplayName, &avatar, &email, &phone, &c.CreatedAt, &c.UpdatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		if avatar.Valid {
			c.AvatarURL = avatar.String
		}
		scanContactIdentity(email, phone, &c)
		contacts = append(contacts, c)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	if len(contacts) == 0 {
		return contacts, nil
	}

	chRows, err := d.conn.QueryContext(ctx, `SELECT id, contact_id, provider, account_id, remote_id, handle, is_preferred, metadata FROM contact_channels WHERE contact_id <> ''`)
	if err != nil {
		return nil, err
	}
	defer chRows.Close()

	channelsByContact := make(map[string][]domain.ContactChannel, len(contacts))
	for chRows.Next() {
		ch, err := scanContactChannel(chRows)
		if err != nil {
			return nil, err
		}
		channelsByContact[ch.ContactID] = append(channelsByContact[ch.ContactID], ch)
	}
	if err := chRows.Err(); err != nil {
		return nil, err
	}
	for i := range contacts {
		contacts[i].Channels = channelsByContact[contacts[i].ID]
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
	if err := rows.Err(); err != nil {
		return nil, err
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

type channelScanner interface {
	Scan(dest ...any) error
}

func scanContactChannel(s channelScanner) (domain.ContactChannel, error) {
	var ch domain.ContactChannel
	var provider string
	var handle, meta sql.NullString
	if err := s.Scan(&ch.ID, &ch.ContactID, &provider, &ch.AccountID, &ch.RemoteID, &handle, &ch.IsPreferred, &meta); err != nil {
		return ch, err
	}
	ch.Provider = domain.Provider(provider)
	if handle.Valid {
		ch.Handle = handle.String
	}
	if meta.Valid {
		ch.Metadata = meta.String
	}
	return ch, nil
}

// SaveChannel almacena o actualiza un punto de contacto.
func (d *DB) SaveChannel(ctx context.Context, ch *domain.ContactChannel) error {
	query := `
	INSERT INTO contact_channels (id, contact_id, provider, account_id, remote_id, handle, is_preferred, metadata)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(provider, account_id, remote_id) DO UPDATE SET
		contact_id=excluded.contact_id,
		handle=excluded.handle,
		is_preferred=excluded.is_preferred;
	`
	_, err := d.conn.ExecContext(ctx, query, ch.ID, ch.ContactID, string(ch.Provider), ch.AccountID, ch.RemoteID, ch.Handle, ch.IsPreferred, ch.Metadata)
	return err
}

// GetChannelsByContactID retorna todos los canales vinculados a un contacto.
func (d *DB) GetChannelsByContactID(ctx context.Context, contactID string) ([]domain.ContactChannel, error) {
	query := `SELECT id, contact_id, provider, account_id, remote_id, handle, is_preferred, metadata FROM contact_channels WHERE contact_id = ?`
	rows, err := d.conn.QueryContext(ctx, query, contactID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []domain.ContactChannel
	for rows.Next() {
		ch, err := scanContactChannel(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, ch)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return list, nil
}

// GetChannelByID retorna un canal por su UUID.
func (d *DB) GetChannelByID(ctx context.Context, id string) (*domain.ContactChannel, error) {
	query := `SELECT id, contact_id, provider, account_id, remote_id, handle, is_preferred, metadata FROM contact_channels WHERE id = ?`
	ch, err := scanContactChannel(d.conn.QueryRowContext(ctx, query, id))
	if err != nil {
		return nil, err
	}
	return &ch, nil
}

// GetChannelByProviderAndRemoteID busca un canal por proveedor y remote_id.
func (d *DB) GetChannelByProviderAndRemoteID(ctx context.Context, provider, remoteID string) (*domain.ContactChannel, error) {
	query := `SELECT id, contact_id, provider, account_id, remote_id, handle, is_preferred, metadata FROM contact_channels WHERE provider = ? AND remote_id = ? LIMIT 1`
	ch, err := scanContactChannel(d.conn.QueryRowContext(ctx, query, provider, remoteID))
	if err != nil {
		return nil, err
	}
	return &ch, nil
}

// GetChannelByProviderAccountAndRemoteID busca un canal por proveedor, account_id y remote_id.
func (d *DB) GetChannelByProviderAccountAndRemoteID(ctx context.Context, provider, accountID, remoteID string) (*domain.ContactChannel, error) {
	query := `SELECT id, contact_id, provider, account_id, remote_id, handle, is_preferred, metadata FROM contact_channels WHERE provider = ? AND account_id = ? AND remote_id = ?`
	ch, err := scanContactChannel(d.conn.QueryRowContext(ctx, query, provider, accountID, remoteID))
	if err != nil {
		return nil, err
	}
	return &ch, nil
}

// FindChannelByRemoteIDAcrossProviders busca si existe algún canal con el mismo remote_id (ej. mismo teléfono en WhatsApp y Telegram).
func (d *DB) FindChannelByRemoteIDAcrossProviders(ctx context.Context, remoteID string) (*domain.ContactChannel, error) {
	query := `SELECT id, contact_id, provider, account_id, remote_id, handle, is_preferred, metadata FROM contact_channels WHERE remote_id = ? LIMIT 1`
	ch, err := scanContactChannel(d.conn.QueryRowContext(ctx, query, remoteID))
	if err != nil {
		return nil, err
	}
	return &ch, nil
}

// CleanupNonPersonContacts elimina contactos legacy cuyos canales pertenecen únicamente a grupos,
// temas de foro o listas de difusión, preservando intactas las conversaciones de grupo.
func (d *DB) CleanupNonPersonContacts(ctx context.Context) (int64, error) {
	rows, err := d.conn.QueryContext(ctx, `
		SELECT DISTINCT c.id
		FROM contacts c
		JOIN contact_channels cc ON cc.contact_id = c.id
		WHERE (cc.remote_id LIKE '%@g.us'
		   OR cc.remote_id LIKE '%@broadcast'
		   OR cc.remote_id LIKE '%@newsletter'
		   OR cc.remote_id LIKE '%_topic_%')
		  AND NOT EXISTS (
		    SELECT 1 FROM contact_channels cc2
		    WHERE cc2.contact_id = c.id
		      AND cc2.remote_id NOT LIKE '%@g.us'
		      AND cc2.remote_id NOT LIKE '%@broadcast'
		      AND cc2.remote_id NOT LIKE '%@newsletter'
		      AND cc2.remote_id NOT LIKE '%_topic_%'
		  )
	`)
	if err != nil {
		return 0, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()

	if len(ids) == 0 {
		return 0, nil
	}

	tx, err := d.conn.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	var deleted int64
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `UPDATE conversations SET contact_id = NULL WHERE contact_id = ?`, id); err != nil {
			return 0, err
		}
		// Preserve channels still referenced by group conversations or historical messages by detaching them.
		if _, err := tx.ExecContext(ctx, `
			UPDATE contact_channels
			SET contact_id = ''
			WHERE contact_id = ?
			  AND (
			    id IN (SELECT active_channel_id FROM conversations WHERE active_channel_id IS NOT NULL AND (is_group = 1 OR id LIKE 'whatsapp:%'))
			    OR id IN (SELECT channel_id FROM messages WHERE channel_id IS NOT NULL)
			  )
		`, id); err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE conversations
			SET active_channel_id = NULL
			WHERE active_channel_id IN (SELECT id FROM contact_channels WHERE contact_id = ?)
			  AND is_group = 0
			  AND id NOT LIKE 'whatsapp:%'
		`, id); err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM contact_channels WHERE contact_id = ?`, id); err != nil {
			return 0, err
		}
		res, err := tx.ExecContext(ctx, `DELETE FROM contacts WHERE id = ?`, id)
		if err != nil {
			return 0, err
		}
		if n, err := res.RowsAffected(); err == nil {
			deleted += n
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return deleted, nil
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
