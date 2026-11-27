package storage

import (
	"context"
	"path/filepath"
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
