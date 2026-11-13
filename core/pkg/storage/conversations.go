package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/cestila/ConduitOmniChat/core/pkg/domain"
	conduitv1 "github.com/cestila/ConduitOmniChat/sdk/conduitv1"
)

// PollDomainToProto convierte un domain.Poll en conduitv1.ProtoPoll.
func PollDomainToProto(p *domain.Poll) *conduitv1.ProtoPoll {
	if p == nil {
		return nil
	}
	var opts []*conduitv1.ProtoPollOption
	for _, o := range p.Options {
		opts = append(opts, &conduitv1.ProtoPollOption{
			Id:        o.ID,
			Text:      o.Text,
			VoteCount: int32(o.VoteCount),
			Voters:    o.Voters,
			Chosen:    o.Chosen,
		})
	}
	return &conduitv1.ProtoPoll{
		Question:        p.Question,
		Options:         opts,
		SelectableCount: int32(p.SelectableCount),
		IsClosed:        p.IsClosed,
		TotalVotes:      int32(p.TotalVotes),
		IsQuiz:          p.IsQuiz,
	}
}

// PollProtoToDomain convierte un conduitv1.ProtoPoll en domain.Poll.
func PollProtoToDomain(p *conduitv1.ProtoPoll) *domain.Poll {
	if p == nil {
		return nil
	}
	var opts []domain.PollOption
	for _, o := range p.Options {
		opts = append(opts, domain.PollOption{
			ID:        o.Id,
			Text:      o.Text,
			VoteCount: int(o.VoteCount),
			Voters:    o.Voters,
			Chosen:    o.Chosen,
		})
	}
	return &domain.Poll{
		Question:        p.Question,
		Options:         opts,
		SelectableCount: int(p.SelectableCount),
		IsClosed:        p.IsClosed,
		TotalVotes:      int(p.TotalVotes),
		IsQuiz:          p.IsQuiz,
	}
}

// ButtonRowsDomainToProto convierte []domain.ButtonRow a []*conduitv1.ProtoButtonRow.
func ButtonRowsDomainToProto(rows []domain.ButtonRow) []*conduitv1.ProtoButtonRow {
	var protoRows []*conduitv1.ProtoButtonRow
	for _, r := range rows {
		var btns []*conduitv1.ProtoButton
		for _, b := range r.Buttons {
			btns = append(btns, &conduitv1.ProtoButton{
				Id:   b.ID,
				Text: b.Text,
				Type: b.Type,
				Url:  b.URL,
			})
		}
		protoRows = append(protoRows, &conduitv1.ProtoButtonRow{Buttons: btns})
	}
	return protoRows
}

// ButtonRowsProtoToDomain convierte []*conduitv1.ProtoButtonRow a []domain.ButtonRow.
func ButtonRowsProtoToDomain(protoRows []*conduitv1.ProtoButtonRow) []domain.ButtonRow {
	var rows []domain.ButtonRow
	for _, r := range protoRows {
		var btns []domain.Button
		for _, b := range r.Buttons {
			btns = append(btns, domain.Button{
				ID:   b.Id,
				Text: b.Text,
				Type: b.Type,
				URL:  b.Url,
			})
		}
		rows = append(rows, domain.ButtonRow{Buttons: btns})
	}
	return rows
}

// EnsureConversation existe o crea una conversación vinculada a un contacto o canal/servidor.
func (d *DB) EnsureConversation(ctx context.Context, conv *domain.Conversation) error {
	query := `
	INSERT INTO conversations (id, contact_id, is_group, title, last_message_at, active_channel_id, server_id, server_name, parent_channel_id, channel_type, avatar_url, position)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		title=CASE 
			WHEN excluded.title != '' AND excluded.title != excluded.id AND excluded.title NOT LIKE '%@g.us' AND excluded.title NOT LIKE '%@s.whatsapp.net' THEN excluded.title 
			ELSE conversations.title 
		END,
		last_message_at=CASE 
			WHEN excluded.last_message_at > conversations.last_message_at THEN excluded.last_message_at 
			ELSE conversations.last_message_at 
		END,
		active_channel_id=coalesce(conversations.active_channel_id, excluded.active_channel_id),
		server_id=coalesce(excluded.server_id, conversations.server_id),
		server_name=coalesce(excluded.server_name, conversations.server_name),
		parent_channel_id=coalesce(excluded.parent_channel_id, conversations.parent_channel_id),
		channel_type=coalesce(excluded.channel_type, conversations.channel_type),
		avatar_url=CASE 
			WHEN excluded.avatar_url != '' THEN excluded.avatar_url 
			ELSE conversations.avatar_url 
		END,
		position=CASE
			WHEN excluded.position > 0 THEN excluded.position
			ELSE conversations.position
		END;
	`
	lastMsg := conv.LastMessageAt
	if lastMsg.IsZero() {
		lastMsg = time.Unix(0, 0)
	}
	_, err := d.conn.ExecContext(ctx, query, conv.ID, conv.ContactID, conv.IsGroup, conv.Title, stripMonotonic(lastMsg), conv.ActiveChannelID, conv.ServerID, conv.ServerName, conv.ParentChannelID, conv.ChannelType, conv.AvatarURL, conv.Position)
	return err
}

