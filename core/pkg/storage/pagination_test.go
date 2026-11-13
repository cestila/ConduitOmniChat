package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/cestila/ConduitOmniChat/core/pkg/domain"
	"github.com/google/uuid"
)

// Varios mensajes comparten el mismo segundo (Telegram y WhatsApp no dan más
// precisión). La paginación debe recorrerlos todos sin perder ni repetir.
func TestGetMessagesByConversationPaginatesSameTimestamp(t *testing.T) {
	db, err := NewDB(filepath.Join(t.TempDir(), "pagination.db"))
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	contactID := uuid.New().String()
	channelID := uuid.New().String()
	convID := uuid.New().String()
	sharedTimestamp := time.Unix(1700000000, 0)

	if _, err := db.conn.Exec("INSERT INTO contacts (id, display_name) VALUES (?, ?)", contactID, "Test User"); err != nil {
		t.Fatalf("insert contact failed: %v", err)
	}
	if _, err := db.conn.Exec("INSERT INTO contact_channels (id, contact_id, provider, remote_id) VALUES (?, ?, 'telegram', 'u1')", channelID, contactID); err != nil {
		t.Fatalf("insert channel failed: %v", err)
	}
	if err := db.EnsureConversation(ctx, &domain.Conversation{ID: convID, ContactID: &contactID, Title: "Chat"}); err != nil {
		t.Fatalf("EnsureConversation failed: %v", err)
	}

	const total = 6
	for i := 1; i <= total; i++ {
		msg := &domain.Message{
			ID:              uuid.New().String(),
			ConversationID:  convID,
			ChannelID:       channelID,
			RemoteMessageID: "m" + string(rune('0'+i)),
			SenderType:      domain.SenderContact,
			Body:            "mensaje " + string(rune('0'+i)),
			ContentType:     domain.ContentTypeText,
			Status:          domain.StatusDelivered,
			SentAt:          sharedTimestamp,
		}
		if err := db.SaveMessage(ctx, msg); err != nil {
			t.Fatalf("SaveMessage failed: %v", err)
		}
	}

	firstPage, err := db.GetMessagesByConversation(ctx, convID, 3, "")
	if err != nil {
		t.Fatalf("first page failed: %v", err)
	}
	if len(firstPage) != 3 {
		t.Fatalf("first page: expected 3 messages, got %d", len(firstPage))
	}

	secondPage, err := db.GetMessagesByConversation(ctx, convID, 3, firstPage[0].RemoteMessageId)
	if err != nil {
		t.Fatalf("second page failed: %v", err)
	}
	if len(secondPage) != 3 {
		t.Fatalf("second page: expected 3 messages, got %d", len(secondPage))
	}

	seen := map[string]bool{}
	for _, m := range append(firstPage, secondPage...) {
		if seen[m.RemoteMessageId] {
			t.Fatalf("el mensaje %s apareció en dos páginas", m.RemoteMessageId)
		}
		seen[m.RemoteMessageId] = true
	}
	if len(seen) != total {
		t.Fatalf("se esperaban %d mensajes distintos, se vieron %d", total, len(seen))
	}

	// No debe quedar ninguna página más.
	thirdPage, err := db.GetMessagesByConversation(ctx, convID, 3, secondPage[0].RemoteMessageId)
	if err != nil {
		t.Fatalf("third page failed: %v", err)
	}
	if len(thirdPage) != 0 {
		t.Fatalf("se esperaba el final del historial, got %d mensajes", len(thirdPage))
	}
}
