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

// seedReadSyncChannel crea un canal y una conversación que lo usa como canal activo.
func seedReadSyncChannel(t *testing.T, db *DB, provider domain.Provider, remoteID, convID string) string {
	t.Helper()
	ctx := context.Background()
	chanID := uuid.New().String()
	require.NoError(t, db.SaveChannel(ctx, &domain.ContactChannel{
		ID: chanID, Provider: provider, RemoteID: remoteID, Handle: remoteID, IsPreferred: true,
	}))
	require.NoError(t, db.EnsureConversation(ctx, &domain.Conversation{
		ID: convID, Title: remoteID, ActiveChannelID: &chanID,
	}))
	return chanID
}

func saveIncoming(t *testing.T, db *DB, convID, chanID, remoteID, sender string, sentAt time.Time) {
	t.Helper()
	require.NoError(t, db.SaveMessage(context.Background(), &domain.Message{
		ID: uuid.New().String(), ConversationID: convID, ChannelID: chanID,
		RemoteMessageID: remoteID, SenderType: domain.SenderContact, SenderRemoteID: sender,
		Body: "hola " + remoteID, ContentType: domain.ContentTypeText,
		Status: domain.StatusDelivered, SentAt: sentAt,
	}))
}

// S9: un mensaje ya leído que vuelve a llegar (historial, catch-up, resync) no vuelve a no leído.
func TestSaveMessageNeverDowngradesReadStatus(t *testing.T) {
	db, err := NewDB(":memory:")
	require.NoError(t, err)
	defer db.Close()
	ctx := context.Background()

	chanID := seedReadSyncChannel(t, db, domain.ProviderTelegram, "u42", "conv-s9")
	base := time.Now().Add(-time.Hour)
	saveIncoming(t, db, "conv-s9", chanID, "100", "", base)
	require.NoError(t, db.MarkMessagesAsRead(ctx, "conv-s9"))

	// El mismo mensaje remoto llega de nuevo con estado 'delivered' (con otro ID local).
	saveIncoming(t, db, "conv-s9", chanID, "100", "", base)

	unreads, err := db.GetUnreadRemoteMessageIDs(ctx, "conv-s9")
	require.NoError(t, err)
	assert.Empty(t, unreads, "un mensaje leído no debe volver a no leído al re-sincronizarse")
}

// S1 + S2: marcar leído deja el leído remoto pendiente con el remitente de cada mensaje.
func TestMarkConversationReadForSyncTracksPendingWithSender(t *testing.T) {
	db, err := NewDB(":memory:")
	require.NoError(t, err)
	defer db.Close()
	ctx := context.Background()

	chanID := seedReadSyncChannel(t, db, domain.ProviderWhatsApp, "1203@g.us", "conv-grp")
	base := time.Now().Add(-time.Hour)
	saveIncoming(t, db, "conv-grp", chanID, "A1", "111@s.whatsapp.net", base)
	saveIncoming(t, db, "conv-grp", chanID, "B1", "222@s.whatsapp.net", base.Add(time.Minute))

	marked, err := db.MarkConversationReadForSync(ctx, "conv-grp")
	require.NoError(t, err)
	assert.Equal(t, 2, marked)

	unreads, err := db.GetUnreadRemoteMessageIDs(ctx, "conv-grp")
	require.NoError(t, err)
	assert.Empty(t, unreads, "los mensajes quedan leídos localmente")

	refs, err := db.PendingReadSync(ctx, "conv-grp")
	require.NoError(t, err)
	assert.Equal(t, []ReadSyncRef{
		{ChannelID: chanID, RemoteMessageID: "A1", SenderRemoteID: "111@s.whatsapp.net"},
		{ChannelID: chanID, RemoteMessageID: "B1", SenderRemoteID: "222@s.whatsapp.net"},
	}, refs)

	// Una segunda marca no encuentra nada nuevo, pero el pendiente sigue ahí.
	marked, err = db.MarkConversationReadForSync(ctx, "conv-grp")
	require.NoError(t, err)
	assert.Equal(t, 0, marked)
	refs, err = db.PendingReadSync(ctx, "conv-grp")
	require.NoError(t, err)
	assert.Len(t, refs, 2)

	require.NoError(t, db.ClearReadSyncPending(ctx, chanID, []string{"A1"}))
	refs, err = db.PendingReadSync(ctx, "conv-grp")
	require.NoError(t, err)
	assert.Equal(t, []ReadSyncRef{{ChannelID: chanID, RemoteMessageID: "B1", SenderRemoteID: "222@s.whatsapp.net"}}, refs)
}

