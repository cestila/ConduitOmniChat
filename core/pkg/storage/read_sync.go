package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/cestila/ConduitOmniChat/core/pkg/domain"
)

// incomingFilter excluye los mensajes propios: solo los recibidos cuentan como no leídos.
const incomingFilter = "sender_type NOT IN ('me', 'USER', 'OUTGOING')"

// ReadSyncRef identifica un mensaje leído localmente cuyo leído aún no confirmó el proveedor.
type ReadSyncRef struct {
	ChannelID       string
	RemoteMessageID string
	SenderRemoteID  string
}

// nullIfEmpty guarda NULL en lugar de una cadena vacía.
func nullIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}

// MarkConversationReadForSync marca como leídos los mensajes recibidos de la conversación y
// los deja pendientes de propagar al proveedor. Devuelve cuántos mensajes cambiaron de estado.
func (d *DB) MarkConversationReadForSync(ctx context.Context, convID string) (int, error) {
	res, err := d.conn.ExecContext(ctx, `
	UPDATE messages
	SET status = 'read', read_sync_pending = 1
	WHERE conversation_id = ? AND status != 'read' AND `+incomingFilter+`
	  AND remote_message_id != ''`, convID)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

// PendingReadSync devuelve, en orden cronológico, los mensajes de la conversación cuyo leído
// todavía no se propagó al proveedor.
func (d *DB) PendingReadSync(ctx context.Context, convID string) ([]ReadSyncRef, error) {
	rows, err := d.conn.QueryContext(ctx, `
	SELECT channel_id, remote_message_id, coalesce(sender_remote_id, '')
	FROM messages
	WHERE conversation_id = ? AND read_sync_pending = 1
	ORDER BY sent_at ASC, rowid ASC`, convID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var refs []ReadSyncRef
	for rows.Next() {
		var ref ReadSyncRef
		if err := rows.Scan(&ref.ChannelID, &ref.RemoteMessageID, &ref.SenderRemoteID); err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	return refs, rows.Err()
}

// ClearReadSyncPending registra que el proveedor confirmó el leído de esos mensajes.
func (d *DB) ClearReadSyncPending(ctx context.Context, channelID string, remoteIDs []string) error {
	if len(remoteIDs) == 0 {
		return nil
	}
	placeholders := make([]string, len(remoteIDs))
	args := make([]any, 0, len(remoteIDs)+1)
	args = append(args, channelID)
	for i, id := range remoteIDs {
		placeholders[i] = "?"
		args = append(args, id)
	}
	_, err := d.conn.ExecContext(ctx, fmt.Sprintf(`
	UPDATE messages SET read_sync_pending = 0
	WHERE channel_id = ? AND remote_message_id IN (%s)`, strings.Join(placeholders, ",")), args...)
	return err
}

// ConversationsWithPendingReadSync lista las conversaciones con leídos pendientes en canales del proveedor.
func (d *DB) ConversationsWithPendingReadSync(ctx context.Context, provider domain.Provider) ([]string, error) {
	rows, err := d.conn.QueryContext(ctx, `
	SELECT DISTINCT m.conversation_id
	FROM messages m
	JOIN contact_channels ch ON ch.id = m.channel_id
	WHERE m.read_sync_pending = 1 AND ch.provider = ?
	ORDER BY m.conversation_id`, string(provider))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var convs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		convs = append(convs, id)
	}
	return convs, rows.Err()
}

// UnreadCount devuelve cuántos mensajes recibidos siguen sin leer en la conversación.
func (d *DB) UnreadCount(ctx context.Context, convID string) (int32, error) {
	var count int32
	err := d.conn.QueryRowContext(ctx, `
	SELECT count(*) FROM messages
	WHERE conversation_id = ? AND status != 'read' AND `+incomingFilter, convID).Scan(&count)
	return count, err
}

// ConversationIDForChannel devuelve la conversación de algún mensaje ya guardado del canal,
// o "" si el canal todavía no tiene mensajes. Es de solo lectura.
func (d *DB) ConversationIDForChannel(ctx context.Context, channelID string) (string, error) {
	var convID string
	err := d.conn.QueryRowContext(ctx, `
	SELECT conversation_id FROM messages WHERE channel_id = ?
	ORDER BY sent_at DESC, rowid DESC LIMIT 1`, channelID).Scan(&convID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return convID, err
}