// NextChannelPosition devuelve la siguiente posición libre para un canal nuevo del servidor.
// El orden lo dicta el servidor: se respeta el orden de publicación, no la actividad.
func (d *DB) NextChannelPosition(ctx context.Context, serverID string) (int32, error) {
	var next int32
	err := d.conn.QueryRowContext(ctx,
		"SELECT COALESCE(MAX(position), 0) + 1 FROM conversations WHERE server_id = ?",
		serverID).Scan(&next)
	if err != nil {
		return 0, fmt.Errorf("error calculando la posición del canal: %w", err)
	}
	return next, nil
}

// GetConversationByID obtiene una conversación por su ID.
func (d *DB) GetConversationByID(ctx context.Context, id string) (*conduitv1.ProtoConversation, error) {
	query := `SELECT id, contact_id, is_group, title, last_message_at, active_channel_id, coalesce(server_id, ''), coalesce(server_name, ''), coalesce(parent_channel_id, ''), coalesce(channel_type, ''), coalesce(avatar_url, ''), coalesce(position, 0) FROM conversations WHERE id = ?`
	row := d.conn.QueryRowContext(ctx, query, id)

	var c conduitv1.ProtoConversation
	var contactID, activeChan sql.NullString
	var lastMsg time.Time
	if err := row.Scan(&c.Id, &contactID, &c.IsGroup, &c.Title, &lastMsg, &activeChan, &c.ServerId, &c.ServerName, &c.ParentChannelId, &c.ChannelType, &c.AvatarUrl, &c.Position); err != nil {
		return nil, err
	}
	if contactID.Valid {
		c.ContactId = contactID.String
	}
	if activeChan.Valid {
		c.ActiveChannelId = activeChan.String
	}
	c.LastMessageAtUnix = lastMsg.Unix()
	return &c, nil
}

// ListConversations retorna las conversaciones ordenadas por actividad reciente de mensajes,
// mostrando arriba los que tienen mensajes más nuevos y extrayendo un snippet del último mensaje.
func (d *DB) ListConversations(ctx context.Context, limit, offset int) ([]*conduitv1.ProtoConversation, error) {
	if limit <= 0 {
		limit = 100
	}
	query := `
	SELECT 
		c.id, 
		c.contact_id, 
		c.is_group, 
		c.title, 
		coalesce(max(m.sent_at), c.last_message_at) as effective_last_msg_at, 
		c.active_channel_id,
		coalesce((SELECT body FROM messages m2 WHERE m2.conversation_id = c.id ORDER BY m2.sent_at DESC LIMIT 1), '') as snippet,
		(SELECT count(*) FROM messages m3 WHERE m3.conversation_id = c.id) as msg_count,
		coalesce((SELECT count(*) FROM messages m4 WHERE m4.conversation_id = c.id AND m4.status != 'read' AND m4.sender_type NOT IN ('me', 'USER', 'OUTGOING')), 0) as unread_count,
		coalesce(c.server_id, '') as server_id,
		coalesce(c.server_name, '') as server_name,
		coalesce(c.parent_channel_id, '') as parent_channel_id,
		coalesce(c.channel_type, '') as channel_type,
		coalesce(nullif(c.avatar_url, ''), (SELECT avatar_url FROM contacts ct WHERE ct.id = c.contact_id), '') as avatar_url,
		coalesce(c.position, 0) as position,
		coalesce(
			(SELECT ch.provider FROM contact_channels ch WHERE ch.id = c.active_channel_id),
			(SELECT ch.provider FROM contact_channels ch WHERE ch.contact_id = c.contact_id ORDER BY ch.is_preferred DESC LIMIT 1),
			''
		) as provider
	FROM conversations c
	LEFT JOIN messages m ON c.id = m.conversation_id
	GROUP BY c.id
	ORDER BY (msg_count > 0) DESC, effective_last_msg_at DESC, c.title ASC
	LIMIT ? OFFSET ?;
	`
	rows, err := d.conn.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("error consultando conversaciones: %w", err)
	}
	defer rows.Close()

	var list []*conduitv1.ProtoConversation
	for rows.Next() {
		var c conduitv1.ProtoConversation
		var contactID, activeChan sql.NullString
		var lastMsgRaw any
		var snippet string
		var msgCount int
		var unreadCount int
		if err := rows.Scan(&c.Id, &contactID, &c.IsGroup, &c.Title, &lastMsgRaw, &activeChan, &snippet, &msgCount, &unreadCount, &c.ServerId, &c.ServerName, &c.ParentChannelId, &c.ChannelType, &c.AvatarUrl, &c.Position, &c.Provider); err != nil {
			return nil, err
		}
		c.UnreadCount = int32(unreadCount)
		if contactID.Valid {
			c.ContactId = contactID.String
		}
		if activeChan.Valid {
			c.ActiveChannelId = activeChan.String
		}
		if lastMsgRaw != nil {
			switch v := lastMsgRaw.(type) {
			case time.Time:
				c.LastMessageAtUnix = v.Unix()
			case string:
				if t, ok := parseStoredTime(v); ok {
					c.LastMessageAtUnix = t.Unix()
				}
			}
		}
		c.LastMessagePreview = snippet
		list = append(list, &c)
	}

	// Asegurar que para todos los servidores presentes en la lista,
	// se incluyan TODOS sus canales/temas asociados, aunque no tengan mensajes recientes
	// o hayan quedado fuera del LIMIT por paginación.
	serverIDsMap := make(map[string]bool)
	presentIDs := make(map[string]bool)
	for _, conv := range list {
		presentIDs[conv.Id] = true
		if conv.ServerId != "" {
			serverIDsMap[conv.ServerId] = true
		}
	}

	if len(serverIDsMap) > 0 {
		var serverIDs []string
		for sid := range serverIDsMap {
			serverIDs = append(serverIDs, sid)
		}
		siblingChannels, err := d.GetChannelsByServerIDs(ctx, serverIDs)
		if err == nil {
			for _, ch := range siblingChannels {
				if !presentIDs[ch.Id] {
					presentIDs[ch.Id] = true
					list = append(list, ch)
				}
			}
		}
	}

	return list, nil
}

