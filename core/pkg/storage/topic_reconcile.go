package storage

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"modernc.org/sqlite"
)

// ReconcileResult describe lo que borró DeleteConversationsAbsentFrom o ReconcileForumTopics.
type ReconcileResult struct {
	// DeletedIDs son los ids de las conversaciones borradas.
	DeletedIDs []string
	// MediaPaths son las rutas locales de los adjuntos borrados que ningún adjunto restante usa.
	// El llamador decide si puede borrar esos archivos del disco, ya con la transacción confirmada.
	MediaPaths []string
	// RehomedIDs son las conversaciones con nombre de grupo cuyos mensajes se movieron al tema
	// General y cuya fila se borró (solo ReconcileForumTopics).
	RehomedIDs []string
}

// generalTopicSuffix forma el remote id del tema General de un foro.
const generalTopicSuffix = "_topic_1"

const (
	// reconcileRetryAttempts es el tope de intentos de una transacción de reconciliación.
	reconcileRetryAttempts = 5
	// reconcileRetryBaseDelay es la espera del primer reintento; se duplica en cada uno de los
	// siguientes (20, 40, 80, 160 ms).
	reconcileRetryBaseDelay = 20 * time.Millisecond
	// Códigos base de SQLite: el código extendido de busy/locked comparte estos bits bajos.
	sqliteCodeBusy   = 5
	sqliteCodeLocked = 6
)

// reconcileRetrySleep es el hook de espera entre reintentos; los tests lo reemplazan para no
// dormir. No se exporta para no ampliar la superficie pública del paquete.
var reconcileRetrySleep = func(d time.Duration) { time.Sleep(d) }

// isBusyErr informa si err es un error de base bloqueada de SQLite (BUSY o LOCKED, incluido el
// código extendido SQLITE_BUSY_SNAPSHOT = 517 que aparece cuando otra conexión confirmó entre
// las lecturas y las escrituras de esta transacción).
func isBusyErr(err error) bool {
	if err == nil {
		return false
	}
	var se *sqlite.Error
	if errors.As(err, &se) {
		switch se.Code() & 0xff {
		case sqliteCodeBusy, sqliteCodeLocked:
			return true
		}
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "database is locked") ||
		strings.Contains(msg, "database table is locked") ||
		strings.Contains(msg, "sqlite_busy") ||
		strings.Contains(msg, "sqlite_locked")
}

// retryOnBusy ejecuta op reintentando, con una transacción nueva por intento (es op quien la
// abre), solo cuando falla por base bloqueada. Un error distinto se devuelve de inmediato.
func retryOnBusy(ctx context.Context, op func() error) error {
	var err error
	for attempt := 0; attempt < reconcileRetryAttempts; attempt++ {
		if attempt > 0 {
			reconcileRetrySleep(reconcileRetryBaseDelay << (attempt - 1))
		}
		if err = op(); err == nil {
			return nil
		}
		if !isBusyErr(err) {
			return err
		}
	}
	return err
}

// generalTopicTitle es el título del tema General de un foro.
const generalTopicTitle = "General"

// forumTopicSeparator separa el remote id del foro del número de tema en el id de una conversación
// de tema (`<plugin>:<ServerId>_topic_<N>`).
const forumTopicSeparator = "_topic_"

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
	return d.reconcileForumTopics(ctx, convPrefix, provider, serverID, snapshotAt, isCandidate, keep, false)
}

// ReconcileForumTopics hace, en una sola transacción y antes de borrar nada, la reparación de los
// pseudo temas con nombre de grupo (ver rehomeGroupNamedForumConversationsTx) y luego la limpieza de
// DeleteConversationsAbsentFrom. Todo o nada: si falla cualquier paso no se toca la base.
func (d *DB) ReconcileForumTopics(ctx context.Context, convPrefix, provider, serverID string, snapshotAt time.Time, isCandidate func(remoteID string) bool, keep map[string]bool) (ReconcileResult, error) {
	return d.reconcileForumTopics(ctx, convPrefix, provider, serverID, snapshotAt, isCandidate, keep, true)
}

