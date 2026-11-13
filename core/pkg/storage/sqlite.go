package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cestila/ConduitOmniChat/core/pkg/domain"
	_ "modernc.org/sqlite"
)

// DB envuelve la conexión a la base de datos SQLite.
type DB struct {
	conn *sql.DB
}

// NewDB inicializa o abre la base de datos SQLite y ejecuta las migraciones DDL.
func NewDB(dbPath string) (*DB, error) {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("no se pudo crear el directorio de la base de datos: %w", err)
	}

	conn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("error al abrir base de datos sqlite: %w", err)
	}

	// modernc.org/sqlite con WAL mode necesita que busy_timeout esté configurado en el DSN
	// o que la conexión no se bloquee a sí misma en iteraciones sobre rows activas.
	// Limitar MaxOpenConns a 10 y permitir un busy_timeout generoso evita SQLITE_BUSY.
	conn.SetMaxOpenConns(10)
	conn.SetMaxIdleConns(5)

	// Configurar pragmas para concurrencia WAL sin bloqueos
	_, _ = conn.Exec("PRAGMA journal_mode = WAL;")
	_, _ = conn.Exec("PRAGMA busy_timeout = 10000;")
	_, _ = conn.Exec("PRAGMA synchronous = NORMAL;")

	db := &DB{conn: conn}
	if err := db.migrate(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("error al ejecutar migraciones: %w", err)
	}

	return db, nil
}

// Close cierra la conexión.
func (d *DB) Close() error {
	return d.conn.Close()
}

// QueryRowContext ejecuta una consulta de fila única contra la base de datos SQLite.
func (d *DB) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return d.conn.QueryRowContext(ctx, query, args...)
}

