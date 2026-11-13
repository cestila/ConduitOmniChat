package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cestila/ConduitOmniChat/core/pkg/domain"
)

// AddReaction agrega o actualiza una reacción de un remitente a un mensaje de forma idempotente.
func (d *DB) AddReaction(ctx context.Context, messageID, senderID, emoji string) error {
	messageID = strings.TrimSpace(messageID)
	senderID = strings.TrimSpace(senderID)
	emoji = strings.TrimSpace(emoji)
	if messageID == "" || senderID == "" || emoji == "" {
		return fmt.Errorf("message_id, sender_id y emoji son obligatorios")
	}

	query := `
	INSERT INTO message_reactions (message_id, sender_id, emoji, created_at)
	VALUES (?, ?, ?, CURRENT_TIMESTAMP)
	ON CONFLICT(message_id, sender_id, emoji) DO UPDATE SET created_at = CURRENT_TIMESTAMP;
	`
	_, err := d.conn.ExecContext(ctx, query, messageID, senderID, emoji)
	return err
}

// RemoveReaction elimina una reacción específica de un remitente.
func (d *DB) RemoveReaction(ctx context.Context, messageID, senderID, emoji string) error {
	query := `DELETE FROM message_reactions WHERE message_id = ? AND sender_id = ? AND emoji = ?;`
	_, err := d.conn.ExecContext(ctx, query, messageID, senderID, emoji)
	return err
}

// RemoveAllReactionsBySender elimina todas las reacciones de un remitente para un mensaje.
func (d *DB) RemoveAllReactionsBySender(ctx context.Context, messageID, senderID string) error {
	query := `DELETE FROM message_reactions WHERE message_id = ? AND sender_id = ?;`
	_, err := d.conn.ExecContext(ctx, query, messageID, senderID)
	return err
}

// ToggleReaction agrega la reacción si no existe o la remueve si ya estaba presente.
// Retorna true si fue agregada, o false si fue removida.
func (d *DB) ToggleReaction(ctx context.Context, messageID, senderID, emoji string) (bool, error) {
	messageID = strings.TrimSpace(messageID)
	senderID = strings.TrimSpace(senderID)
	emoji = strings.TrimSpace(emoji)
	if messageID == "" || senderID == "" || emoji == "" {
		return false, fmt.Errorf("message_id, sender_id y emoji son obligatorios")
	}

	var exists int
	err := d.conn.QueryRowContext(ctx,
		`SELECT 1 FROM message_reactions WHERE message_id = ? AND sender_id = ? AND emoji = ? LIMIT 1;`,
		messageID, senderID, emoji,
	).Scan(&exists)

	if err == nil && exists == 1 {
		// Ya existía: remover
		if err := d.RemoveReaction(ctx, messageID, senderID, emoji); err != nil {
			return false, err
		}
		return false, nil
	} else if err != nil && err != sql.ErrNoRows {
		return false, err
	}

	// No existía: agregar
	if err := d.AddReaction(ctx, messageID, senderID, emoji); err != nil {
		return false, err
	}
	return true, nil
}

// GetReactionsByMessageIDs agrupa y devuelve las reacciones de una lista de mensajes.
func (d *DB) GetReactionsByMessageIDs(ctx context.Context, messageIDs []string) (map[string][]domain.Reaction, error) {
	result := make(map[string][]domain.Reaction)
	if len(messageIDs) == 0 {
		return result, nil
	}

	placeholders := make([]string, len(messageIDs))
	args := make([]any, len(messageIDs))
	for i, id := range messageIDs {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf(`
		SELECT message_id, emoji, count(*) AS cnt, group_concat(sender_id, ',') AS senders
		FROM message_reactions
		WHERE message_id IN (%s)
		GROUP BY message_id, emoji
		ORDER BY min(created_at) ASC;
	`, strings.Join(placeholders, ","))

	rows, err := d.conn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("error consultando reacciones: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var msgID, emoji, sendersConcat string
		var count int
		if err := rows.Scan(&msgID, &emoji, &count, &sendersConcat); err != nil {
			return nil, err
		}

		var senders []string
		includesMe := false
		if sendersConcat != "" {
			parts := strings.Split(sendersConcat, ",")
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if p != "" {
					senders = append(senders, p)
					if p == "me" || p == "YO" {
						includesMe = true
					}
				}
			}
		}

		result[msgID] = append(result[msgID], domain.Reaction{
			Emoji:      emoji,
			Count:      count,
			Senders:    senders,
			IncludesMe: includesMe,
		})
	}

	return result, nil
}