// GetChannelsByServerIDs devuelve todas las conversaciones/canales que pertenecen a una lista de servidores.
func (d *DB) GetChannelsByServerIDs(ctx context.Context, serverIDs []string) ([]*conduitv1.ProtoConversation, error) {
	if len(serverIDs) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(serverIDs))
	args := make([]any, len(serverIDs))
	for i, sid := range serverIDs {
		placeholders[i] = "?"
		args[i] = sid
	}
	query := fmt.Sprintf(`
	SELECT 
		c.id, 
		c.contact_id, 
		c.is_group, 
		c.title, 
		coalesce(max(m.sent_at), c.last_message_at) as effective_last_msg_at, 
		c.active_channel_id,
		coalesce((SELECT body FROM messages m2 WHERE m2.conversation_id = c.id ORDER BY m2.sent_at DESC LIMIT 1), '') as snippet,
		(SELECT count(*) FROM messages m3 WHERE m3.conversation_id = c.id) as msg_count,
		coalesce((SELECT count(*) FROM messages m4 WHERE m4.conversation_id = c.id AND m4.status != 'read' AND m4.sender_type NOT IN ('me', 'USER', 'OUTGOING')), 0) as unread_count,
		coalesce(c.server_id, '') as server_id,
		coalesce(c.server_name, '') as server_name,
		coalesce(c.parent_channel_id, '') as parent_channel_id,
		coalesce(c.channel_type, '') as channel_type,
		coalesce(nullif(c.avatar_url, ''), (SELECT avatar_url FROM contacts ct WHERE ct.id = c.contact_id), '') as avatar_url,
		coalesce(c.position, 0) as position,
		coalesce(
			(SELECT ch.provider FROM contact_channels ch WHERE ch.id = c.active_channel_id),
			(SELECT ch.provider FROM contact_channels ch WHERE ch.contact_id = c.contact_id ORDER BY ch.is_preferred DESC LIMIT 1),
			''
		) as provider
	FROM conversations c
	LEFT JOIN messages m ON c.id = m.conversation_id
	WHERE c.server_id IN (%s)
	GROUP BY c.id
	ORDER BY c.position ASC, effective_last_msg_at DESC, c.title ASC;
	`, strings.Join(placeholders, ","))

	rows, err := d.conn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("error consultando canales de servidores: %w", err)
	}
	defer rows.Close()

	var list []*conduitv1.ProtoConversation
	for rows.Next() {
		var c conduitv1.ProtoConversation
		var contactID, activeChan sql.NullString
		var lastMsgRaw any
		var snippet string
		var msgCount int
		var unreadCount int
		if err := rows.Scan(&c.Id, &contactID, &c.IsGroup, &c.Title, &lastMsgRaw, &activeChan, &snippet, &msgCount, &unreadCount, &c.ServerId, &c.ServerName, &c.ParentChannelId, &c.ChannelType, &c.AvatarUrl, &c.Position, &c.Provider); err != nil {
			return nil, err
		}
		c.UnreadCount = int32(unreadCount)
		if contactID.Valid {
			c.ContactId = contactID.String
		}
		if activeChan.Valid {
			c.ActiveChannelId = activeChan.String
		}
		if lastMsgRaw != nil {
			switch v := lastMsgRaw.(type) {
			case time.Time:
				c.LastMessageAtUnix = v.Unix()
			case string:
				if t, ok := parseStoredTime(v); ok {
					c.LastMessageAtUnix = t.Unix()
				}
			}
		}
		c.LastMessagePreview = snippet
		list = append(list, &c)
	}
	return list, nil
}