func (d *DB) migrate() error {
	query := `
	CREATE TABLE IF NOT EXISTS contacts (
		id TEXT PRIMARY KEY,
		display_name TEXT NOT NULL,
		avatar_url TEXT,
		email TEXT,
		phone TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS contact_channels (
		id TEXT PRIMARY KEY,
		contact_id TEXT NOT NULL REFERENCES contacts(id) ON DELETE CASCADE,
		provider TEXT NOT NULL,
		remote_id TEXT NOT NULL,
		handle TEXT,
		is_preferred BOOLEAN DEFAULT FALSE,
		metadata TEXT,
		UNIQUE(provider, remote_id)
	);

	CREATE TABLE IF NOT EXISTS conversations (
		id TEXT PRIMARY KEY,
		contact_id TEXT REFERENCES contacts(id),
		is_group BOOLEAN DEFAULT FALSE,
		title TEXT NOT NULL,
		last_message_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		active_channel_id TEXT REFERENCES contact_channels(id),
		server_id TEXT,
		server_name TEXT,
		parent_channel_id TEXT,
		channel_type TEXT,
		avatar_url TEXT,
		position INTEGER DEFAULT 0
	);

	CREATE TABLE IF NOT EXISTS messages (
		id TEXT PRIMARY KEY,
		conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
		channel_id TEXT NOT NULL REFERENCES contact_channels(id),
		remote_message_id TEXT NOT NULL,
		sender_type TEXT NOT NULL,
		body TEXT,
		content_type TEXT NOT NULL,
		status TEXT NOT NULL,
		sent_at DATETIME NOT NULL,
		received_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		reply_to_message_id TEXT REFERENCES messages(id),
		metadata TEXT,
		UNIQUE(channel_id, remote_message_id)
	);

	CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts5(
		body,
		content='messages',
		content_rowid='rowid'
	);

	-- Triggers para mantener FTS5 sincronizado
	CREATE TRIGGER IF NOT EXISTS messages_ai AFTER INSERT ON messages BEGIN
		INSERT INTO messages_fts(rowid, body) VALUES (new.rowid, new.body);
	END;

	CREATE TRIGGER IF NOT EXISTS messages_ad AFTER DELETE ON messages BEGIN
		INSERT INTO messages_fts(messages_fts, rowid, body) VALUES('delete', old.rowid, old.body);
	END;

	CREATE TRIGGER IF NOT EXISTS messages_au AFTER UPDATE OF body ON messages BEGIN
		INSERT INTO messages_fts(messages_fts, rowid, body) VALUES('delete', old.rowid, old.body);
		INSERT INTO messages_fts(rowid, body) VALUES (new.rowid, new.body);
	END;

	CREATE TABLE IF NOT EXISTS attachments (
		id TEXT PRIMARY KEY,
		message_id TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
		file_name TEXT,
		mime_type TEXT NOT NULL,
		file_size INTEGER,
		local_path TEXT,
		download_status TEXT NOT NULL,
		checksum TEXT,
		expires_at DATETIME
	);

	CREATE TABLE IF NOT EXISTS message_reactions (
		message_id TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
		sender_id TEXT NOT NULL,
		emoji TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (message_id, sender_id, emoji)
	);
	`
	if _, err := d.conn.Exec(query); err != nil {
		return err
	}

	// Migraciones seguras para bases de datos existentes
	_, _ = d.conn.Exec("ALTER TABLE conversations ADD COLUMN server_id TEXT;")
	_, _ = d.conn.Exec("ALTER TABLE conversations ADD COLUMN server_name TEXT;")
	_, _ = d.conn.Exec("ALTER TABLE conversations ADD COLUMN parent_channel_id TEXT;")
	_, _ = d.conn.Exec("ALTER TABLE conversations ADD COLUMN channel_type TEXT;")
	_, _ = d.conn.Exec("ALTER TABLE conversations ADD COLUMN avatar_url TEXT;")
	_, _ = d.conn.Exec("ALTER TABLE conversations ADD COLUMN position INTEGER DEFAULT 0;")
	_, _ = d.conn.Exec("ALTER TABLE contacts ADD COLUMN email TEXT;")
	_, _ = d.conn.Exec("ALTER TABLE contacts ADD COLUMN phone TEXT;")
	// Remitente remoto de cada mensaje (WhatsApp exige el participante para marcar leídos en grupos)
	// y marca de leído local todavía no confirmado por el proveedor.
	_, _ = d.conn.Exec("ALTER TABLE messages ADD COLUMN sender_remote_id TEXT;")
	_, _ = d.conn.Exec("ALTER TABLE messages ADD COLUMN read_sync_pending INTEGER NOT NULL DEFAULT 0;")
	_, _ = d.conn.Exec("ALTER TABLE attachments ADD COLUMN remote_ref TEXT;")
	_, _ = d.conn.Exec("CREATE INDEX IF NOT EXISTS idx_messages_read_sync ON messages(conversation_id) WHERE read_sync_pending = 1;")

	// Una identidad canónica (email/teléfono) no puede pertenecer a dos contactos.
	// El índice es parcial: los contactos sin identidad asignada no participan.
	_, _ = d.conn.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_contacts_email_identity ON contacts(email) WHERE email IS NOT NULL AND email <> '';")
	_, _ = d.conn.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_contacts_phone_identity ON contacts(phone) WHERE phone IS NOT NULL AND phone <> '';")

	// Índices de alto rendimiento para consultas inmediatas
	_, _ = d.conn.Exec("CREATE INDEX IF NOT EXISTS idx_messages_conv_sent ON messages(conversation_id, sent_at);")
	_, _ = d.conn.Exec("CREATE INDEX IF NOT EXISTS idx_conversations_last_msg ON conversations(last_message_at DESC);")
	_, _ = d.conn.Exec("CREATE INDEX IF NOT EXISTS idx_reactions_message ON message_reactions(message_id);")

	// Reparar temas de foro en bases existentes que hayan quedado con channel_type='group' o título pisado por el servidor
	_, _ = d.conn.Exec(`
		UPDATE conversations
		SET channel_type = 'forum',
		    parent_channel_id = server_id
		WHERE id LIKE '%_topic_%'
		  AND (channel_type = 'group' OR coalesce(parent_channel_id, '') = '');
	`)
	_, _ = d.conn.Exec(`
		UPDATE conversations
		SET title = 'Tema ' || substr(id, instr(id, '_topic_') + 7)
		WHERE id LIKE '%_topic_%'
		  AND (title = server_name OR title = '' OR title = id);
	`)

	if err := d.cleanupAndMergeWhatsAppLIDs(); err != nil {
		fmt.Printf("[storage] advertencia en migración y unificación de LIDs: %v\n", err)
	}

	// Bases anteriores a la unificación de hilos pueden tener varias conversaciones
	// 1 a 1 para el mismo contacto. Se consolidan en cada arranque (idempotente).
	if err := d.ConsolidateDuplicateDMConversations(context.Background()); err != nil {
		fmt.Printf("[storage] advertencia consolidando hilos duplicados: %v\n", err)
	}

	return d.EnsureAccountAndSettingsTables()
}

