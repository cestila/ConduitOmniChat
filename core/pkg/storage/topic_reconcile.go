package storage

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// ReconcileResult describe lo que borró DeleteConversationsAbsentFrom.
type ReconcileResult struct {
	// DeletedIDs son los ids de las conversaciones borradas.
	DeletedIDs []string
	// MediaPaths son las rutas locales de los adjuntos borrados que ningún adjunto restante usa.
	// El llamador decide si puede borrar esos archivos del disco, ya con la transacción confirmada.
	MediaPaths []string
}

// DeleteConversationsAbsentFrom borra, en una sola transacción, las conversaciones de canal
// cuyo id es `<convPrefix><remote id>` y cuyo remote id está en candidatos del servidor serverID
// pero no en keep, y cuya última actividad (last_message_at) no es posterior a snapshotAt: la
// lista viva se obtuvo en ese instante, así que un tema más reciente pudo crearse después y nunca
// se borra. Una conversación con hora ilegible tampoco se borra. snapshotAt cero es un error.
//
// El prefijo se compara en Go (caracteres UTF-8 completos), no con substr() de SQLite.
// `isCandidate` decide qué remote ids son elegibles (el llamador define el formato válido);
// nunca se tocan conversaciones de otro servidor ni sin server_id.
//
// Por cada conversación elimina sus mensajes, adjuntos y reacciones (la base no activa
// foreign_keys, así que el ON DELETE CASCADE no actúa) y la fila contact_channels huérfana
// (contact_id vacío) que coincide por proveedor y remote id, si ya nadie la referencia. Devuelve los ids borrados y las rutas locales de media que quedaron sin referencia.
func (d *DB) DeleteConversationsAbsentFrom(ctx context.Context, convPrefix, provider, serverID string, snapshotAt time.Time, isCandidate func(remoteID string) bool, keep map[string]bool) (ReconcileResult, error) {
	if snapshotAt.IsZero() || snapshotAt.Unix() <= 0 {
		return ReconcileResult{}, errors.New("hora del snapshot ausente: no es seguro borrar")
	}
	tx, err := d.conn.BeginTx(ctx, nil)
	if err != nil {
		return ReconcileResult{}, err
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.QueryContext(ctx,
		`SELECT id, CAST(last_message_at AS TEXT) FROM conversations WHERE server_id = ?`, serverID)
	if err != nil {
		return ReconcileResult{}, err
	}
	var doomed []string
	for rows.Next() {
		var id string
		var lastRaw *string
		if err := rows.Scan(&id, &lastRaw); err != nil {
			rows.Close()
			return ReconcileResult{}, err
		}
		remoteID, ok := strings.CutPrefix(id, convPrefix)
		if !ok || !isCandidate(remoteID) || keep[remoteID] {
			continue
		}
		last, parsed := time.Time{}, false
		if lastRaw != nil {
			last, parsed = parseStoredTime(*lastRaw)
		}
		if parsed && !last.After(snapshotAt) {
			doomed = append(doomed, id)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return ReconcileResult{}, err
	}
	rows.Close()

	var mediaCandidates []string
	for _, id := range doomed {
		paths, err := localMediaPaths(ctx, tx, id)
		if err != nil {
			return ReconcileResult{}, err
		}
		mediaCandidates = append(mediaCandidates, paths...)
	}

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
				return ReconcileResult{}, err
			}
		}
	}
	// Solo se devuelven las rutas que ningún adjunto restante referencia (el plugin nombra los
	// archivos por id de mensaje, así que dos adjuntos podrían compartir ruta).
	var orphans []string
	seen := map[string]bool{}
	for _, p := range mediaCandidates {
		if seen[p] {
			continue
		}
		seen[p] = true
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM attachments WHERE local_path = ?`, p).Scan(&n); err != nil {
			return ReconcileResult{}, err
		}
		if n == 0 {
			orphans = append(orphans, p)
		}
	}
	if err := tx.Commit(); err != nil {
		return ReconcileResult{}, err
	}
	return ReconcileResult{DeletedIDs: doomed, MediaPaths: orphans}, nil
}

// localMediaPaths devuelve las rutas locales no vacías de los adjuntos de una conversación.
func localMediaPaths(ctx context.Context, tx *sql.Tx, convID string) ([]string, error) {
	rows, err := tx.QueryContext(ctx,
		`SELECT coalesce(a.local_path, '') FROM attachments a JOIN messages m ON m.id = a.message_id WHERE m.conversation_id = ?`, convID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		if p != "" {
			out = append(out, p)
		}
	}
	return out, rows.Err()
}