// GetMessagesByConversation retorna los mensajes de un hilo/conversación ordenados cronológicamente,
// soportando paginación infinita con beforeMessageID (como Telegram y Discord).
func (d *DB) GetMessagesByConversation(ctx context.Context, convID string, limit int, beforeMessageID string) ([]*conduitv1.ProtoMessage, error) {
	if limit <= 0 {
		limit = 40
	}

	// El cursor es (sent_at, rowid): varios mensajes comparten el mismo segundo y
	// ordenar solo por sent_at los perdería o los repetiría al cambiar de página.
	var beforeSentAt any
	var beforeRowID int64
	hasCursor := false
	if beforeMessageID != "" {
		if err := d.conn.QueryRowContext(ctx,
			"SELECT sent_at, rowid FROM messages WHERE id = ? OR remote_message_id = ? ORDER BY rowid DESC LIMIT 1",
			beforeMessageID, beforeMessageID,
		).Scan(&beforeSentAt, &beforeRowID); err == nil {
			hasCursor = true
		}
	}

	var query string
	var args []any

	if hasCursor {
		query = `
		SELECT id, conversation_id, channel_id, remote_message_id, sender_type, body, content_type, status, sent_at, coalesce(reply_to_message_id, ''), coalesce(metadata, '')
		FROM (
			SELECT id, conversation_id, channel_id, remote_message_id, sender_type, body, content_type, status, sent_at, reply_to_message_id, metadata, rowid AS rid
			FROM messages
			WHERE conversation_id = ? AND (sent_at < ? OR (sent_at = ? AND rowid < ?))
			ORDER BY sent_at DESC, rowid DESC
			LIMIT ?
		)
		ORDER BY sent_at ASC, rid ASC;
		`
		args = []any{convID, beforeSentAt, beforeSentAt, beforeRowID, limit}
	} else {
		query = `
		SELECT id, conversation_id, channel_id, remote_message_id, sender_type, body, content_type, status, sent_at, coalesce(reply_to_message_id, ''), coalesce(metadata, '')
		FROM (
			SELECT id, conversation_id, channel_id, remote_message_id, sender_type, body, content_type, status, sent_at, reply_to_message_id, metadata, rowid AS rid
			FROM messages
			WHERE conversation_id = ?
			ORDER BY sent_at DESC, rowid DESC
			LIMIT ?
		)
		ORDER BY sent_at ASC, rid ASC;
		`
		args = []any{convID, limit}
	}

	rows, err := d.conn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("error consultando mensajes: %w", err)
	}
	defer rows.Close()

	var list []*conduitv1.ProtoMessage
	for rows.Next() {
		var m conduitv1.ProtoMessage
		var sentAtRaw any
		var replyTo, metaStr string
		if err := rows.Scan(&m.Id, &m.ConversationId, &m.ChannelId, &m.RemoteMessageId, &m.SenderType, &m.Body, &m.ContentType, &m.Status, &sentAtRaw, &replyTo, &metaStr); err != nil {
			return nil, err
		}
		m.ReplyToMessageId = replyTo
		if metaStr != "" {
			if strings.Contains(metaStr, `"question"`) {
				var p domain.Poll
				if err := json.Unmarshal([]byte(metaStr), &p); err == nil && p.Question != "" {
					m.Poll = PollDomainToProto(&p)
				}
			}
			if strings.Contains(metaStr, `"button_rows"`) {
				var bObj struct {
					ButtonRows []domain.ButtonRow `json:"button_rows"`
				}
				if err := json.Unmarshal([]byte(metaStr), &bObj); err == nil && len(bObj.ButtonRows) > 0 {
					m.ButtonRows = ButtonRowsDomainToProto(bObj.ButtonRows)
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
				m.EditedAtUnix = metaFields.EditedAtUnix
				m.IsPinned = metaFields.IsPinned
				if metaFields.Location != nil {
					m.Location = &conduitv1.ProtoLocation{
						Latitude:  metaFields.Location.Latitude,
						Longitude: metaFields.Location.Longitude,
						Title:     metaFields.Location.Title,
						Address:   metaFields.Location.Address,
					}
				}
				if metaFields.SharedContact != nil {
					m.SharedContact = &conduitv1.ProtoSharedContact{
						DisplayName: metaFields.SharedContact.DisplayName,
						PhoneNumber: metaFields.SharedContact.PhoneNumber,
						Vcard:       metaFields.SharedContact.Vcard,
					}
				}
			}
		}
		if sentAtRaw != nil {
			switch v := sentAtRaw.(type) {
			case time.Time:
				m.SentAtUnix = v.Unix()
			case string:
				formats := []string{
					"2006-01-02 15:04:05.999999999 -0700 MST",
					"2006-01-02 15:04:05.999999999 -0700 -07",
					"2006-01-02 15:04:05 -0700 MST",
					"2006-01-02 15:04:05 -0700 -07",
					"2006-01-02 15:04:05",
					time.RFC3339,
					time.RFC3339Nano,
				}
				for _, f := range formats {
					if t, err := time.Parse(f, v); err == nil {
						m.SentAtUnix = t.Unix()
						break
					}
				}
				if m.SentAtUnix == 0 && len(v) >= 19 {
					if t, err := time.Parse("2006-01-02 15:04:05", v[:19]); err == nil {
						m.SentAtUnix = t.Unix()
					}
				}
			}
		}
		list = append(list, &m)
	}

	if len(list) > 0 {
		msgIDs := make([]string, len(list))
		parentIDsMap := make(map[string]bool)
		for i, m := range list {
			msgIDs[i] = m.Id
			if m.ReplyToMessageId != "" {
				parentIDsMap[m.ReplyToMessageId] = true
			}
		}

		// 1. Cargar y vincular adjuntos
		attsMap, err := d.GetAttachmentsByMessageIDs(ctx, msgIDs)
		if err == nil && len(attsMap) > 0 {
			for _, m := range list {
				if atts, exists := attsMap[m.Id]; exists {
					for _, a := range atts {
						locPath := ""
						if a.LocalPath != nil {
							locPath = *a.LocalPath
						}
						m.Attachments = append(m.Attachments, &conduitv1.ProtoAttachment{
							Id:             a.ID,
							MessageId:      a.MessageID,
							FileName:       a.FileName,
							MimeType:       a.MimeType,
							FileSize:       a.FileSize,
							LocalPath:      locPath,
							DownloadStatus: string(a.DownloadStatus),
							Checksum:       a.Checksum,
						})
					}
				}
			}
		}

		// 2. Cargar vistas previas de mensajes citados (reply_to_sender y reply_to_body)
		if len(parentIDsMap) > 0 {
			var pIDs []string
			var pArgs []any
			for pID := range parentIDsMap {
				pIDs = append(pIDs, "?")
				pArgs = append(pArgs, pID)
			}
			pQuery := fmt.Sprintf(`SELECT id, sender_type, coalesce(body, '') FROM messages WHERE id IN (%s)`, strings.Join(pIDs, ","))
			if pRows, err := d.conn.QueryContext(ctx, pQuery, pArgs...); err == nil {
				type parentInfo struct{ sender, body string }
				parents := make(map[string]parentInfo)
				for pRows.Next() {
					var id, sType, b string
					if err := pRows.Scan(&id, &sType, &b); err == nil {
						parents[id] = parentInfo{sender: sType, body: b}
					}
				}
				pRows.Close()

				for _, m := range list {
					if m.ReplyToMessageId != "" {
						if pi, ok := parents[m.ReplyToMessageId]; ok {
							m.ReplyToSender = pi.sender
							m.ReplyToBody = pi.body
						}
					}
				}
			}
		}

		// 3. Cargar reacciones asociadas
		reactsMap, err := d.GetReactionsByMessageIDs(ctx, msgIDs)
		if err == nil && len(reactsMap) > 0 {
			for _, m := range list {
				if rList, exists := reactsMap[m.Id]; exists {
					for _, r := range rList {
						m.Reactions = append(m.Reactions, &conduitv1.ProtoReaction{
							Emoji:      r.Emoji,
							Count:      int32(r.Count),
							Senders:    r.Senders,
							IncludesMe: r.IncludesMe,
						})
					}
				}
			}
		}
	}

	return list, nil
}