// cleanupAndMergeWhatsAppLIDs unifica contactos y conversaciones duplicadas entre canales @lid y @s.whatsapp.net,
// y restablece el last_message_at de conversaciones de contactos sin mensajes al timestamp real de su último mensaje.
func (d *DB) cleanupAndMergeWhatsAppLIDs() error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	waDbPath := filepath.Join(homeDir, ".local", "share", "conduit", "whatsapp", "session.db")
	if _, err := os.Stat(waDbPath); err != nil {
		// No existe aún sesión de WhatsApp, nada que migrar
		return nil
	}

	// Adjuntar la base de datos de whatsmeow temporalmente
	attachQuery := fmt.Sprintf("ATTACH DATABASE '%s' AS wa_session;", waDbPath)
	if _, err := d.conn.Exec(attachQuery); err != nil {
		return fmt.Errorf("error adjuntando wa session: %w", err)
	}
	defer func() {
		_, _ = d.conn.Exec("DETACH DATABASE wa_session;")
	}()

	// 1. Encontrar todos los canales @lid que tienen un mapeo a Phone Number en whatsmeow_lid_map
	rows, err := d.conn.Query(`
		SELECT 
			cc_lid.id, cc_lid.contact_id, cc_lid.remote_id,
			lm.pn,
			coalesce(cc_pn.id, ''), coalesce(cc_pn.contact_id, '')
		FROM contact_channels cc_lid
		JOIN wa_session.whatsmeow_lid_map lm ON lm.lid = replace(cc_lid.remote_id, '@lid', '')
		LEFT JOIN contact_channels cc_pn ON cc_pn.remote_id = lm.pn || '@s.whatsapp.net'
		WHERE cc_lid.remote_id LIKE '%@lid';
	`)
	if err != nil {
		return fmt.Errorf("error consultando canales lid mapeables: %w", err)
	}
	defer rows.Close()

	type lidMergePair struct {
		lidChannelID string
		lidContactID string
		lidRemoteID  string
		pn           string
		pnChannelID  string
		pnContactID  string
	}

	var pairs []lidMergePair
	for rows.Next() {
		var p lidMergePair
		if err := rows.Scan(&p.lidChannelID, &p.lidContactID, &p.lidRemoteID, &p.pn, &p.pnChannelID, &p.pnContactID); err != nil {
			return err
		}
		pairs = append(pairs, p)
	}
	rows.Close()

	for _, p := range pairs {
		targetContactID := p.pnContactID
		targetChannelID := p.pnChannelID

		// Si no existe canal de PN, actualizar el remote_id del canal lid para que sea el PN canónico
		if targetChannelID == "" {
			canonicalRemoteID := p.pn + "@s.whatsapp.net"
			_, _ = d.conn.Exec("UPDATE contact_channels SET remote_id = ? WHERE id = ?", canonicalRemoteID, p.lidChannelID)
			continue
		}

		// Si ya existe un canal de PN y pertenecen a contactos diferentes, fusionar los contactos y mensajes
		if targetContactID != "" && targetContactID != p.lidContactID {
			// Reasignar mensajes que apuntaban a la conversación del contacto @lid
			_, _ = d.conn.Exec("UPDATE messages SET conversation_id = ? WHERE conversation_id = ?", targetContactID, p.lidContactID)
			// Reasignar mensajes que tenían como channel_id el canal @lid al canal PN
			_, _ = d.conn.Exec("UPDATE messages SET channel_id = ? WHERE channel_id = ?", targetChannelID, p.lidChannelID)
			// Eliminar el canal @lid redundante
			_, _ = d.conn.Exec("DELETE FROM contact_channels WHERE id = ?", p.lidChannelID)
			// Eliminar la conversación duplicada del contacto @lid
			_, _ = d.conn.Exec("DELETE FROM conversations WHERE id = ?", p.lidContactID)
			// Eliminar el contacto @lid redundante
			_, _ = d.conn.Exec("DELETE FROM contacts WHERE id = ?", p.lidContactID)
		} else if targetContactID != "" && targetContactID == p.lidContactID {
			// Mismo contacto pero dos canales: reasignar mensajes al canal PN y eliminar el canal LID
			_, _ = d.conn.Exec("UPDATE messages SET channel_id = ? WHERE channel_id = ?", targetChannelID, p.lidChannelID)
			_, _ = d.conn.Exec("DELETE FROM contact_channels WHERE id = ?", p.lidChannelID)
		}
	}

	// 2. Corregir last_message_at en todas las conversaciones de contactos que fueron contaminadas con time.Now()
	// Las conversaciones con mensajes reales toman el max(sent_at).
	// Las conversaciones sin mensajes se colocan en 1970-01-01 para no tapar los chats con actividad.
	_, _ = d.conn.Exec(`
		UPDATE conversations
		SET last_message_at = coalesce(
			(SELECT max(sent_at) FROM messages WHERE conversation_id = conversations.id),
			'1970-01-01 00:00:00'
		)
		WHERE is_group = 0;
	`)

	return nil
}

