package storage

import (
	"context"
	"testing"
	"time"

	"github.com/cestila/ConduitOmniChat/core/pkg/domain"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMarkMessagesAsReadAndGetUnread(t *testing.T) {
	db, err := NewDB(":memory:")
	require.NoError(t, err)
	defer db.Close()

	ctx := context.Background()

	// 1. Crear canal
	chanID := uuid.New().String()
	err = db.SaveChannel(ctx, &domain.ContactChannel{
		ID:          chanID,
		Provider:    domain.ProviderWhatsApp,
		RemoteID:    "123456789@s.whatsapp.net",
		Handle:      "Test User",
		IsPreferred: true,
	})
	require.NoError(t, err)

	// 2. Crear conversación
	convID := "conv-1"
	err = db.EnsureConversation(ctx, &domain.Conversation{
		ID:              convID,
		Title:           "Test User",
		ActiveChannelID: &chanID,
	})
	require.NoError(t, err)

	// 3. Guardar mensajes: 2 de contacto unread, 1 propio
	msg1 := &domain.Message{
		ID:              uuid.New().String(),
		ConversationID:  convID,
		ChannelID:       chanID,
		RemoteMessageID: "wamid-1",
		SenderType:      domain.SenderContact,
		Body:            "Hola 1",
		ContentType:     domain.ContentTypeText,
		Status:          domain.StatusDelivered,
		SentAt:          time.Now().Add(-2 * time.Minute),
	}
	msg2 := &domain.Message{
		ID:              uuid.New().String(),
		ConversationID:  convID,
		ChannelID:       chanID,
		RemoteMessageID: "wamid-2",
		SenderType:      domain.SenderContact,
		Body:            "Hola 2",
		ContentType:     domain.ContentTypeText,
		Status:          domain.StatusDelivered,
		SentAt:          time.Now().Add(-1 * time.Minute),
	}
	msgMe := &domain.Message{
		ID:              uuid.New().String(),
		ConversationID:  convID,
		ChannelID:       chanID,
		RemoteMessageID: "wamid-me",
		SenderType:      domain.SenderMe,
		Body:            "Respuesta",
		ContentType:     domain.ContentTypeText,
		Status:          domain.StatusSent,
		SentAt:          time.Now(),
	}

	require.NoError(t, db.SaveMessage(ctx, msg1))
	require.NoError(t, db.SaveMessage(ctx, msg2))
	require.NoError(t, db.SaveMessage(ctx, msgMe))

	// Obtener no leídos antes de marcar
	unreads, err := db.GetUnreadRemoteMessageIDs(ctx, convID)
	require.NoError(t, err)
	assert.Len(t, unreads, 2)
	assert.Contains(t, unreads, "wamid-1")
	assert.Contains(t, unreads, "wamid-2")

	// Marcar conversación como leída
	marked, err := db.MarkMessagesAsReadAndReturnRemoteIDs(ctx, convID)
	require.NoError(t, err)
	assert.Len(t, marked, 2)
	assert.Contains(t, marked, "wamid-1")
	assert.Contains(t, marked, "wamid-2")

	// Verificar que ya no hay no leídos
	unreadsAfter, err := db.GetUnreadRemoteMessageIDs(ctx, convID)
	require.NoError(t, err)
	assert.Empty(t, unreadsAfter)

	// Test marcar por RemoteIDs (cuando llega lectura desde celular)
	msg3 := &domain.Message{
		ID:              uuid.New().String(),
		ConversationID:  convID,
		ChannelID:       chanID,
		RemoteMessageID: "wamid-3",
		SenderType:      domain.SenderContact,
		Body:            "Hola 3",
		ContentType:     domain.ContentTypeText,
		Status:          domain.StatusDelivered,
		SentAt:          time.Now(),
	}
	require.NoError(t, db.SaveMessage(ctx, msg3))

	affected, err := db.MarkMessagesAsReadByRemoteIDs(ctx, chanID, []string{"wamid-3"})
	require.NoError(t, err)
	assert.Equal(t, int64(1), affected)

	unreadsFinal, err := db.GetUnreadRemoteMessageIDs(ctx, convID)
	require.NoError(t, err)
	assert.Empty(t, unreadsFinal)
}

func TestMarkMessagesAsReadUpToRemoteID(t *testing.T) {
	db, err := NewDB(":memory:")
	require.NoError(t, err)
	defer db.Close()

	ctx := context.Background()
	chanID := uuid.New().String()
	require.NoError(t, db.SaveChannel(ctx, &domain.ContactChannel{
		ID:          chanID,
		Provider:    domain.ProviderTelegram,
		RemoteID:    "tg-chat-1",
		Handle:      "TG Chat",
		IsPreferred: true,
	}))

	convID := "telegram:tg-chat-1"
	require.NoError(t, db.EnsureConversation(ctx, &domain.Conversation{
		ID:              convID,
		Title:           "TG Chat",
		ActiveChannelID: &chanID,
	}))

	t1 := time.Now().Add(-5 * time.Minute)
	t2 := time.Now().Add(-4 * time.Minute)
	t3 := time.Now().Add(-1 * time.Minute)

	require.NoError(t, db.SaveMessage(ctx, &domain.Message{
		ID:              uuid.New().String(),
		ConversationID:  convID,
		ChannelID:       chanID,
		RemoteMessageID: "101",
		SenderType:      domain.SenderContact,
		Body:            "Msg 1",
		Status:          domain.StatusDelivered,
		SentAt:          t1,
	}))
	require.NoError(t, db.SaveMessage(ctx, &domain.Message{
		ID:              uuid.New().String(),
		ConversationID:  convID,
		ChannelID:       chanID,
		RemoteMessageID: "102",
		SenderType:      domain.SenderContact,
		Body:            "Msg 2",
		Status:          domain.StatusDelivered,
		SentAt:          t2,
	}))
	require.NoError(t, db.SaveMessage(ctx, &domain.Message{
		ID:              uuid.New().String(),
		ConversationID:  convID,
		ChannelID:       chanID,
		RemoteMessageID: "103",
		SenderType:      domain.SenderContact,
		Body:            "Msg 3",
		Status:          domain.StatusDelivered,
		SentAt:          t3,
	}))

	// Marcar hasta 102 inclusive
	affected, err := db.MarkMessagesAsReadUpToRemoteID(ctx, chanID, "102")
	require.NoError(t, err)
	assert.Equal(t, int64(2), affected)

	unreads, err := db.GetUnreadRemoteMessageIDs(ctx, convID)
	require.NoError(t, err)
	assert.Len(t, unreads, 1)
	assert.Equal(t, "103", unreads[0])
}