// GetPinnedMessagesByConversation devuelve todos los mensajes fijados de una conversación ordenados cronológicamente.
func (d *DB) GetPinnedMessagesByConversation(ctx context.Context, convID string) ([]*conduitv1.ProtoMessage, error) {
	query := `
	SELECT id, conversation_id, channel_id, remote_message_id, sender_type, body, content_type, status, sent_at, coalesce(reply_to_message_id, ''), coalesce(metadata, '')
	FROM messages
	WHERE conversation_id = ? 
	  AND metadata IS NOT NULL 
	  AND metadata != '' 
	  AND json_valid(metadata) = 1 
	  AND json_extract(metadata, '$.is_pinned') IN (1, 'true', true)
	ORDER BY sent_at ASC;
	`
	rows, err := d.conn.QueryContext(ctx, query, convID)
	if err != nil {
		return nil, fmt.Errorf("error consultando mensajes fijados: %w", err)
	}
	defer rows.Close()

	var list []*conduitv1.ProtoMessage
	for rows.Next() {
		var m conduitv1.ProtoMessage
		var sentAtRaw any
		var replyTo, metaStr string
		if err := rows.Scan(&m.Id, &m.ConversationId, &m.ChannelId, &m.RemoteMessageId, &m.SenderType, &m.Body, &m.ContentType, &m.Status, &sentAtRaw, &replyTo, &metaStr); err != nil {
			return nil, err
		}
		m.ReplyToMessageId = replyTo
		if metaStr != "" {
			if strings.Contains(metaStr, `"question"`) {
				var p domain.Poll
				if err := json.Unmarshal([]byte(metaStr), &p); err == nil && p.Question != "" {
					m.Poll = PollDomainToProto(&p)
				}
			}
			if strings.Contains(metaStr, `"button_rows"`) {
				var bObj struct {
					ButtonRows []domain.ButtonRow `json:"button_rows"`
				}
				if err := json.Unmarshal([]byte(metaStr), &bObj); err == nil && len(bObj.ButtonRows) > 0 {
					m.ButtonRows = ButtonRowsDomainToProto(bObj.ButtonRows)
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
				m.EditedAtUnix = metaFields.EditedAtUnix
				m.IsPinned = metaFields.IsPinned
				if metaFields.Location != nil {
					m.Location = &conduitv1.ProtoLocation{
						Latitude:  metaFields.Location.Latitude,
						Longitude: metaFields.Location.Longitude,
						Title:     metaFields.Location.Title,
						Address:   metaFields.Location.Address,
					}
				}
				if metaFields.SharedContact != nil {
					m.SharedContact = &conduitv1.ProtoSharedContact{
						DisplayName: metaFields.SharedContact.DisplayName,
						PhoneNumber: metaFields.SharedContact.PhoneNumber,
						Vcard:       metaFields.SharedContact.Vcard,
					}
				}
			}
		}
		if sentAtRaw != nil {
			switch v := sentAtRaw.(type) {
			case time.Time:
				m.SentAtUnix = v.Unix()
			case string:
				formats := []string{
					"2006-01-02 15:04:05.999999999 -0700 MST",
					"2006-01-02 15:04:05.999999999 -0700 -07",
					"2006-01-02 15:04:05 -0700 MST",
					"2006-01-02 15:04:05 -0700 -07",
					"2006-01-02 15:04:05",
					time.RFC3339,
					time.RFC3339Nano,
				}
				for _, f := range formats {
					if t, err := time.Parse(f, v); err == nil {
						m.SentAtUnix = t.Unix()
						break
					}
				}
				if m.SentAtUnix == 0 && len(v) >= 19 {
					if t, err := time.Parse("2006-01-02 15:04:05", v[:19]); err == nil {
						m.SentAtUnix = t.Unix()
					}
				}
			}
		}
		list = append(list, &m)
	}

	return list, nil
}

// MarkMessagesAsRead marca como leídos todos los mensajes recibidos de una conversación.
func (d *DB) MarkMessagesAsRead(ctx context.Context, convID string) error {
	query := `
	UPDATE messages 
	SET status = 'read', read_sync_pending = 0
	WHERE conversation_id = ? AND (status != 'read' OR read_sync_pending = 1) AND sender_type NOT IN ('me', 'USER', 'OUTGOING');
	`
	_, err := d.conn.ExecContext(ctx, query, convID)
	return err
}

// GetUnreadRemoteMessageIDs devuelve los IDs remotos de los mensajes recibidos no leídos de una conversación.
func (d *DB) GetUnreadRemoteMessageIDs(ctx context.Context, convID string) ([]string, error) {
	query := `
	SELECT remote_message_id FROM messages
	WHERE conversation_id = ? AND status != 'read' AND sender_type NOT IN ('me', 'USER', 'OUTGOING')
	ORDER BY sent_at ASC
	`
	rows, err := d.conn.QueryContext(ctx, query, convID)
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
		if id != "" {
			ids = append(ids, id)
		}
	}
	return ids, rows.Err()
}