// SaveMessage almacena un mensaje de forma idempotente.
func (d *DB) SaveMessage(ctx context.Context, msg *domain.Message) error {
	receivedAt := msg.ReceivedAt
	if receivedAt.IsZero() {
		receivedAt = time.Now()
	}
	// time.Time.String() incluye la lectura monotónica del proceso; el driver la
	// persiste y ensucia el formato. Se quita para que todos los timestamps
	// guarden el mismo formato.
	sentAt := stripMonotonic(msg.SentAt)
	receivedAt = stripMonotonic(receivedAt)

	msg.Metadata = d.buildMessageMetadataJSON(msg)

	// 1. Si ya existe un mensaje con este ID específico, actualizarlo
	var existingID string
	err := d.conn.QueryRowContext(ctx, "SELECT id FROM messages WHERE id = ?", msg.ID).Scan(&existingID)
	if err == nil && existingID != "" {
		_, err := d.conn.ExecContext(ctx, `
			UPDATE messages 
			SET remote_message_id = CASE WHEN ? != '' THEN ? ELSE remote_message_id END,
			    status = CASE WHEN status = 'read' THEN 'read' ELSE ? END,
			    body = ?,
			    content_type = CASE WHEN ? != '' AND ? != 'text' THEN ? ELSE content_type END,
			    metadata = CASE WHEN ? != '' THEN ? ELSE metadata END
			WHERE id = ?`,
			msg.RemoteMessageID, msg.RemoteMessageID, string(msg.Status), msg.Body,
			string(msg.ContentType), string(msg.ContentType), string(msg.ContentType),
			msg.Metadata, msg.Metadata, msg.ID,
		)
		if err != nil {
			return fmt.Errorf("error actualizando mensaje por id: %w", err)
		}
		_, _ = d.conn.ExecContext(ctx, "UPDATE conversations SET last_message_at = ? WHERE id = ?", sentAt, msg.ConversationID)
		return nil
	}

	// 2. Si ya existe un mensaje con este remote_message_id en este canal, actualizarlo
	if msg.RemoteMessageID != "" {
		err = d.conn.QueryRowContext(ctx, "SELECT id FROM messages WHERE channel_id = ? AND remote_message_id = ?", msg.ChannelID, msg.RemoteMessageID).Scan(&existingID)
		if err == nil && existingID != "" {
			_, err := d.conn.ExecContext(ctx, `
				UPDATE messages 
				SET status = CASE WHEN status = 'read' THEN 'read' ELSE ? END, 
				    body = ?,
				    content_type = CASE WHEN ? != '' AND ? != 'text' THEN ? ELSE content_type END,
				    metadata = CASE WHEN ? != '' THEN ? ELSE metadata END,
				    sender_remote_id = coalesce(sender_remote_id, ?)
				WHERE id = ?`,
				string(msg.Status), msg.Body,
				string(msg.ContentType), string(msg.ContentType), string(msg.ContentType),
				msg.Metadata, msg.Metadata,
				nullIfEmpty(msg.SenderRemoteID), existingID,
			)
			if err != nil {
				return fmt.Errorf("error actualizando mensaje por remote_id: %w", err)
			}
			_, _ = d.conn.ExecContext(ctx, "UPDATE conversations SET last_message_at = ? WHERE id = ?", sentAt, msg.ConversationID)
			return nil
		}
	}

	// 3. Si es un mensaje saliente ('me') y ya existe un mensaje reciente 'out-%' con el mismo body en la misma conversación,
	// asociar el remote_message_id real a ese mensaje existente para evitar duplicación
	if msg.SenderType == domain.SenderMe && msg.RemoteMessageID != "" && !strings.HasPrefix(msg.RemoteMessageID, "out-") {
		var outMsgID string
		err = d.conn.QueryRowContext(ctx, `
			SELECT id FROM messages 
			WHERE conversation_id = ? AND sender_type = 'me' AND remote_message_id LIKE 'out-%' AND body = ? 
			ORDER BY sent_at DESC LIMIT 1`,
			msg.ConversationID, msg.Body,
		).Scan(&outMsgID)
		if err == nil && outMsgID != "" {
			_, err := d.conn.ExecContext(ctx, `
				UPDATE messages 
				SET remote_message_id = ?, status = ?
				WHERE id = ?`,
				msg.RemoteMessageID, string(msg.Status), outMsgID,
			)
			if err != nil {
				return fmt.Errorf("error vinculando remote_message_id a out- mensaje: %w", err)
			}
			_, _ = d.conn.ExecContext(ctx, "UPDATE conversations SET last_message_at = ? WHERE id = ?", sentAt, msg.ConversationID)
			return nil
		}
	}

	// 4. Si no existe, insertar nuevo de forma idempotente
	if msg.Metadata == "" {
		metaMap := make(map[string]any)
		if msg.Poll != nil {
			metaMap["question"] = msg.Poll.Question
			metaMap["options"] = msg.Poll.Options
			metaMap["selectable_count"] = msg.Poll.SelectableCount
			metaMap["is_closed"] = msg.Poll.IsClosed
			metaMap["total_votes"] = msg.Poll.TotalVotes
			metaMap["is_quiz"] = msg.Poll.IsQuiz
		}
		if len(msg.ButtonRows) > 0 {
			metaMap["button_rows"] = msg.ButtonRows
		}
		if msg.Location != nil {
			metaMap["location"] = msg.Location
		}
		if msg.SharedContact != nil {
			metaMap["shared_contact"] = msg.SharedContact
		}
		if msg.IsPinned {
			metaMap["is_pinned"] = true
		}
		if msg.IsEdited {
			metaMap["is_edited"] = true
			if msg.EditedAt != nil {
				metaMap["edited_at_unix"] = msg.EditedAt.Unix()
			}
		}
		if len(metaMap) > 0 {
			if data, err := json.Marshal(metaMap); err == nil {
				msg.Metadata = string(data)
			}
		}
	}

	query := `
	INSERT INTO messages (
		id, conversation_id, channel_id, remote_message_id,
		sender_type, body, content_type, status, sent_at, received_at, reply_to_message_id, metadata, sender_remote_id
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(channel_id, remote_message_id) DO UPDATE SET
		status=CASE WHEN messages.status = 'read' THEN 'read' ELSE excluded.status END,
		body=excluded.body,
		content_type=CASE WHEN excluded.content_type != '' AND excluded.content_type != 'text' THEN excluded.content_type ELSE messages.content_type END,
		metadata=CASE WHEN excluded.metadata != '' THEN excluded.metadata ELSE messages.metadata END;
	`
	_, err = d.conn.ExecContext(ctx, query,
		msg.ID, msg.ConversationID, msg.ChannelID, msg.RemoteMessageID,
		string(msg.SenderType), msg.Body, string(msg.ContentType), string(msg.Status),
		sentAt, receivedAt, msg.ReplyToMessageID, msg.Metadata, nullIfEmpty(msg.SenderRemoteID),
	)
	if err != nil {
		return fmt.Errorf("error guardando mensaje: %w", err)
	}

	// Guardar adjuntos si existen
	for _, att := range msg.Attachments {
		_ = d.SaveAttachment(ctx, &att)
	}

	// Actualizar last_message_at en la conversación
	_, _ = d.conn.ExecContext(ctx, "UPDATE conversations SET last_message_at = ? WHERE id = ?", sentAt, msg.ConversationID)
	return nil
}

