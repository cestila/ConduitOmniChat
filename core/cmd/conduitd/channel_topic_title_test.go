package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/cestila/ConduitOmniChat/core/pkg/domain"
	"github.com/cestila/ConduitOmniChat/core/pkg/storage"
	conduitv1 "github.com/cestila/ConduitOmniChat/sdk/conduitv1"
)

// S4: un tema de foro nunca se titula con el nombre del grupo cuando el plugin
// todavía no conoce el nombre real del tema (ChannelName vacío).
type topicTitleFixture struct {
	t   *testing.T
	db  *storage.DB
	ctx context.Context
}

func newTopicTitleFixture(t *testing.T) *topicTitleFixture {
	t.Helper()
	db, err := storage.NewDB(filepath.Join(t.TempDir(), "topic-title.db"))
	if err != nil {
		t.Fatalf("error creando base: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return &topicTitleFixture{t: t, db: db, ctx: context.Background()}
}

func (f *topicTitleFixture) sync(ev *conduitv1.PluginEvent) {
	f.t.Helper()
	handleChannelSync(f.ctx, f.db, telegramCfg, ev)
}

// title devuelve el título de la conversación, o "" si no existe.
func (f *topicTitleFixture) title(convID string) (string, bool) {
	f.t.Helper()
	conv, err := f.db.GetConversationByID(f.ctx, convID)
	if err != nil || conv == nil {
		return "", false
	}
	return conv.Title, true
}

func (f *topicTitleFixture) requireTitle(convID, want string) {
	f.t.Helper()
	got, ok := f.title(convID)
	if !ok {
		f.t.Fatalf("%s debería existir con el título %q", convID, want)
	}
	if got != want {
		f.t.Fatalf("%s debería titularse %q, obtuve %q", convID, want, got)
	}
}

func (f *topicTitleFixture) addConv(convID, title string) {
	f.t.Helper()
	chanID := "ch-" + convID
	if err := f.db.SaveChannel(f.ctx, &domain.ContactChannel{ID: chanID, Provider: domain.ProviderTelegram, RemoteID: convID, Handle: title, IsPreferred: true}); err != nil {
		f.t.Fatalf("SaveChannel: %v", err)
	}
	if err := f.db.EnsureConversation(f.ctx, &domain.Conversation{
		ID: convID, IsGroup: true, Title: title, LastMessageAt: time.Unix(1000, 0),
		ActiveChannelID: &chanID, ServerID: "c1", ServerName: "Mi Grupo", ParentChannelID: "c1", ChannelType: "forum",
	}); err != nil {
		f.t.Fatalf("EnsureConversation: %v", err)
	}
}

func topicSyncEvent(remoteID, channelName, senderName string) *conduitv1.PluginEvent {
	return &conduitv1.PluginEvent{
		EventType:      "channel.sync",
		RemoteSenderId: remoteID,
		ChannelName:    channelName,
		SenderName:     senderName,
		ServerId:       "c1",
		ServerName:     "Mi Grupo",
		ChannelType:    "forum",
		SentAtUnix:     2000,
	}
}

// (a) Un tema sin nombre real no crea una conversación con el nombre del grupo.
func TestChannelSyncTopicWithoutNameDoesNotCreateGroupNamedConversation(t *testing.T) {
	f := newTopicTitleFixture(t)

	f.sync(topicSyncEvent("c1_topic_5", "", "Mi Grupo"))

	convID := "telegram:c1_topic_5"
	if title, ok := f.title(convID); ok {
		t.Fatalf("no debería crearse %s, quedó titulada %q", convID, title)
	}
	if n := countConversations(t, f.db); n != 0 {
		t.Fatalf("no debería crearse ninguna conversación, obtuve %d", n)
	}
}

// (b) Si la conversación del tema ya existe, su título se conserva.
func TestChannelSyncTopicWithoutNameKeepsExistingTitle(t *testing.T) {
	f := newTopicTitleFixture(t)
	f.addConv("telegram:c1_topic_5", "Backend Go")

	f.sync(topicSyncEvent("c1_topic_5", "", "Mi Grupo"))

	f.requireTitle("telegram:c1_topic_5", "Backend Go")
	if n := countConversations(t, f.db); n != 1 {
		t.Fatalf("no debería crearse otra conversación, obtuve %d", n)
	}
}

// (c) Un canal que no es tema conserva el respaldo al nombre del remitente.
func TestChannelSyncNonTopicFallsBackToSenderName(t *testing.T) {
	f := newTopicTitleFixture(t)

	f.sync(&conduitv1.PluginEvent{
		EventType:      "channel.sync",
		RemoteSenderId: "c2",
		ChannelName:    "",
		SenderName:     "Grupo WhatsApp",
		ServerId:       "c2",
		ServerName:     "Grupo WhatsApp",
		ChannelType:    "group",
		SentAtUnix:     2000,
	})

	f.requireTitle("telegram:c2", "Grupo WhatsApp")
}

// (d) Un tema con nombre real se crea y se renombra como hasta ahora.
func TestChannelSyncTopicWithNameCreatesAndRenames(t *testing.T) {
	f := newTopicTitleFixture(t)

	f.sync(topicSyncEvent("c1_topic_5", "Backend Go", "Mi Grupo"))
	f.requireTitle("telegram:c1_topic_5", "Backend Go")

	f.addConv("telegram:c1_topic_6", "General")
	f.sync(topicSyncEvent("c1_topic_6", "Backend Go", "Mi Grupo"))
	f.requireTitle("telegram:c1_topic_6", "Backend Go")
}

func countConversations(t *testing.T, db *storage.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM conversations`).Scan(&n); err != nil {
		t.Fatalf("count conversations: %v", err)
	}
	return n
}