// FindMessageByID busca un mensaje por su identificador local primario.
func (d *DB) FindMessageByID(ctx context.Context, id string) (*domain.Message, error) {
	query := `
		SELECT id, conversation_id, channel_id, remote_message_id, sender_type, coalesce(body, ''),
		       content_type, status, sent_at, coalesce(reply_to_message_id, ''), coalesce(metadata, '')
		FROM messages
		WHERE id = ? LIMIT 1;
	`
	var m domain.Message
	var replyTo, metaStr string
	var sentAtRaw any
	err := d.conn.QueryRowContext(ctx, query, id).Scan(
		&m.ID, &m.ConversationID, &m.ChannelID, &m.RemoteMessageID,
		&m.SenderType, &m.Body, &m.ContentType, &m.Status, &sentAtRaw, &replyTo, &metaStr,
	)
	if err != nil {
		return nil, err
	}
	if replyTo != "" {
		m.ReplyToMessageID = &replyTo
	}
	if t, ok := sentAtRaw.(time.Time); ok {
		m.SentAt = t
	}
	m.Metadata = metaStr
	if metaStr != "" {
		if strings.Contains(metaStr, `"question"`) {
			var p domain.Poll
			if err := json.Unmarshal([]byte(metaStr), &p); err == nil && p.Question != "" {
				m.Poll = &p
			}
		}
		if strings.Contains(metaStr, `"button_rows"`) {
			var bObj struct {
				ButtonRows []domain.ButtonRow `json:"button_rows"`
			}
			if err := json.Unmarshal([]byte(metaStr), &bObj); err == nil && len(bObj.ButtonRows) > 0 {
				m.ButtonRows = bObj.ButtonRows
			}
		}
		var metaFields struct {
			IsEdited      bool                  `json:"is_edited"`
			EditedAtUnix  int64                 `json:"edited_at_unix"`
			IsPinned      bool                  `json:"is_pinned"`
			Location      *domain.Location      `json:"location"`
			SharedContact *domain.SharedContact `json:"shared_contact"`
		}
		if err := json.Unmarshal([]byte(metaStr), &metaFields); err == nil {
			m.IsEdited = metaFields.IsEdited
			if metaFields.EditedAtUnix > 0 {
				t := time.Unix(metaFields.EditedAtUnix, 0)
				m.EditedAt = &t
			}
			m.IsPinned = metaFields.IsPinned
			m.Location = metaFields.Location
			m.SharedContact = metaFields.SharedContact
		}
	}
	return &m, nil
}

// FindMessageByRemoteID busca un mensaje por su channel_id y remote_message_id.
// Si channelID es vacío, busca el mensaje únicamente por remote_message_id.
func (d *DB) FindMessageByRemoteID(ctx context.Context, channelID, remoteMessageID string) (*domain.Message, error) {
	var query string
	var args []any
	if channelID != "" {
		query = `
			SELECT id, conversation_id, channel_id, remote_message_id, sender_type, coalesce(body, ''),
			       content_type, status, sent_at, coalesce(reply_to_message_id, ''), coalesce(metadata, '')
			FROM messages
			WHERE channel_id = ? AND remote_message_id = ? LIMIT 1;
		`
		args = []any{channelID, remoteMessageID}
	} else {
		query = `
			SELECT id, conversation_id, channel_id, remote_message_id, sender_type, coalesce(body, ''),
			       content_type, status, sent_at, coalesce(reply_to_message_id, ''), coalesce(metadata, '')
			FROM messages
			WHERE remote_message_id = ? LIMIT 1;
		`
		args = []any{remoteMessageID}
	}
	var m domain.Message
	var replyTo, metaStr string
	var sentAtRaw any
	err := d.conn.QueryRowContext(ctx, query, args...).Scan(
		&m.ID, &m.ConversationID, &m.ChannelID, &m.RemoteMessageID,
		&m.SenderType, &m.Body, &m.ContentType, &m.Status, &sentAtRaw, &replyTo, &metaStr,
	)
	if err != nil {
		return nil, err
	}
	if replyTo != "" {
		m.ReplyToMessageID = &replyTo
	}
	if t, ok := sentAtRaw.(time.Time); ok {
		m.SentAt = t
	}
	m.Metadata = metaStr
	if metaStr != "" {
		if strings.Contains(metaStr, `"question"`) {
			var p domain.Poll
			if err := json.Unmarshal([]byte(metaStr), &p); err == nil && p.Question != "" {
				m.Poll = &p
			}
		}
		if strings.Contains(metaStr, `"button_rows"`) {
			var bObj struct {
				ButtonRows []domain.ButtonRow `json:"button_rows"`
			}
			if err := json.Unmarshal([]byte(metaStr), &bObj); err == nil && len(bObj.ButtonRows) > 0 {
				m.ButtonRows = bObj.ButtonRows
			}
		}
		var metaFields struct {
			IsEdited      bool                  `json:"is_edited"`
			EditedAtUnix  int64                 `json:"edited_at_unix"`
			IsPinned      bool                  `json:"is_pinned"`
			Location      *domain.Location      `json:"location"`
			SharedContact *domain.SharedContact `json:"shared_contact"`
		}
		if err := json.Unmarshal([]byte(metaStr), &metaFields); err == nil {
			m.IsEdited = metaFields.IsEdited
			if metaFields.EditedAtUnix > 0 {
				t := time.Unix(metaFields.EditedAtUnix, 0)
				m.EditedAt = &t
			}
			m.IsPinned = metaFields.IsPinned
			m.Location = metaFields.Location
			m.SharedContact = metaFields.SharedContact
		}
	}
	return &m, nil
}

// FindMessageIDByRemoteID retorna el id local dado el channel_id y remote_message_id.
func (d *DB) FindMessageIDByRemoteID(ctx context.Context, channelID, remoteMessageID string) (string, error) {
	var id string
	err := d.conn.QueryRowContext(ctx,
		`SELECT id FROM messages WHERE channel_id = ? AND remote_message_id = ? LIMIT 1;`,
		channelID, remoteMessageID,
	).Scan(&id)
	return id, err
}