func (d *DB) reconcileForumTopics(ctx context.Context, convPrefix, provider, serverID string, snapshotAt time.Time, isCandidate func(remoteID string) bool, keep map[string]bool, rehome bool) (ReconcileResult, error) {
	if snapshotAt.IsZero() || snapshotAt.Unix() <= 0 {
		return ReconcileResult{}, errors.New("hora del snapshot ausente: no es seguro borrar")
	}
	var res ReconcileResult
	err := retryOnBusy(ctx, func() error {
		r, err := d.reconcileForumTopicsOnce(ctx, convPrefix, provider, serverID, snapshotAt, isCandidate, keep, rehome)
		if err != nil {
			return err
		}
		res = r
		return nil
	})
	if err != nil {
		return ReconcileResult{}, err
	}
	return res, nil
}

// reconcileForumTopicsOnce hace un intento completo de la reconciliación en una transacción
// propia: la operación es idempotente, así que el llamador puede repetirla desde cero.
func (d *DB) reconcileForumTopicsOnce(ctx context.Context, convPrefix, provider, serverID string, snapshotAt time.Time, isCandidate func(remoteID string) bool, keep map[string]bool, rehome bool) (ReconcileResult, error) {
	tx, err := d.conn.BeginTx(ctx, nil)
	if err != nil {
		return ReconcileResult{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var rehomed []string
	if rehome {
		if rehomed, err = rehomeGroupNamedForumConversationsTx(ctx, tx, convPrefix, provider, serverID); err != nil {
			return ReconcileResult{}, err
		}
	}

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
	return ReconcileResult{DeletedIDs: doomed, MediaPaths: orphans, RehomedIDs: rehomed}, nil
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

// rehomeGroupNamedForumConversationsTx repara el pseudo tema con nombre de grupo que el fallback
// antiguo del plugin creaba al no poder resolver el tema: una conversación `<convPrefix><serverID>`
// (sin `_topic_`), sin parent_channel_id ni contacto, de tipo 'forum' o 'group'. El tipo 'group' se
// acepta solo cuando el mismo servidor prueba ser un foro (tiene conversaciones de tema, ver
// serverIsForumTx): un grupo básico de verdad nunca se toca. Mueve sus mensajes al tema General
// (`<convPrefix><serverID>_topic_1`, se crea si falta) y borra la conversación vieja junto con su
// fila de canal cuando ya nada la referencia. Nunca toca grupos reales, temas reales (con parent),
// DMs (con contacto) ni conversaciones de otro proveedor.
func rehomeGroupNamedForumConversationsTx(ctx context.Context, tx *sql.Tx, convPrefix, provider, serverID string) ([]string, error) {
	if serverID == "" {
		return nil, nil
	}
	convID := convPrefix + serverID
	var channelType, parent, contactID, activeChannel string
	var lastRaw *string
	err := tx.QueryRowContext(ctx, `
		SELECT coalesce(channel_type, ''), coalesce(parent_channel_id, ''), coalesce(contact_id, ''),
		       coalesce(active_channel_id, ''), CAST(last_message_at AS TEXT)
		FROM conversations
		WHERE id = ? AND coalesce(server_id, '') = ?`, convID, serverID).
		Scan(&channelType, &parent, &contactID, &activeChannel, &lastRaw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if parent != "" || contactID != "" {
		return nil, nil
	}
	switch strings.ToLower(strings.TrimSpace(channelType)) {
	case "forum":
		// El tipo declarado ya lo identifica como foro.
	case "group":
		// En la base real el pseudo tema quedó tipado 'group': solo se repara si el
		// servidor prueba ser un foro por tener conversaciones de tema.
		forum, err := serverIsForumTx(ctx, tx, convPrefix, serverID)
		if err != nil {
			return nil, err
		}
		if !forum {
			return nil, nil
		}
	default:
		return nil, nil
	}
	if activeChannel != "" {
		var chProvider string
		err := tx.QueryRowContext(ctx, `SELECT provider FROM contact_channels WHERE id = ?`, activeChannel).Scan(&chProvider)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		if chProvider != "" && !strings.EqualFold(chProvider, provider) {
			return nil, nil
		}
	}

	oldLast, oldLastOK := time.Time{}, false
	if lastRaw != nil {
		oldLast, oldLastOK = parseStoredTime(*lastRaw)
	}

	generalID := convID + generalTopicSuffix
	generalRemoteID := serverID + generalTopicSuffix
	var generalActive string
	var generalRaw *string
	err = tx.QueryRowContext(ctx, `
		SELECT coalesce(active_channel_id, ''), CAST(last_message_at AS TEXT)
		FROM conversations WHERE id = ?`, generalID).Scan(&generalActive, &generalRaw)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		generalActive, err = ensureTopicChannelTx(ctx, tx, provider, generalRemoteID)
		if err != nil {
			return nil, err
		}
		var lastAt any
		if oldLastOK {
			lastAt = stripMonotonic(oldLast)
		} else {
			// La marca de la conversación original no se pudo leer (NULL o formato
			// ajeno): General no puede quedar con last_message_at NULL porque la
			// lectura de la conversación falla. Se usa la actividad real de los
			// mensajes movidos.
			lastAt = maxMessageSentAtTx(ctx, tx, convID)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO conversations (id, contact_id, is_group, title, last_message_at, active_channel_id, server_id, parent_channel_id, channel_type)
			VALUES (?, NULL, 1, ?, ?, ?, ?, ?, 'forum')`,
			generalID, generalTopicTitle, lastAt, generalActive, serverID, serverID); err != nil {
			return nil, err
		}
	case err != nil:
		return nil, err
	default:
		// General ya existe: se conserva su título y solo se lleva la actividad al valor más nuevo.
		if generalActive == "" {
			generalActive, err = ensureTopicChannelTx(ctx, tx, provider, generalRemoteID)
			if err != nil {
				return nil, err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE conversations SET active_channel_id = ? WHERE id = ?`, generalActive, generalID); err != nil {
				return nil, err
			}
		}
		cur, curOK := time.Time{}, false
		if generalRaw != nil {
			cur, curOK = parseStoredTime(*generalRaw)
		}
		candidate, candidateOK := oldLast, oldLastOK
		if !candidateOK {
			// La marca del pseudo tema no se pudo leer: la actividad real está en sus
			// mensajes, que ya se van a mover a General.
			if at, ok := maxMessageSentAtTx(ctx, tx, convID).(time.Time); ok {
				candidate, candidateOK = at, true
			}
		}
		if candidateOK && (!curOK || candidate.After(cur)) {
			if _, err := tx.ExecContext(ctx, `UPDATE conversations SET last_message_at = ? WHERE id = ?`, stripMonotonic(candidate), generalID); err != nil {
				return nil, err
			}
		}
	}

	// Los mensajes pasan a General y su canal se reapunta al tema 1 para que respuestas y acuses
	// usen el tema correcto. Si el reapuntado choca con UNIQUE(channel_id, remote_message_id) (el
	// mismo mensaje ya está en General), ese mensaje se mueve conservando su canal: nunca se pierde.
	if _, err := tx.ExecContext(ctx,
		`UPDATE OR IGNORE messages SET conversation_id = ?, channel_id = ? WHERE conversation_id = ?`,
		generalID, generalActive, convID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE messages SET conversation_id = ? WHERE conversation_id = ?`, generalID, convID); err != nil {
		return nil, err
	}

	// La fila de canal del grupo se borra solo si ya nada la referencia (regla compartida con la
	// limpieza de temas: la misma fila la usan todas las instancias del plugin del proveedor).
	stmts := []struct {
		q    string
		args []any
	}{
		{`DELETE FROM conversations WHERE id = ?`, []any{convID}},
		{`DELETE FROM contact_channels WHERE provider = ? AND remote_id = ? AND coalesce(contact_id, '') = ''
			AND NOT EXISTS (SELECT 1 FROM conversations WHERE active_channel_id = contact_channels.id)
			AND NOT EXISTS (SELECT 1 FROM messages WHERE channel_id = contact_channels.id)`, []any{provider, serverID}},
	}
	for _, s := range stmts {
		if _, err := tx.ExecContext(ctx, s.q, s.args...); err != nil {
			return nil, err
		}
	}
	return []string{convID}, nil
}

// serverIsForumTx informa si serverID es un foro según sus conversaciones: existe al menos una
// conversación de ese servidor cuyo id es `<convPrefix><serverID>_topic_<N>` o cuyo
// parent_channel_id es el propio servidor. Es la prueba que permite reparar un pseudo tema tipado
// 'group' sin tocar grupos básicos. El prefijo se compara en Go (caracteres UTF-8 completos).
func serverIsForumTx(ctx context.Context, tx *sql.Tx, convPrefix, serverID string) (bool, error) {
	rows, err := tx.QueryContext(ctx,
		`SELECT id, coalesce(parent_channel_id, '') FROM conversations WHERE coalesce(server_id, '') = ?`, serverID)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	topicIDPrefix := convPrefix + serverID + forumTopicSeparator
	found := false
	for rows.Next() {
		var id, parent string
		if err := rows.Scan(&id, &parent); err != nil {
			return false, err
		}
		if strings.HasPrefix(id, topicIDPrefix) || parent == serverID {
			found = true
			break
		}
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	return found, nil
}

// ensureTopicChannelTx devuelve la fila contact_channels del tema (provider, remoteID), creándola
// sin contacto si falta. Reutiliza la existente para no duplicar filas que comparten las instancias
// del plugin del mismo proveedor.
func ensureTopicChannelTx(ctx context.Context, tx *sql.Tx, provider, remoteID string) (string, error) {
	var id string
	err := tx.QueryRowContext(ctx, `
		SELECT id FROM contact_channels
		WHERE provider = ? AND remote_id = ?
		ORDER BY (coalesce(contact_id, '') = '') DESC, id
		LIMIT 1`, provider, remoteID).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	id = "ch-" + provider + ":" + remoteID
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO contact_channels (id, contact_id, provider, account_id, remote_id, handle, is_preferred)
		VALUES (?, '', ?, '', ?, ?, 1)
		ON CONFLICT DO NOTHING`, id, provider, remoteID, generalTopicTitle); err != nil {
		return "", err
	}
	if err := tx.QueryRowContext(ctx, `
		SELECT id FROM contact_channels WHERE provider = ? AND remote_id = ? LIMIT 1`, provider, remoteID).Scan(&id); err != nil {
		return "", err
	}
	return id, nil
}

// maxMessageSentAtTx devuelve la última actividad de una conversación según sus
// mensajes. Se usa cuando last_message_at no es legible: General no puede quedar
// con un valor que rompa la lectura de la conversación.
func maxMessageSentAtTx(ctx context.Context, tx *sql.Tx, convID string) any {
	var raw sql.NullString
	_ = tx.QueryRowContext(ctx, `
		SELECT CAST(max(sent_at) AS TEXT) FROM messages WHERE conversation_id = ?`, convID).Scan(&raw)
	if raw.Valid {
		if parsed, ok := parseStoredTime(raw.String); ok {
			return stripMonotonic(parsed)
		}
		// No se propaga el valor ilegible: General quedaría sin poder leerse.
		return stripMonotonic(time.Now().UTC())
	}
	return stripMonotonic(time.Now().UTC())
}