// UpdateMessageMetadata actualiza la columna metadata de un mensaje (usado para encuestas y estados ricos).
func (d *DB) UpdateMessageMetadata(ctx context.Context, messageID, metadata string) error {
	_, err := d.conn.ExecContext(ctx, "UPDATE messages SET metadata = ? WHERE id = ?", metadata, messageID)
	return err
}

// UpdateMessageBody actualiza el texto de un mensaje y lo marca como editado.
func (d *DB) UpdateMessageBody(ctx context.Context, messageID, newBody string) error {
	nowUnix := time.Now().Unix()
	query := `
		UPDATE messages 
		SET body = ?,
		    metadata = json_set(
		        case when json_valid(coalesce(metadata, '')) then metadata else '{}' end,
		        '$.is_edited', json('true'),
		        '$.edited_at_unix', ?
		    )
		WHERE id = ?;
	`
	_, err := d.conn.ExecContext(ctx, query, newBody, nowUnix, messageID)
	return err
}

// DeleteMessage elimina un mensaje de la base de datos de forma permanente.
func (d *DB) DeleteMessage(ctx context.Context, messageID string) error {
	_, err := d.conn.ExecContext(ctx, "DELETE FROM messages WHERE id = ?", messageID)
	return err
}

// DeleteMessageByRemoteID busca y elimina un mensaje dado su canal y remote_message_id, devolviendo el id local.
// Si channelID es vacío, busca el mensaje únicamente por remote_message_id.
func (d *DB) DeleteMessageByRemoteID(ctx context.Context, channelID, remoteMessageID string) (string, error) {
	var localID string
	var query string
	var args []any
	if channelID != "" {
		query = "SELECT id FROM messages WHERE channel_id = ? AND remote_message_id = ? LIMIT 1"
		args = []any{channelID, remoteMessageID}
	} else {
		query = "SELECT id FROM messages WHERE remote_message_id = ? LIMIT 1"
		args = []any{remoteMessageID}
	}
	err := d.conn.QueryRowContext(ctx, query, args...).Scan(&localID)
	if err != nil {
		return "", err
	}
	_, err = d.conn.ExecContext(ctx, "DELETE FROM messages WHERE id = ?", localID)
	return localID, err
}