// Un mensaje leído desde el celular ya no necesita propagarse.
func TestReadFromPhoneClearsPendingSync(t *testing.T) {
	db, err := NewDB(":memory:")
	require.NoError(t, err)
	defer db.Close()
	ctx := context.Background()

	chanID := seedReadSyncChannel(t, db, domain.ProviderTelegram, "u7", "conv-phone")
	base := time.Now().Add(-time.Hour)
	saveIncoming(t, db, "conv-phone", chanID, "5", "", base)
	saveIncoming(t, db, "conv-phone", chanID, "6", "", base.Add(time.Minute))
	_, err = db.MarkConversationReadForSync(ctx, "conv-phone")
	require.NoError(t, err)

	_, err = db.MarkMessagesAsReadUpToRemoteID(ctx, chanID, "6")
	require.NoError(t, err)

	refs, err := db.PendingReadSync(ctx, "conv-phone")
	require.NoError(t, err)
	assert.Empty(t, refs)
}

// S2: el flush por proveedor encuentra las conversaciones con leídos pendientes.
func TestConversationsWithPendingReadSyncByProvider(t *testing.T) {
	db, err := NewDB(":memory:")
	require.NoError(t, err)
	defer db.Close()
	ctx := context.Background()

	waChan := seedReadSyncChannel(t, db, domain.ProviderWhatsApp, "9@s.whatsapp.net", "conv-wa")
	tgChan := seedReadSyncChannel(t, db, domain.ProviderTelegram, "u9", "conv-tg")
	base := time.Now().Add(-time.Hour)
	saveIncoming(t, db, "conv-wa", waChan, "W1", "", base)
	saveIncoming(t, db, "conv-tg", tgChan, "1", "", base)
	_, err = db.MarkConversationReadForSync(ctx, "conv-wa")
	require.NoError(t, err)
	_, err = db.MarkConversationReadForSync(ctx, "conv-tg")
	require.NoError(t, err)

	convs, err := db.ConversationsWithPendingReadSync(ctx, domain.ProviderWhatsApp)
	require.NoError(t, err)
	assert.Equal(t, []string{"conv-wa"}, convs)
}

// S8: el conteo de no leídos de una conversación refleja lecturas parciales.
func TestUnreadCountForConversation(t *testing.T) {
	db, err := NewDB(":memory:")
	require.NoError(t, err)
	defer db.Close()
	ctx := context.Background()

	chanID := seedReadSyncChannel(t, db, domain.ProviderTelegram, "u8", "conv-count")
	base := time.Now().Add(-time.Hour)
	saveIncoming(t, db, "conv-count", chanID, "1", "", base)
	saveIncoming(t, db, "conv-count", chanID, "2", "", base.Add(time.Minute))
	saveIncoming(t, db, "conv-count", chanID, "3", "", base.Add(2*time.Minute))

	_, err = db.MarkMessagesAsReadUpToRemoteID(ctx, chanID, "2")
	require.NoError(t, err)

	count, err := db.UnreadCount(ctx, "conv-count")
	require.NoError(t, err)
	assert.Equal(t, int32(1), count)
}

// Fallback de S7: la conversación de un canal se deduce de sus mensajes ya guardados.
func TestConversationIDForChannel(t *testing.T) {
	db, err := NewDB(":memory:")
	require.NoError(t, err)
	defer db.Close()
	ctx := context.Background()

	chanID := seedReadSyncChannel(t, db, domain.ProviderWhatsApp, "111@s.whatsapp.net", "conv-a")
	got, err := db.ConversationIDForChannel(ctx, chanID)
	require.NoError(t, err)
	assert.Equal(t, "", got, "sin mensajes no hay conversación que deducir")

	saveIncoming(t, db, "conv-a", chanID, "m1", "", time.Unix(1000, 0))
	got, err = db.ConversationIDForChannel(ctx, chanID)
	require.NoError(t, err)
	assert.Equal(t, "conv-a", got)
}

// S9: un leído aplicado por la vía general (MarkMessagesAsRead) tampoco queda pendiente de propagar.
func TestMarkMessagesAsReadClearsPendingSync(t *testing.T) {
	db, err := NewDB(":memory:")
	require.NoError(t, err)
	defer db.Close()
	ctx := context.Background()

	chanID := seedReadSyncChannel(t, db, domain.ProviderWhatsApp, "111@s.whatsapp.net", "conv-legacy")
	saveIncoming(t, db, "conv-legacy", chanID, "200", "", time.Now().Add(-time.Minute))
	_, err = db.MarkConversationReadForSync(ctx, "conv-legacy")
	require.NoError(t, err)

	require.NoError(t, db.MarkMessagesAsRead(ctx, "conv-legacy"))

	refs, err := db.PendingReadSync(ctx, "conv-legacy")
	require.NoError(t, err)
	assert.Empty(t, refs)
}
