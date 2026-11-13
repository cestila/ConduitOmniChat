package storage

import (
	"context"
	"testing"
	"time"

	"github.com/cestila/ConduitOmniChat/core/pkg/domain"
	"github.com/google/uuid"
)

func TestAddAndToggleReactions(t *testing.T) {
	ctx := context.Background()
	db, err := NewDB(":memory:")
	if err != nil {
		t.Fatalf("NewDB failed: %v", err)
	}
	defer db.Close()

	convID := uuid.New().String()
	chanID := uuid.New().String()
	msgID := uuid.New().String()

	_, _ = db.conn.Exec("INSERT INTO conversations (id, title, last_message_at) VALUES (?, ?, ?)", convID, "Chat Test", time.Now())
	_, _ = db.conn.Exec("INSERT INTO contact_channels (id, contact_id, provider, remote_id) VALUES (?, ?, ?, ?)", chanID, convID, "telegram", "tg-123")

	_ = db.SaveMessage(ctx, &domain.Message{
		ID:              msgID,
		ConversationID:  convID,
		ChannelID:       chanID,
		RemoteMessageID: "rem-1",
		SenderType:      domain.SenderContact,
		Body:            "Mensaje con reacciones",
		ContentType:     domain.ContentTypeText,
		Status:          domain.StatusDelivered,
		SentAt:          time.Now(),
	})

	// 1. Agregar reacción de un contacto
	if err := db.AddReaction(ctx, msgID, "user-456", "👍"); err != nil {
		t.Fatalf("AddReaction falló: %v", err)
	}

	// 2. Toggle reacción de 'me' (debe agregar)
	added, err := db.ToggleReaction(ctx, msgID, "me", "❤️")
	if err != nil {
		t.Fatalf("ToggleReaction falló: %v", err)
	}
	if !added {
		t.Fatalf("se esperaba added=true en primer toggle")
	}

	// 3. Consultar reacciones
	reactionsMap, err := db.GetReactionsByMessageIDs(ctx, []string{msgID})
	if err != nil {
		t.Fatalf("GetReactionsByMessageIDs falló: %v", err)
	}
	reactions := reactionsMap[msgID]
	if len(reactions) != 2 {
		t.Fatalf("se esperaban 2 reacciones, se obtuvieron %d", len(reactions))
	}

	foundHeart := false
	for _, r := range reactions {
		if r.Emoji == "❤️" {
			foundHeart = true
			if !r.IncludesMe {
				t.Errorf("se esperaba IncludesMe=true para ❤️")
			}
			if r.Count != 1 {
				t.Errorf("se esperaba Count=1 para ❤️, obtenido %d", r.Count)
			}
		}
	}
	if !foundHeart {
		t.Errorf("no se encontró la reacción ❤️")
	}

	// 4. Toggle de nuevo 'me' con ❤️ (debe remover)
	addedAgain, err := db.ToggleReaction(ctx, msgID, "me", "❤️")
	if err != nil {
		t.Fatalf("segundo ToggleReaction falló: %v", err)
	}
	if addedAgain {
		t.Fatalf("se esperaba added=false en segundo toggle (remover)")
	}

	reactionsMap2, _ := db.GetReactionsByMessageIDs(ctx, []string{msgID})
	reactions2 := reactionsMap2[msgID]
	if len(reactions2) != 1 {
		t.Fatalf("se esperaba 1 reacción tras remover ❤️, hay %d", len(reactions2))
	}
	if reactions2[0].Emoji != "👍" {
		t.Errorf("la reacción restante debía ser 👍, se obtuvo %s", reactions2[0].Emoji)
	}
}

func TestGetMessagesIncludesRepliesAndReactions(t *testing.T) {
	ctx := context.Background()
	db, err := NewDB(":memory:")
	if err != nil {
		t.Fatalf("NewDB failed: %v", err)
	}
	defer db.Close()

	convID := uuid.New().String()
	chanID := uuid.New().String()
	m1ID := uuid.New().String()
	m2ID := uuid.New().String()

	_, _ = db.conn.Exec("INSERT INTO conversations (id, title, last_message_at) VALUES (?, ?, ?)", convID, "Chat Quotes", time.Now())
	_, _ = db.conn.Exec("INSERT INTO contact_channels (id, contact_id, provider, remote_id) VALUES (?, ?, ?, ?)", chanID, convID, "whatsapp", "wa-123")

	now := time.Now()
	_ = db.SaveMessage(ctx, &domain.Message{
		ID:              m1ID,
		ConversationID:  convID,
		ChannelID:       chanID,
		RemoteMessageID: "rem-m1",
		SenderType:      domain.SenderContact,
		Body:            "Hola mundo original",
		ContentType:     domain.ContentTypeText,
		Status:          domain.StatusDelivered,
		SentAt:          now.Add(-1 * time.Minute),
	})

	_ = db.SaveMessage(ctx, &domain.Message{
		ID:               m2ID,
		ConversationID:   convID,
		ChannelID:        chanID,
		RemoteMessageID:  "rem-m2",
		SenderType:       domain.SenderMe,
		Body:             "Respuesta citando el original",
		ContentType:      domain.ContentTypeText,
		Status:           domain.StatusSent,
		SentAt:           now,
		ReplyToMessageID: &m1ID,
	})

	_ = db.AddReaction(ctx, m2ID, "user-wa", "🔥")

	messages, err := db.GetMessagesByConversation(ctx, convID, 10, "")
	if err != nil {
		t.Fatalf("GetMessagesByConversation falló: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("se esperaban 2 mensajes, se obtuvieron %d", len(messages))
	}

	replyMsg := messages[1]
	if replyMsg.ReplyToMessageId != m1ID {
		t.Errorf("ReplyToMessageId esperado %s, obtenido %s", m1ID, replyMsg.ReplyToMessageId)
	}
	if replyMsg.ReplyToBody != "Hola mundo original" {
		t.Errorf("ReplyToBody esperado 'Hola mundo original', obtenido %q", replyMsg.ReplyToBody)
	}
	if replyMsg.ReplyToSender != string(domain.SenderContact) {
		t.Errorf("ReplyToSender esperado %q, obtenido %q", domain.SenderContact, replyMsg.ReplyToSender)
	}

	if len(replyMsg.Reactions) != 1 {
		t.Fatalf("se esperaba 1 reacción en m2, se obtuvieron %d", len(replyMsg.Reactions))
	}
	if replyMsg.Reactions[0].Emoji != "🔥" {
		t.Errorf("reacción esperada 🔥, obtenida %s", replyMsg.Reactions[0].Emoji)
	}
}