func (d *DB) buildMessageMetadataJSON(msg *domain.Message) string {
	if msg.Metadata != "" {
		return msg.Metadata
	}
	metaMap := make(map[string]any)
	if msg.Poll != nil {
		metaMap["question"] = msg.Poll.Question
		metaMap["options"] = msg.Poll.Options
		metaMap["selectable_count"] = msg.Poll.SelectableCount
		metaMap["is_closed"] = msg.Poll.IsClosed
		metaMap["total_votes"] = msg.Poll.TotalVotes
		metaMap["is_quiz"] = msg.Poll.IsQuiz
	}
	if len(msg.ButtonRows) > 0 {
		metaMap["button_rows"] = msg.ButtonRows
	}
	if msg.Location != nil {
		metaMap["location"] = msg.Location
	}
	if msg.SharedContact != nil {
		metaMap["shared_contact"] = msg.SharedContact
	}
	if msg.IsPinned {
		metaMap["is_pinned"] = true
	}
	if msg.IsEdited {
		metaMap["is_edited"] = true
		if msg.EditedAt != nil {
			metaMap["edited_at_unix"] = msg.EditedAt.Unix()
		}
	}
	if len(metaMap) > 0 {
		if data, err := json.Marshal(metaMap); err == nil {
			return string(data)
		}
	}
	return ""
}