// MarkMessagesAsReadAndReturnRemoteIDs marca como leídos los mensajes no leídos y retorna sus IDs remotos.
func (d *DB) MarkMessagesAsReadAndReturnRemoteIDs(ctx context.Context, convID string) ([]string, error) {
	ids, err := d.GetUnreadRemoteMessageIDs(ctx, convID)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}
	if err := d.MarkMessagesAsRead(ctx, convID); err != nil {
		return nil, err
	}
	return ids, nil
}

// MarkMessagesAsReadByRemoteIDs marca como leídos mensajes específicos por su remote_message_id en un canal.
func (d *DB) MarkMessagesAsReadByRemoteIDs(ctx context.Context, channelID string, remoteIDs []string) (int64, error) {
	if len(remoteIDs) == 0 {
		return 0, nil
	}
	placeholders := make([]string, len(remoteIDs))
	args := make([]interface{}, 0, len(remoteIDs)+1)
	args = append(args, channelID)
	for i, id := range remoteIDs {
		placeholders[i] = "?"
		args = append(args, id)
	}

	query := fmt.Sprintf(`
	UPDATE messages
	SET status = 'read', read_sync_pending = 0
	WHERE channel_id = ? AND remote_message_id IN (%s) AND (status != 'read' OR read_sync_pending = 1)
	`, strings.Join(placeholders, ","))

	res, err := d.conn.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// MarkMessagesAsReadUpToRemoteID marca como leídos todos los mensajes de un canal hasta un remote_message_id (inclusive, por sent_at o comparación numérica).
func (d *DB) MarkMessagesAsReadUpToRemoteID(ctx context.Context, channelID string, maxRemoteID string) (int64, error) {
	// 1. Si el remote_message_id es numérico (Telegram), marcar por comparación numérica directa
	if _, err := strconv.ParseInt(maxRemoteID, 10, 64); err == nil {
		query := `
		UPDATE messages
		SET status = 'read', read_sync_pending = 0
		WHERE channel_id = ? AND CAST(remote_message_id AS INTEGER) <= CAST(? AS INTEGER) AND (status != 'read' OR read_sync_pending = 1) AND sender_type NOT IN ('me', 'USER', 'OUTGOING')
		`
		res, err := d.conn.ExecContext(ctx, query, channelID, maxRemoteID)
		if err == nil {
			return res.RowsAffected()
		}
	}

	// 2. Si no es puramente numérico, buscar la fecha del mensaje de corte
	var maxSentAt time.Time
	err := d.conn.QueryRowContext(ctx, "SELECT sent_at FROM messages WHERE channel_id = ? AND remote_message_id = ?", channelID, maxRemoteID).Scan(&maxSentAt)
	if err != nil {
		// Fallback si no encontramos el mensaje por remote_id: marcar el remoteID específico si existe
		return d.MarkMessagesAsReadByRemoteIDs(ctx, channelID, []string{maxRemoteID})
	}

	query := `
	UPDATE messages
	SET status = 'read', read_sync_pending = 0
	WHERE channel_id = ? AND sent_at <= ? AND (status != 'read' OR read_sync_pending = 1) AND sender_type NOT IN ('me', 'USER', 'OUTGOING')
	`
	res, err := d.conn.ExecContext(ctx, query, channelID, maxSentAt)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ListDMConversationIDsByContact devuelve los IDs de las conversaciones 1 a 1 de un contacto.
// Quedan fuera grupos, canales y servidores.
func (d *DB) ListDMConversationIDsByContact(ctx context.Context, contactID string) ([]string, error) {
	rows, err := d.conn.QueryContext(ctx, `
		SELECT id FROM conversations
		WHERE contact_id = ? AND is_group = 0
		  AND coalesce(server_id, '') = ''
		  AND coalesce(parent_channel_id, '') = ''
		ORDER BY id ASC`, contactID)
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
	return ids, rows.Err()
}

// MoveMessages reasigna todos los mensajes de una conversación a otra.
func (d *DB) MoveMessages(ctx context.Context, fromConversationID, toConversationID string) (int64, error) {
	result, err := d.conn.ExecContext(ctx, `UPDATE messages SET conversation_id = ? WHERE conversation_id = ?`, toConversationID, fromConversationID)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// DeleteConversation elimina una conversación.
func (d *DB) DeleteConversation(ctx context.Context, conversationID string) error {
	_, err := d.conn.ExecContext(ctx, `DELETE FROM conversations WHERE id = ?`, conversationID)
	return err
}

// LatestChannelIDForConversation devuelve el canal del mensaje más reciente de la conversación.
func (d *DB) LatestChannelIDForConversation(ctx context.Context, conversationID string) (string, error) {
	var channelID string
	err := d.conn.QueryRowContext(ctx, `
		SELECT channel_id FROM messages
		WHERE conversation_id = ?
		ORDER BY sent_at DESC, rowid DESC
		LIMIT 1`, conversationID).Scan(&channelID)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return channelID, err
}

// SetConversationActiveChannel fija el canal sticky de una conversación.
func (d *DB) SetConversationActiveChannel(ctx context.Context, conversationID, channelID string) error {
	_, err := d.conn.ExecContext(ctx, `UPDATE conversations SET active_channel_id = ? WHERE id = ?`, channelID, conversationID)
	return err
}

// RecomputeConversationActivity recalcula last_message_at desde los mensajes reales.
func (d *DB) RecomputeConversationActivity(ctx context.Context, conversationID string) error {
	_, err := d.conn.ExecContext(ctx, `
		UPDATE conversations
		SET last_message_at = coalesce(
			(SELECT max(sent_at) FROM messages WHERE conversation_id = ?),
			'1970-01-01 00:00:00'
		)
		WHERE id = ?`, conversationID, conversationID)
	return err
}

// ConsolidateDMConversations deja un solo hilo 1 a 1 por contacto.
// El hilo canónico tiene id = contact_id; los mensajes de cualquier conversación
// duplicada se mueven allí, las filas duplicadas se eliminan y el sticky queda en
// el canal del último mensaje.
func (d *DB) ConsolidateDMConversations(ctx context.Context, contactID string) error {
	var displayName string
	err := d.conn.QueryRowContext(ctx, `SELECT display_name FROM contacts WHERE id = ?`, contactID).Scan(&displayName)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}

	conversationIDs, err := d.ListDMConversationIDsByContact(ctx, contactID)
	if err != nil {
		return err
	}

	hasCanonical := false
	for _, id := range conversationIDs {
		if id == contactID {
			hasCanonical = true
			break
		}
	}

	if !hasCanonical {
		var activeChannelID *string
		var channelID string
		if err := d.conn.QueryRowContext(ctx, `SELECT id FROM contact_channels WHERE contact_id = ? ORDER BY is_preferred DESC, id ASC LIMIT 1`, contactID).Scan(&channelID); err == nil {
			activeChannelID = &channelID
		}
		if err := d.EnsureConversation(ctx, &domain.Conversation{
			ID:              contactID,
			ContactID:       &contactID,
			IsGroup:         false,
			Title:           displayName,
			ActiveChannelID: activeChannelID,
		}); err != nil {
			return err
		}
	}

	for _, id := range conversationIDs {
		if id == contactID {
			continue
		}
		if _, err := d.MoveMessages(ctx, id, contactID); err != nil {
			return err
		}
		if err := d.DeleteConversation(ctx, id); err != nil {
			return err
		}
	}

	if channelID, err := d.LatestChannelIDForConversation(ctx, contactID); err == nil && channelID != "" {
		_ = d.SetConversationActiveChannel(ctx, contactID, channelID)
	}

	return d.RecomputeConversationActivity(ctx, contactID)
}

// ConsolidateDuplicateDMConversations consolida todos los contactos que quedaron
// con más de un hilo 1 a 1. Es idempotente: se puede repetir en cada arranque.
func (d *DB) ConsolidateDuplicateDMConversations(ctx context.Context) error {
	contactIDs, err := d.ListContactIDsWithDuplicateDMConversations(ctx)
	if err != nil {
		return err
	}
	for _, contactID := range contactIDs {
		if err := d.ConsolidateDMConversations(ctx, contactID); err != nil {
			return err
		}
	}
	return nil
}

// ListContactIDsWithDuplicateDMConversations devuelve los contactos con más de un hilo 1 a 1.
func (d *DB) ListContactIDsWithDuplicateDMConversations(ctx context.Context) ([]string, error) {
	rows, err := d.conn.QueryContext(ctx, `
		SELECT contact_id FROM conversations
		WHERE contact_id IS NOT NULL AND contact_id <> ''
		  AND is_group = 0
		  AND coalesce(server_id, '') = ''
		  AND coalesce(parent_channel_id, '') = ''
		GROUP BY contact_id
		HAVING count(*) > 1
		ORDER BY contact_id ASC`)
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
	return ids, rows.Err()
}
