package storage

import (
	"context"
	"strings"
)

// DeleteConversationsAbsentFrom borra, en una sola transacción, las conversaciones de canal
// cuyo id es `<convPrefix><remote id>` y cuyo remote id está en candidatos del servidor serverID
// pero no en keep. `isCandidate` decide qué remote ids son elegibles (el llamador define el
// formato válido); nunca se tocan conversaciones de otro servidor ni sin server_id.
//
// Por cada conversación elimina sus mensajes, adjuntos y reacciones (la base no activa
// foreign_keys, así que el ON DELETE CASCADE no actúa) y la fila contact_channels huérfana
// (contact_id vacío) que coincide por proveedor y remote id, si ya nadie la referencia. Devuelve los ids borrados.
func (d *DB) DeleteConversationsAbsentFrom(ctx context.Context, convPrefix, provider, serverID string, isCandidate func(remoteID string) bool, keep map[string]bool) ([]string, error) {
	tx, err := d.conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.QueryContext(ctx,
		`SELECT id FROM conversations WHERE server_id = ? AND substr(id, 1, ?) = ?`,
		serverID, len(convPrefix), convPrefix)
	if err != nil {
		return nil, err
	}
	var doomed []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		remoteID := strings.TrimPrefix(id, convPrefix)
		if isCandidate(remoteID) && !keep[remoteID] {
			doomed = append(doomed, id)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	for _, id := range doomed {
		remoteID := strings.TrimPrefix(id, convPrefix)
		stmts := []struct {
			q    string
			args []any
		}{
			{`DELETE FROM attachments WHERE message_id IN (SELECT id FROM messages WHERE conversation_id = ?)`, []any{id}},
			{`DELETE FROM message_reactions WHERE message_id IN (SELECT id FROM messages WHERE conversation_id = ?)`, []any{id}},
			{`DELETE FROM messages WHERE conversation_id = ?`, []any{id}},
			{`DELETE FROM conversations WHERE id = ?`, []any{id}},
			// La fila la comparten todas las instancias de plugin del mismo proveedor: solo se borra
			// si, tras borrar esta conversación, nada más la usa (conversación o mensaje).
			{`DELETE FROM contact_channels WHERE provider = ? AND remote_id = ? AND coalesce(contact_id, '') = ''
				AND NOT EXISTS (SELECT 1 FROM conversations WHERE active_channel_id = contact_channels.id)
				AND NOT EXISTS (SELECT 1 FROM messages WHERE channel_id = contact_channels.id)`, []any{provider, remoteID}},
		}
		for _, s := range stmts {
			if _, err := tx.ExecContext(ctx, s.q, s.args...); err != nil {
				return nil, err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return doomed, nil
}
