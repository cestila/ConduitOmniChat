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

func newMutationTestDB(t *testing.T) *storage.DB {
	t.Helper()
	db, err := storage.NewDB(filepath.Join(t.TempDir(), "mutation.db"))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return db
}

func TestApplyEditEventWithChannel(t *testing.T) {
	db := newMutationTestDB(t)
	bus := eventbus.NewBus()
	defer bus.Close()
	ctx := context.Background()

	sub := bus.Subscribe(eventbus.EventMessageUpdated)
	defer bus.Unsubscribe(eventbus.EventMessageUpdated, sub)

	origTime := time.Date(2025, 5, 10, 15, 0, 0, 0, time.UTC)
	chanID := seedChat(t, db, domain.ProviderWhatsApp, "+5491100001111", "conv-edit-1", "cont-edit-1", false, origTime)
	seedIncoming(t, db, "conv-edit-1", chanID, "rem-msg-10", origTime)

	editTimeUnix := int64(1746889260)
	applyEditEvent(ctx, db, bus, domain.ProviderWhatsApp, &conduitv1.PluginEvent{
		EventType:       "message.edited",
		RemoteMessageId: "rem-msg-10",
		ChatJid:         "+5491100001111",
		RemoteSenderId:  "+5491100001111",
		Body:            "Mensaje editado correctamente",
		IsEdited:        true,
		EditedAtUnix:    editTimeUnix,
	})

	select {
	case ev := <-sub:
		msg, ok := ev.Payload.(*domain.Message)
		require.True(t, ok)
		assert.Equal(t, "Mensaje editado correctamente", msg.Body)
		assert.True(t, msg.IsEdited)
		require.NotNil(t, msg.EditedAt)
		assert.Equal(t, editTimeUnix, msg.EditedAt.Unix())
	case <-time.After(time.Second):
		t.Fatal("se esperaba EventMessageUpdated")
	}

	// Verificar persistencia en base de datos
	found, err := db.FindMessageByRemoteID(ctx, chanID, "rem-msg-10")
	require.NoError(t, err)
	assert.Equal(t, "Mensaje editado correctamente", found.Body)
	assert.True(t, found.IsEdited)

	// Verificar que la fecha de la conversación no fue tocada
	conv, err := db.GetConversationByID(ctx, "conv-edit-1")
	require.NoError(t, err)
	assert.Equal(t, origTime.Unix(), conv.LastMessageAtUnix)
}

func TestApplyEditEventFallbackWithoutChatJid(t *testing.T) {
	db := newMutationTestDB(t)
	bus := eventbus.NewBus()
	defer bus.Close()
	ctx := context.Background()

	sub := bus.Subscribe(eventbus.EventMessageUpdated)
	defer bus.Unsubscribe(eventbus.EventMessageUpdated, sub)

	origTime := time.Date(2025, 5, 10, 15, 0, 0, 0, time.UTC)
	chanID := seedChat(t, db, domain.ProviderTelegram, "u12345", "conv-edit-2", "cont-edit-2", false, origTime)
	seedIncoming(t, db, "conv-edit-2", chanID, "rem-msg-20", origTime)

	// Evento sin ChatJid ni RemoteSenderId (búsqueda fallback por RemoteMessageId)
	applyEditEvent(ctx, db, bus, domain.ProviderTelegram, &conduitv1.PluginEvent{
		EventType:       "message.edited",
		RemoteMessageId: "rem-msg-20",
		Body:            "Texto nuevo sin chat context",
		IsEdited:        true,
		EditedAtUnix:    time.Now().Unix(),
	})

	select {
	case ev := <-sub:
		msg, ok := ev.Payload.(*domain.Message)
		require.True(t, ok)
		assert.Equal(t, "Texto nuevo sin chat context", msg.Body)
		assert.True(t, msg.IsEdited)
	case <-time.After(time.Second):
		t.Fatal("se esperaba EventMessageUpdated vía fallback")
	}
}

func TestApplyDeleteEventWithChannel(t *testing.T) {
	db := newMutationTestDB(t)
	bus := eventbus.NewBus()
	defer bus.Close()
	ctx := context.Background()

	sub := bus.Subscribe(eventbus.EventMessageDeleted)
	defer bus.Unsubscribe(eventbus.EventMessageDeleted, sub)

	origTime := time.Date(2025, 5, 10, 15, 0, 0, 0, time.UTC)
	chanID := seedChat(t, db, domain.ProviderWhatsApp, "+5491122223333", "conv-del-1", "cont-del-1", false, origTime)
	seedIncoming(t, db, "conv-del-1", chanID, "rem-del-10", origTime)

	applyDeleteEvent(ctx, db, bus, domain.ProviderWhatsApp, &conduitv1.PluginEvent{
		EventType:       "message.deleted",
		RemoteMessageId: "rem-del-10",
		ChatJid:         "+5491122223333",
		RemoteSenderId:  "+5491122223333",
	})

	select {
	case ev := <-sub:
		deletedID, ok := ev.Payload.(string)
		require.True(t, ok)
		assert.Equal(t, "msg-"+chanID+"-rem-del-10", deletedID)
	case <-time.After(time.Second):
		t.Fatal("se esperaba EventMessageDeleted")
	}

	// Verificar que el mensaje ya no existe en la base de datos
	_, err := db.FindMessageByRemoteID(ctx, chanID, "rem-del-10")
	require.Error(t, err)

	// Conversación no se ve afectada
	conv, err := db.GetConversationByID(ctx, "conv-del-1")
	require.NoError(t, err)
	assert.Equal(t, origTime.Unix(), conv.LastMessageAtUnix)
}

func TestApplyDeleteEventFallbackWithoutChatJid(t *testing.T) {
	db := newMutationTestDB(t)
	bus := eventbus.NewBus()
	defer bus.Close()
	ctx := context.Background()

	sub := bus.Subscribe(eventbus.EventMessageDeleted)
	defer bus.Unsubscribe(eventbus.EventMessageDeleted, sub)

	origTime := time.Date(2025, 5, 10, 15, 0, 0, 0, time.UTC)
	chanID := seedChat(t, db, domain.ProviderTelegram, "u55555", "conv-del-2", "cont-del-2", false, origTime)
	seedIncoming(t, db, "conv-del-2", chanID, "rem-del-20", origTime)

	// Evento de Telegram DeleteMessages (sólo RemoteMessageId, sin chat context)
	applyDeleteEvent(ctx, db, bus, domain.ProviderTelegram, &conduitv1.PluginEvent{
		EventType:       "message.deleted",
		RemoteMessageId: "rem-del-20",
	})

	select {
	case ev := <-sub:
		deletedID, ok := ev.Payload.(string)
		require.True(t, ok)
		assert.Equal(t, "msg-"+chanID+"-rem-del-20", deletedID)
	case <-time.After(time.Second):
		t.Fatal("se esperaba EventMessageDeleted vía fallback")
	}

	_, err := db.FindMessageByRemoteID(ctx, "", "rem-del-20")
	require.Error(t, err)
}