// SetMessagePinned actualiza el estado fijado de un mensaje.
func (d *DB) SetMessagePinned(ctx context.Context, messageID string, pinned bool) error {
	pinnedVal := "false"
	if pinned {
		pinnedVal = "true"
	}
	query := `
		UPDATE messages
		SET metadata = json_set(
		    case when json_valid(coalesce(metadata, '')) then metadata else '{}' end,
		    '$.is_pinned', json(?)
		)
		WHERE id = ?;
	`
	_, err := d.conn.ExecContext(ctx, query, pinnedVal, messageID)
	return err
}

// SaveAttachment guarda un adjunto multimedia en la base de datos (upsert por id).
// Un adjunto ya 'cached' con archivo local es pegajoso: un guardado posterior que no trae
// archivo (remote_only, failed, downloading o ruta vacía) conserva estado y ruta. La remote_ref
// solo se completa cuando la almacenada está vacía.
func (d *DB) SaveAttachment(ctx context.Context, att *domain.Attachment) error {
	query := `
	INSERT INTO attachments (id, message_id, file_name, mime_type, file_size, local_path, download_status, checksum, remote_ref)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		file_name=excluded.file_name,
		mime_type=excluded.mime_type,
		file_size=CASE WHEN ` + stickyCached + ` THEN attachments.file_size ELSE excluded.file_size END,
		local_path=CASE WHEN ` + stickyCached + ` THEN attachments.local_path ELSE excluded.local_path END,
		download_status=CASE WHEN ` + stickyCached + ` THEN attachments.download_status ELSE excluded.download_status END,
		checksum=excluded.checksum,
		remote_ref=CASE WHEN coalesce(attachments.remote_ref, '') = '' THEN excluded.remote_ref ELSE attachments.remote_ref END;
	`
	_, err := d.conn.ExecContext(ctx, query,
		att.ID, att.MessageID, att.FileName, att.MimeType, att.FileSize,
		att.LocalPath, string(att.DownloadStatus), att.Checksum, att.RemoteRef,
	)
	return err
}

// stickyCached es la condición SQL que indica que la fila almacenada está cacheada con archivo
// y el guardado entrante no trae un archivo propio.
const stickyCached = `(attachments.download_status = 'cached' AND coalesce(attachments.local_path, '') <> ''
		AND (excluded.download_status <> 'cached' OR coalesce(excluded.local_path, '') = ''))`

// GetAttachmentByID retorna un adjunto (incluida su remote_ref) o sql.ErrNoRows si no existe.
func (d *DB) GetAttachmentByID(ctx context.Context, id string) (*domain.Attachment, error) {
	var att domain.Attachment
	var localPath, statusStr string
	err := d.conn.QueryRowContext(ctx, `
		SELECT id, message_id, coalesce(file_name, ''), mime_type, coalesce(file_size, 0), coalesce(local_path, ''),
		       download_status, coalesce(checksum, ''), coalesce(remote_ref, '')
		FROM attachments WHERE id = ?`, id).Scan(
		&att.ID, &att.MessageID, &att.FileName, &att.MimeType, &att.FileSize, &localPath, &statusStr, &att.Checksum, &att.RemoteRef)
	if err != nil {
		return nil, err
	}
	if localPath != "" {
		att.LocalPath = &localPath
	}
	att.DownloadStatus = domain.DownloadStatus(statusStr)
	return &att, nil
}

