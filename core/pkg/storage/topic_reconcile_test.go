package storage

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Una última actividad ausente o ilegible (NULL, vacío, basura, entero) nunca autoriza el borrado.
func TestDeleteConversationsAbsentFromKeepsUnreadableLastMessageAt(t *testing.T) {
	db, err := NewDB(filepath.Join(t.TempDir(), "reconcile.db"))
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer db.Close()
	ctx := context.Background()

	values := map[string]any{
		"null":    nil,
		"vacio":   "",
		"basura":  "garbage",
		"entero":  5,
		"antigua": "2020-01-01 00:00:00", // control: legible y anterior al snapshot, se borra
	}
	for name, v := range values {
		if _, err := db.conn.Exec(`INSERT INTO conversations (id, title, is_group, server_id, last_message_at) VALUES (?, 'x', 1, 'c1', ?)`,
			"telegram:c1_topic_"+name, v); err != nil {
			t.Fatalf("insert %s: %v", name, err)
		}
	}

	res, err := db.DeleteConversationsAbsentFrom(ctx, "telegram:", "telegram", "c1", time.Unix(2_000_000_000, 0),
		func(string) bool { return true }, map[string]bool{})
	if err != nil {
		t.Fatalf("DeleteConversationsAbsentFrom: %v", err)
	}

	if len(res.DeletedIDs) != 1 || res.DeletedIDs[0] != "telegram:c1_topic_antigua" {
		t.Fatalf("solo debería borrarse la conversación con hora legible, obtuve %v", res.DeletedIDs)
	}
	var n int
	if err := db.conn.QueryRow(`SELECT COUNT(*) FROM conversations`).Scan(&n); err != nil || n != 4 {
		t.Fatalf("deberían sobrevivir 4 conversaciones, obtuve %d (err=%v)", n, err)
	}
}

// busyErr simula el error que devuelve modernc/sqlite cuando otra conexión confirmó entre las
// lecturas y las escrituras de la transacción (SQLITE_BUSY_SNAPSHOT, código extendido 517).
func busyErr() error { return errors.New("database is locked (517)") }

// overrideReconcileSleep reemplaza el hook de espera para que los reintentos no duerman.
// Devuelve el slice donde se acumulan las esperas pedidas.
func overrideReconcileSleep(t *testing.T) *[]time.Duration {
	t.Helper()
	var waits []time.Duration
	prev := reconcileRetrySleep
	reconcileRetrySleep = func(d time.Duration) { waits = append(waits, d) }
	t.Cleanup(func() { reconcileRetrySleep = prev })
	return &waits
}

// (a) Un error busy se reintenta y el intento posterior tiene éxito.
func TestRetryOnBusyRetriesBusyErrorUntilSuccess(t *testing.T) {
	waits := overrideReconcileSleep(t)
	attempts := 0
	err := retryOnBusy(context.Background(), func() error {
		attempts++
		if attempts <= 3 {
			return busyErr()
		}
		return nil
	})
	if err != nil {
		t.Fatalf("retryOnBusy debería terminar sin error, obtuve %v", err)
	}
	if attempts != 4 {
		t.Fatalf("debería haber 4 intentos (3 busy + 1 ok), obtuve %d", attempts)
	}
	want := []time.Duration{20 * time.Millisecond, 40 * time.Millisecond, 80 * time.Millisecond}
	if len(*waits) != len(want) {
		t.Fatalf("esperas = %v, quería %v", *waits, want)
	}
	for i, w := range want {
		if (*waits)[i] != w {
			t.Fatalf("esperas = %v, quería %v", *waits, want)
		}
	}
}

// (b) Se rinde tras el tope de intentos y devuelve el error busy.
func TestRetryOnBusyGivesUpAfterAttemptCap(t *testing.T) {
	overrideReconcileSleep(t)
	attempts := 0
	err := retryOnBusy(context.Background(), func() error {
		attempts++
		return busyErr()
	})
	if err == nil {
		t.Fatal("retryOnBusy debería devolver el error busy tras agotar los intentos")
	}
	if !strings.Contains(err.Error(), "database is locked") {
		t.Fatalf("debería devolver el error busy original, obtuve %v", err)
	}
	if attempts != reconcileRetryAttempts {
		t.Fatalf("intentos = %d, quería %d", attempts, reconcileRetryAttempts)
	}
	if reconcileRetryAttempts != 5 {
		t.Fatalf("el tope de intentos debería ser 5, es %d", reconcileRetryAttempts)
	}
}

