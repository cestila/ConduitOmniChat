package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/cestila/ConduitOmniChat/core/pkg/domain"
	"github.com/cestila/ConduitOmniChat/core/pkg/eventbus"
	"github.com/cestila/ConduitOmniChat/core/pkg/storage"
	conduitv1 "github.com/cestila/ConduitOmniChat/sdk/conduitv1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newReadTestDB(t *testing.T) *storage.DB {
	t.Helper()
	db, err := storage.NewDB(filepath.Join(t.TempDir(), "read.db"))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return db
}

// seedChat crea contacto opcional, canal y conversación; devuelve el ID del canal.
func seedChat(t *testing.T, db *storage.DB, provider domain.Provider, remoteID, convID, contactID string, isGroup bool, lastAt time.Time) string {
	t.Helper()
	ctx := context.Background()
	if contactID != "" {
		require.NoError(t, db.SaveContact(ctx, &domain.Contact{ID: contactID, DisplayName: "Contacto"}))
	}
	chanID := "chan-" + remoteID
	require.NoError(t, db.SaveChannel(ctx, &domain.ContactChannel{
		ID: chanID, ContactID: contactID, Provider: provider, RemoteID: remoteID, Handle: remoteID, IsPreferred: true,
	}))
	conv := &domain.Conversation{ID: convID, IsGroup: isGroup, Title: "Chat", LastMessageAt: lastAt, ActiveChannelID: &chanID}
	if contactID != "" {
		conv.ContactID = &contactID
	}
	require.NoError(t, db.EnsureConversation(ctx, conv))
	return chanID
}

func seedIncoming(t *testing.T, db *storage.DB, convID, chanID, remoteID string, sentAt time.Time) {
	t.Helper()
	require.NoError(t, db.SaveMessage(context.Background(), &domain.Message{
		ID: "msg-" + chanID + "-" + remoteID, ConversationID: convID, ChannelID: chanID,
		RemoteMessageID: remoteID, SenderType: domain.SenderContact, Body: "hola",
		ContentType: domain.ContentTypeText, Status: domain.StatusDelivered, SentAt: sentAt,
	}))
}

func receiveReadEvent(t *testing.T, sub eventbus.Subscription) *conduitv1.ConversationReadEvent {
	t.Helper()
	select {
	case e := <-sub:
		return e.Payload.(*conduitv1.ConversationReadEvent)
	case <-time.After(time.Second):
		t.Fatal("se esperaba un ConversationReadEvent")
		return nil
	}
}

func counts(t *testing.T, db *storage.DB) (int, int) {
	t.Helper()
	contacts, err := db.ListContacts(context.Background())
	require.NoError(t, err)
	convs, err := db.ListConversations(context.Background(), 100, 0)
	require.NoError(t, err)
	return len(contacts), len(convs)
}

// S7: una lectura de un chat desconocido no crea contactos, canales ni conversaciones.
func TestApplyReadEventUnknownChatCreatesNothing(t *testing.T) {
	db := newReadTestDB(t)
	bus := eventbus.NewBus()
	defer bus.Close()
	sub := bus.Subscribe(eventbus.EventConversationRead)
	ctx := context.Background()

	contactsBefore, convsBefore := counts(t, db)
	applyReadEvent(ctx, db, bus, "telegram", domain.ProviderTelegram, &conduitv1.PluginEvent{
		EventType: "messages.read", ChatJid: "999", RemoteMessageId: "10", SentAtUnix: time.Now().Unix(),
	})

	contactsAfter, convsAfter := counts(t, db)
	assert.Equal(t, contactsBefore, contactsAfter)
	assert.Equal(t, convsBefore, convsAfter)
	ch, err := db.GetChannelByProviderAndRemoteID(ctx, string(domain.ProviderTelegram), "999")
	assert.Error(t, err)
	assert.Nil(t, ch)
	assert.Len(t, sub, 0, "no debe publicarse ningún evento")
}