// UpdateAttachmentDownload actualiza el estado de descarga, la ruta local y el tamaño de un adjunto.
// Un localPath nil deja la ruta en NULL; un fileSize <= 0 conserva el tamaño almacenado.
func (d *DB) UpdateAttachmentDownload(ctx context.Context, id string, status domain.DownloadStatus, localPath *string, fileSize int64) error {
	_, err := d.conn.ExecContext(ctx, `
		UPDATE attachments
		SET download_status = ?, local_path = ?, file_size = CASE WHEN ? > 0 THEN ? ELSE file_size END
		WHERE id = ?`, string(status), localPath, fileSize, fileSize, id)
	return err
}

// GetMessageWithAttachments carga un mensaje por id junto con sus adjuntos.
func (d *DB) GetMessageWithAttachments(ctx context.Context, id string) (*domain.Message, error) {
	msg, err := d.FindMessageByID(ctx, id)
	if err != nil {
		return nil, err
	}
	atts, err := d.GetAttachmentsByMessageIDs(ctx, []string{id})
	if err != nil {
		return nil, err
	}
	msg.Attachments = atts[id]
	return msg, nil
}

// GetAttachmentsByMessageIDs retorna los adjuntos asociados a una lista de IDs de mensajes.
func (d *DB) GetAttachmentsByMessageIDs(ctx context.Context, messageIDs []string) (map[string][]domain.Attachment, error) {
	result := make(map[string][]domain.Attachment)
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
		SELECT id, message_id, coalesce(file_name, ''), mime_type, coalesce(file_size, 0), coalesce(local_path, ''), download_status, coalesce(checksum, ''), coalesce(remote_ref, '')
		FROM attachments
		WHERE message_id IN (%s)
	`, strings.Join(placeholders, ","))

	rows, err := d.conn.QueryContext(ctx, query, args...)
	if err != nil {
		return result, err
	}
	defer rows.Close()

	for rows.Next() {
		var att domain.Attachment
		var localPath string
		var statusStr string
		if err := rows.Scan(&att.ID, &att.MessageID, &att.FileName, &att.MimeType, &att.FileSize, &localPath, &statusStr, &att.Checksum, &att.RemoteRef); err != nil {
			return result, err
		}
		if localPath != "" {
			att.LocalPath = &localPath
		}
		att.DownloadStatus = domain.DownloadStatus(statusStr)
		result[att.MessageID] = append(result[att.MessageID], att)
	}
	return result, nil
}

// SearchMessages realiza búsqueda de texto completo con FTS5.
func (d *DB) SearchMessages(ctx context.Context, query string, limit int) ([]domain.Message, error) {
	sqlQuery := `
	SELECT m.id, m.conversation_id, m.channel_id, m.remote_message_id,
	       m.sender_type, m.body, m.content_type, m.status, m.sent_at, m.received_at
	FROM messages m
	JOIN messages_fts fts ON m.rowid = fts.rowid
	WHERE messages_fts MATCH ?
	ORDER BY m.sent_at DESC
	LIMIT ?;
	`
	rows, err := d.conn.QueryContext(ctx, sqlQuery, query, limit)
	if err != nil {
		return nil, fmt.Errorf("error en busqueda FTS5: %w", err)
	}
	defer rows.Close()

	var results []domain.Message
	for rows.Next() {
		var m domain.Message
		var senderType, contentType, status string
		if err := rows.Scan(
			&m.ID, &m.ConversationID, &m.ChannelID, &m.RemoteMessageID,
			&senderType, &m.Body, &contentType, &status, &m.SentAt, &m.ReceivedAt,
		); err != nil {
			return nil, err
		}
		m.SenderType = domain.SenderType(senderType)
		m.ContentType = domain.ContentType(contentType)
		m.Status = domain.MessageStatus(status)
		results = append(results, m)
	}
	return results, nil
}