// (c) Un error que no es busy se devuelve de inmediato, sin reintentar.
func TestRetryOnBusyReturnsNonBusyErrorImmediately(t *testing.T) {
	waits := overrideReconcileSleep(t)
	attempts := 0
	boom := errors.New("constraint failed: FOREIGN KEY")
	err := retryOnBusy(context.Background(), func() error {
		attempts++
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("debería devolver el error original, obtuve %v", err)
	}
	if attempts != 1 {
		t.Fatalf("no debería reintentar un error no busy, intentos = %d", attempts)
	}
	if len(*waits) != 0 {
		t.Fatalf("no debería esperar ante un error no busy, esperas = %v", *waits)
	}
}

// isBusyErr reconoce el código extendido busy/locked y el texto del driver.
func TestIsBusyErr(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"busy snapshot 517", errors.New("database is locked (517)"), true},
		{"locked", errors.New("database table is locked (6)"), true},
		{"sqlite_busy", errors.New("SQLITE_BUSY"), true},
		{"envuelto", fmt.Errorf("borrando temas: %w", busyErr()), true},
		{"nulo", nil, false},
		{"constraint", errors.New("UNIQUE constraint failed"), false},
		{"cancelado", context.Canceled, false},
	}
	for _, tc := range cases {
		if got := isBusyErr(tc.err); got != tc.want {
			t.Errorf("%s: isBusyErr(%v) = %v, quería %v", tc.name, tc.err, got, tc.want)
		}
	}
}

// (d) De punta a punta y sin errores: ReconcileForumTopics borra el tema desaparecido y
// reubica el pseudo tema con nombre de grupo en General, como antes de los reintentos.
func TestReconcileForumTopicsDeletesVanishedAndRehomes(t *testing.T) {
	overrideReconcileSleep(t)
	db, err := NewDB(filepath.Join(t.TempDir(), "reconcile.db"))
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer db.Close()

	// Canal del pseudo tema con nombre de grupo.
	if _, err := db.conn.Exec(`INSERT INTO contact_channels (id, contact_id, provider, account_id, remote_id, handle, is_preferred)
		VALUES ('ch-foro', '', 'telegram', '', 'c1', 'Foro', 1)`); err != nil {
		t.Fatalf("insert canal: %v", err)
	}
	rows := []struct {
		id, last, channelType, parent, activeChannel string
	}{
		// Tema General y tema 2 vivos; el tema 2 se queda en keep.
		{"tg:c1_topic_1", "2020-01-01 00:00:00", "forum", "c1", ""},
		{"tg:c1_topic_2", "2020-01-01 00:00:00", "forum", "c1", ""},
		// Tema desaparecido en Telegram: no está en la lista viva.
		{"tg:c1_topic_9", "2020-01-01 00:00:00", "forum", "c1", ""},
		// Pseudo tema con nombre de grupo: se reubica en General.
		{"tg:c1", "2021-01-01 00:00:00", "forum", "", "ch-foro"},
	}
	for _, r := range rows {
		if _, err := db.conn.Exec(`INSERT INTO conversations (id, contact_id, is_group, title, last_message_at, active_channel_id, server_id, parent_channel_id, channel_type)
			VALUES (?, NULL, 1, 'x', ?, ?, 'c1', ?, ?)`, r.id, r.last, r.activeChannel, r.parent, r.channelType); err != nil {
			t.Fatalf("insert %s: %v", r.id, err)
		}
	}
	if _, err := db.conn.Exec(`INSERT INTO messages (id, conversation_id, channel_id, remote_message_id, sender_type, body, content_type, status, sent_at)
		VALUES ('m1', 'tg:c1', 'ch-foro', '100', 'user', 'hola', 'text', 'sent', '2021-01-01 00:00:00')`); err != nil {
		t.Fatalf("insert mensaje: %v", err)
	}

	keep := map[string]bool{"c1_topic_1": true, "c1_topic_2": true}
	res, err := db.ReconcileForumTopics(context.Background(), "tg:", "telegram", "c1", time.Unix(2_000_000_000, 0),
		func(remoteID string) bool { return strings.Contains(remoteID, "_topic_") }, keep)
	if err != nil {
		t.Fatalf("ReconcileForumTopics: %v", err)
	}

	if len(res.DeletedIDs) != 1 || res.DeletedIDs[0] != "tg:c1_topic_9" {
		t.Fatalf("debería borrarse solo el tema desaparecido, obtuve %v", res.DeletedIDs)
	}
	if len(res.RehomedIDs) != 1 || res.RehomedIDs[0] != "tg:c1" {
		t.Fatalf("debería reubicarse el pseudo tema del grupo, obtuve %v", res.RehomedIDs)
	}
	var n int
	if err := db.conn.QueryRow(`SELECT COUNT(*) FROM conversations WHERE id IN ('tg:c1', 'tg:c1_topic_9')`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("el pseudo tema y el tema desaparecido no deben quedar, quedan %d (err=%v)", n, err)
	}
	if err := db.conn.QueryRow(`SELECT COUNT(*) FROM conversations WHERE id = 'tg:c1_topic_1'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("General debe seguir existiendo, obtuve %d (err=%v)", n, err)
	}
	var convID string
	if err := db.conn.QueryRow(`SELECT conversation_id FROM messages WHERE id = 'm1'`).Scan(&convID); err != nil || convID != "tg:c1_topic_1" {
		t.Fatalf("el mensaje del pseudo tema debe quedar en General, obtuve %q (err=%v)", convID, err)
	}
}