// S7 + S8: lectura existente con max_id numérico marca hasta ese ID, no reordena y publica el restante.
func TestApplyReadEventDMUpToMaxID(t *testing.T) {
	db := newReadTestDB(t)
	bus := eventbus.NewBus()
	defer bus.Close()
	sub := bus.Subscribe(eventbus.EventConversationRead)
	ctx := context.Background()

	lastAt := time.Unix(5000, 0)
	chanID := seedChat(t, db, domain.ProviderTelegram, "42", "contact-1", "contact-1", false, lastAt)
	for i, id := range []string{"1", "2", "3", "4"} {
		seedIncoming(t, db, "contact-1", chanID, id, time.Unix(int64(1000+i), 0))
	}
	require.NoError(t, db.EnsureConversation(ctx, &domain.Conversation{ID: "contact-1", LastMessageAt: lastAt}))
	before, err := db.GetConversationByID(ctx, "contact-1")
	require.NoError(t, err)

	applyReadEvent(ctx, db, bus, "telegram", domain.ProviderTelegram, &conduitv1.PluginEvent{
		EventType: "messages.read", ChatJid: "42", RemoteMessageId: "2", SentAtUnix: time.Now().Unix(),
	})

	unread, err := db.UnreadCount(ctx, "contact-1")
	require.NoError(t, err)
	assert.Equal(t, int32(2), unread)
	after, err := db.GetConversationByID(ctx, "contact-1")
	require.NoError(t, err)
	assert.Equal(t, before.LastMessageAtUnix, after.LastMessageAtUnix, "last_message_at no cambia")

	ev := receiveReadEvent(t, sub)
	assert.Equal(t, "contact-1", ev.ConversationId)
	assert.Equal(t, int32(2), ev.UnreadCount)
	assert.Equal(t, "2", ev.MaxRemoteId)
	assert.Equal(t, []string{"2"}, ev.RemoteMessageIds)
}

// S6/S7: lectura de grupo de Telegram con ServerId se aplica a la conversación pluginName:chat.
func TestApplyReadEventGroupWithServerID(t *testing.T) {
	db := newReadTestDB(t)
	bus := eventbus.NewBus()
	defer bus.Close()
	sub := bus.Subscribe(eventbus.EventConversationRead)
	ctx := context.Background()

	chanID := seedChat(t, db, domain.ProviderTelegram, "-100", "telegram:-100", "", true, time.Unix(5000, 0))
	for i, id := range []string{"5", "6", "7"} {
		seedIncoming(t, db, "telegram:-100", chanID, id, time.Unix(int64(1000+i), 0))
	}

	applyReadEvent(ctx, db, bus, "telegram", domain.ProviderTelegram, &conduitv1.PluginEvent{
		EventType: "messages.read", ChatJid: "-100", ServerId: "srv", RemoteMessageId: "6",
	})

	unread, err := db.UnreadCount(ctx, "telegram:-100")
	require.NoError(t, err)
	assert.Equal(t, int32(1), unread)
	ev := receiveReadEvent(t, sub)
	assert.Equal(t, "telegram:-100", ev.ConversationId)
	assert.Equal(t, int32(1), ev.UnreadCount)
}

// Si la conversación esperada no existe, se usa la de los mensajes ya guardados del canal.
func TestApplyReadEventFallsBackToMessageConversation(t *testing.T) {
	db := newReadTestDB(t)
	bus := eventbus.NewBus()
	defer bus.Close()
	sub := bus.Subscribe(eventbus.EventConversationRead)
	ctx := context.Background()

	// Canal sin contacto: la conversación de DM por contacto no existe.
	chanID := seedChat(t, db, domain.ProviderWhatsApp, "555@s.whatsapp.net", "legacy-conv", "", false, time.Unix(5000, 0))
	seedIncoming(t, db, "legacy-conv", chanID, "A", time.Unix(1000, 0))

	applyReadEvent(ctx, db, bus, "whatsapp", domain.ProviderWhatsApp, &conduitv1.PluginEvent{
		EventType: "messages.read", ChatJid: "555@s.whatsapp.net", RemoteMessageId: "A",
	})

	unread, err := db.UnreadCount(ctx, "legacy-conv")
	require.NoError(t, err)
	assert.Equal(t, int32(0), unread)
	assert.Equal(t, "legacy-conv", receiveReadEvent(t, sub).ConversationId)
}

// WhatsApp: una lista CSV de IDs marca solo esos mensajes.
func TestApplyReadEventWhatsAppCSVIDs(t *testing.T) {
	db := newReadTestDB(t)
	bus := eventbus.NewBus()
	defer bus.Close()
	sub := bus.Subscribe(eventbus.EventConversationRead)
	ctx := context.Background()

	chanID := seedChat(t, db, domain.ProviderWhatsApp, "777@s.whatsapp.net", "contact-w", "contact-w", false, time.Unix(5000, 0))
	for i, id := range []string{"AAA", "BBB", "CCC"} {
		seedIncoming(t, db, "contact-w", chanID, id, time.Unix(int64(1000+i), 0))
	}

	applyReadEvent(ctx, db, bus, "whatsapp", domain.ProviderWhatsApp, &conduitv1.PluginEvent{
		EventType: "messages.read", ChatJid: "777@s.whatsapp.net", RemoteMessageId: "AAA, CCC",
	})

	unread, err := db.UnreadCount(ctx, "contact-w")
	require.NoError(t, err)
	assert.Equal(t, int32(1), unread)
	ev := receiveReadEvent(t, sub)
	assert.Equal(t, []string{"AAA", "CCC"}, ev.RemoteMessageIds)
	assert.Equal(t, int32(1), ev.UnreadCount)
}
