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
)

type reconcileFixture struct {
	t   *testing.T
	db  *storage.DB
	bus *eventbus.Bus
	ctx context.Context
}

func newReconcileFixture(t *testing.T) *reconcileFixture {
	t.Helper()
	db, err := storage.NewDB(filepath.Join(t.TempDir(), "reconcile.db"))
	if err != nil {
		t.Fatalf("error creando base: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return &reconcileFixture{t: t, db: db, bus: eventbus.NewBus(), ctx: context.Background()}
}

// addConv crea una conversación de canal/tema con su fila contact_channels (sin contacto),
// un mensaje, un adjunto y una reacción. convID y remoteID son independientes para poder
// simular otros plugins con los mismos remote ids.
func (f *reconcileFixture) addConv(convID, provider, remoteID, serverID string, withChannel bool) {
	f.t.Helper()
	chanID := "ch-" + convID
	if withChannel {
		if err := f.db.SaveChannel(f.ctx, &domain.ContactChannel{ID: chanID, Provider: domain.Provider(provider), RemoteID: remoteID, Handle: convID, IsPreferred: true}); err != nil {
			f.t.Fatalf("SaveChannel: %v", err)
		}
	}
	if err := f.db.EnsureConversation(f.ctx, &domain.Conversation{
		ID: convID, IsGroup: true, Title: convID, LastMessageAt: time.Unix(1000, 0),
		ActiveChannelID: &chanID, ServerID: serverID, ParentChannelID: serverID, ChannelType: "forum",
	}); err != nil {
		f.t.Fatalf("EnsureConversation: %v", err)
	}
	msgID := "msg-" + convID
	if err := f.db.SaveMessage(f.ctx, &domain.Message{
		ID: msgID, ConversationID: convID, ChannelID: chanID, RemoteMessageID: "r-" + convID,
		SenderType: domain.SenderContact, Body: "hola " + convID, ContentType: domain.ContentTypeText,
		Status: domain.StatusDelivered, SentAt: time.Unix(1000, 0),
	}); err != nil {
		f.t.Fatalf("SaveMessage: %v", err)
	}
	if err := f.db.SaveAttachment(f.ctx, &domain.Attachment{ID: "att-" + convID, MessageID: msgID, MimeType: "image/png", DownloadStatus: domain.MediaRemoteOnly}); err != nil {
		f.t.Fatalf("SaveAttachment: %v", err)
	}
	if err := f.db.AddReaction(f.ctx, msgID, "u1", "👍"); err != nil {
		f.t.Fatalf("AddReaction: %v", err)
	}
}

func (f *reconcileFixture) count(query string, args ...any) int {
	f.t.Helper()
	var n int
	if err := f.db.QueryRowContext(f.ctx, query, args...).Scan(&n); err != nil {
		f.t.Fatalf("count %q: %v", query, err)
	}
	return n
}

// present informa si sobrevive la conversación y todas sus filas dependientes.
func (f *reconcileFixture) present(convID string) (conv, msgs, atts, reactions, chans int) {
	return f.count(`SELECT COUNT(*) FROM conversations WHERE id = ?`, convID),
		f.count(`SELECT COUNT(*) FROM messages WHERE conversation_id = ?`, convID),
		f.count(`SELECT COUNT(*) FROM attachments WHERE id = ?`, "att-"+convID),
		f.count(`SELECT COUNT(*) FROM message_reactions WHERE message_id = ?`, "msg-"+convID),
		f.count(`SELECT COUNT(*) FROM contact_channels WHERE id = ?`, "ch-"+convID)
}

func (f *reconcileFixture) requireIntact(convID string) {
	f.t.Helper()
	if a, b, c, d, e := f.present(convID); a != 1 || b != 1 || c != 1 || d != 1 || e != 1 {
		f.t.Fatalf("%s debería estar intacta, obtuve conv=%d msgs=%d atts=%d reactions=%d chan=%d", convID, a, b, c, d, e)
	}
}

func (f *reconcileFixture) requireGone(convID string) {
	f.t.Helper()
	if a, b, c, d, e := f.present(convID); a+b+c+d+e != 0 {
		f.t.Fatalf("%s debería haberse borrado, obtuve conv=%d msgs=%d atts=%d reactions=%d chan=%d", convID, a, b, c, d, e)
	}
}

func reconcileEvent(serverID, body string) *conduitv1.PluginEvent {
	return &conduitv1.PluginEvent{EventType: "channel.reconcile", ServerId: serverID, Body: body}
}

var telegramCfg = pluginConfig{Name: "telegram", Provider: domain.ProviderTelegram}

func drainDeleted(sub eventbus.Subscription) []eventbus.ConversationDeleted {
	var out []eventbus.ConversationDeleted
	for {
		select {
		case ev := <-sub:
			out = append(out, ev.Payload.(eventbus.ConversationDeleted))
		default:
			return out
		}
	}
}

func TestReconcileDeletesVanishedTopicWithDependents(t *testing.T) {
	f := newReconcileFixture(t)
	f.addConv("telegram:c1_topic_1", "telegram", "c1_topic_1", "c1", true)
	f.addConv("telegram:c1_topic_7", "telegram", "c1_topic_7", "c1", true)

	got := handleChannelReconcile(f.ctx, f.db, f.bus, telegramCfg, reconcileEvent("c1", "c1_topic_1"))

	if len(got) != 1 || got[0] != "telegram:c1_topic_7" {
		t.Fatalf("debería devolver solo el tema ausente, obtuve %v", got)
	}
	f.requireGone("telegram:c1_topic_7")
	if n := f.count(`SELECT COUNT(*) FROM messages_fts WHERE messages_fts MATCH 'hola'`); n != 1 {
		t.Fatalf("el índice FTS debería conservar solo el mensaje del tema vivo, obtuve %d", n)
	}
}

func TestReconcileKeepsTopicInLiveList(t *testing.T) {
	f := newReconcileFixture(t)
	f.addConv("telegram:c1_topic_1", "telegram", "c1_topic_1", "c1", true)
	f.addConv("telegram:c1_topic_7", "telegram", "c1_topic_7", "c1", true)

	got := handleChannelReconcile(f.ctx, f.db, f.bus, telegramCfg, reconcileEvent("c1", "c1_topic_1\nc1_topic_7"))

	if len(got) != 0 {
		t.Fatalf("no debería borrar nada, obtuve %v", got)
	}
	f.requireIntact("telegram:c1_topic_1")
	f.requireIntact("telegram:c1_topic_7")
}

func TestReconcileLeavesOtherServersDMsAndParentUntouched(t *testing.T) {
	f := newReconcileFixture(t)
	f.addConv("telegram:c1_topic_7", "telegram", "c1_topic_7", "c1", true)
	f.addConv("telegram:c2_topic_7", "telegram", "c2_topic_7", "c2", true) // otro servidor
	f.addConv("telegram:c1", "telegram", "c1", "c1", true)                 // grupo padre
	f.addConv("telegram:u99", "telegram", "u99", "", true)                 // DM / chat sin servidor

	got := handleChannelReconcile(f.ctx, f.db, f.bus, telegramCfg, reconcileEvent("c1", "c1_topic_1"))

	if len(got) != 1 || got[0] != "telegram:c1_topic_7" {
		t.Fatalf("solo debería borrarse c1_topic_7, obtuve %v", got)
	}
	f.requireGone("telegram:c1_topic_7")
	f.requireIntact("telegram:c2_topic_7")
	f.requireIntact("telegram:c1")
	f.requireIntact("telegram:u99")
}

func TestReconcileRefusesEmptyInvalidOrMismatchedList(t *testing.T) {
	cases := map[string]*conduitv1.PluginEvent{
		"lista vacía":           reconcileEvent("c1", ""),
		"solo espacios":         reconcileEvent("c1", " \n\n"),
		"server vacío":          reconcileEvent("", "c1_topic_1"),
		"id sin _topic_":        reconcileEvent("c1", "c1_topic_1\nc1"),
		"id de otro servidor":   reconcileEvent("c1", "c1_topic_1\nc2_topic_3"),
		"número no numérico":    reconcileEvent("c1", "c1_topic_1\nc1_topic_x"),
		"número vacío":          reconcileEvent("c1", "c1_topic_"),
		"basura":                reconcileEvent("c1", "c1_topic_1\nhola mundo"),
		"prefijo parcial":       reconcileEvent("c1", "c1_topic_1\nxc1_topic_2"),
		"servidor como prefijo": reconcileEvent("c1", "c1_topic_1\nc10_topic_2"),
	}
	for name, ev := range cases {
		t.Run(name, func(t *testing.T) {
			f := newReconcileFixture(t)
			sub := f.bus.Subscribe(eventbus.EventConversationDeleted)
			f.addConv("telegram:c1_topic_1", "telegram", "c1_topic_1", "c1", true)
			f.addConv("telegram:c1_topic_7", "telegram", "c1_topic_7", "c1", true)

			got := handleChannelReconcile(f.ctx, f.db, f.bus, telegramCfg, ev)

			if len(got) != 0 {
				t.Fatalf("no debería borrar nada, obtuve %v", got)
			}
			f.requireIntact("telegram:c1_topic_1")
			f.requireIntact("telegram:c1_topic_7")
			if evs := drainDeleted(sub); len(evs) != 0 {
				t.Fatalf("no debería publicar eventos, obtuve %v", evs)
			}
		})
	}
}

func TestReconcileLeavesOtherProviderUntouched(t *testing.T) {
	f := newReconcileFixture(t)
	f.addConv("telegram:c1_topic_7", "telegram", "c1_topic_7", "c1", true)
	// Otro plugin/proveedor con los mismos remote ids.
	f.addConv("discord:c1_topic_7", "discord", "c1_topic_7", "c1", true)

	got := handleChannelReconcile(f.ctx, f.db, f.bus, telegramCfg, reconcileEvent("c1", "c1_topic_1"))

	if len(got) != 1 || got[0] != "telegram:c1_topic_7" {
		t.Fatalf("solo debería borrarse el tema de telegram, obtuve %v", got)
	}
	f.requireGone("telegram:c1_topic_7")
	f.requireIntact("discord:c1_topic_7")
	if n := f.count(`SELECT COUNT(*) FROM contact_channels WHERE provider = 'discord' AND remote_id = 'c1_topic_7'`); n != 1 {
		t.Fatalf("el canal del otro proveedor debe seguir, obtuve %d", n)
	}
}

func TestReconcileKeepsChannelRowsWithContactOrOtherAccount(t *testing.T) {
	f := newReconcileFixture(t)
	f.addConv("telegram:c1_topic_7", "telegram", "c1_topic_7", "c1", false)
	// Fila con contacto asignado: no es un canal de tema huérfano y no se toca.
	if err := f.db.SaveContact(f.ctx, &domain.Contact{ID: "contact-1", DisplayName: "Ana"}); err != nil {
		t.Fatalf("SaveContact: %v", err)
	}
	if err := f.db.SaveChannel(f.ctx, &domain.ContactChannel{ID: "ch-owned", ContactID: "contact-1", Provider: domain.ProviderTelegram, AccountID: "acc-other", RemoteID: "c1_topic_7"}); err != nil {
		t.Fatalf("SaveChannel: %v", err)
	}

	handleChannelReconcile(f.ctx, f.db, f.bus, telegramCfg, reconcileEvent("c1", "c1_topic_1"))

	if n := f.count(`SELECT COUNT(*) FROM conversations WHERE id = 'telegram:c1_topic_7'`); n != 0 {
		t.Fatalf("la conversación debería borrarse")
	}
	if n := f.count(`SELECT COUNT(*) FROM contact_channels WHERE id = 'ch-owned'`); n != 1 {
		t.Fatalf("un canal con contacto no debe borrarse, obtuve %d", n)
	}
}

func TestReconcilePublishesOneEventPerDeletedConversation(t *testing.T) {
	f := newReconcileFixture(t)
	sub := f.bus.Subscribe(eventbus.EventConversationDeleted)
	f.addConv("telegram:c1_topic_1", "telegram", "c1_topic_1", "c1", true)
	f.addConv("telegram:c1_topic_7", "telegram", "c1_topic_7", "c1", true)
	f.addConv("telegram:c1_topic_9", "telegram", "c1_topic_9", "c1", true)

	handleChannelReconcile(f.ctx, f.db, f.bus, telegramCfg, reconcileEvent("c1", "c1_topic_1"))

	evs := drainDeleted(sub)
	if len(evs) != 2 {
		t.Fatalf("esperaba 2 eventos, obtuve %v", evs)
	}
	seen := map[string]bool{}
	for _, e := range evs {
		if e.ServerID != "c1" {
			t.Fatalf("server id inesperado: %+v", e)
		}
		seen[e.ConversationID] = true
	}
	if !seen["telegram:c1_topic_7"] || !seen["telegram:c1_topic_9"] {
		t.Fatalf("eventos incorrectos: %v", evs)
	}
}

func TestReconcileIsIdempotent(t *testing.T) {
	f := newReconcileFixture(t)
	sub := f.bus.Subscribe(eventbus.EventConversationDeleted)
	f.addConv("telegram:c1_topic_1", "telegram", "c1_topic_1", "c1", true)
	f.addConv("telegram:c1_topic_7", "telegram", "c1_topic_7", "c1", true)
	ev := reconcileEvent("c1", "c1_topic_1")

	if got := handleChannelReconcile(f.ctx, f.db, f.bus, telegramCfg, ev); len(got) != 1 {
		t.Fatalf("la primera pasada debería borrar 1, obtuve %v", got)
	}
	if got := handleChannelReconcile(f.ctx, f.db, f.bus, telegramCfg, ev); len(got) != 0 {
		t.Fatalf("la segunda pasada no debería borrar nada, obtuve %v", got)
	}
	f.requireGone("telegram:c1_topic_7")
	f.requireIntact("telegram:c1_topic_1")
	if evs := drainDeleted(sub); len(evs) != 1 {
		t.Fatalf("debería haber exactamente 1 evento en total, obtuve %v", evs)
	}
}

func TestReconcileNeverDeletesGeneralTopic(t *testing.T) {
	f := newReconcileFixture(t)
	f.addConv("telegram:c1_topic_1", "telegram", "c1_topic_1", "c1", true)
	f.addConv("telegram:c1_topic_7", "telegram", "c1_topic_7", "c1", true)

	handleChannelReconcile(f.ctx, f.db, f.bus, telegramCfg, reconcileEvent("c1", "c1_topic_7"))

	f.requireIntact("telegram:c1_topic_1")
	f.requireIntact("telegram:c1_topic_7")
}

// Dos instancias de plugin del mismo proveedor comparten una única fila contact_channels
// por (provider, remote_id). Reconciliar una no debe dejar a la otra con un canal colgante.
func TestReconcileKeepsSharedChannelRowUsedByOtherPluginInstance(t *testing.T) {
	f := newReconcileFixture(t)
	const sharedChan = "ch-shared"
	if err := f.db.SaveChannel(f.ctx, &domain.ContactChannel{ID: sharedChan, Provider: domain.ProviderTelegram, RemoteID: "c1_topic_7", Handle: "t7", IsPreferred: true}); err != nil {
		t.Fatalf("SaveChannel: %v", err)
	}
	for _, convID := range []string{"telegram:c1_topic_7", "telegram2:c1_topic_7"} {
		chanID := sharedChan
		if err := f.db.EnsureConversation(f.ctx, &domain.Conversation{
			ID: convID, IsGroup: true, Title: convID, LastMessageAt: time.Unix(1000, 0),
			ActiveChannelID: &chanID, ServerID: "c1", ParentChannelID: "c1", ChannelType: "forum",
		}); err != nil {
			t.Fatalf("EnsureConversation: %v", err)
		}
		if err := f.db.SaveMessage(f.ctx, &domain.Message{
			ID: "msg-" + convID, ConversationID: convID, ChannelID: sharedChan, RemoteMessageID: "r-" + convID,
			SenderType: domain.SenderContact, Body: "hola", ContentType: domain.ContentTypeText,
			Status: domain.StatusDelivered, SentAt: time.Unix(1000, 0),
		}); err != nil {
			t.Fatalf("SaveMessage: %v", err)
		}
	}

	got := handleChannelReconcile(f.ctx, f.db, f.bus, telegramCfg, reconcileEvent("c1", "c1_topic_1"))

	if len(got) != 1 || got[0] != "telegram:c1_topic_7" {
		t.Fatalf("solo debería borrarse la conversación de telegram, obtuve %v", got)
	}
	if n := f.count(`SELECT COUNT(*) FROM conversations WHERE id = 'telegram:c1_topic_7'`); n != 0 {
		t.Fatalf("la conversación reconciliada debería borrarse")
	}
	if n := f.count(`SELECT COUNT(*) FROM conversations WHERE id = 'telegram2:c1_topic_7'`); n != 1 {
		t.Fatalf("la conversación de la otra instancia debe seguir, obtuve %d", n)
	}
	if n := f.count(`SELECT COUNT(*) FROM messages WHERE conversation_id = 'telegram2:c1_topic_7'`); n != 1 {
		t.Fatalf("los mensajes de la otra instancia deben seguir, obtuve %d", n)
	}
	if n := f.count(`SELECT COUNT(*) FROM contact_channels WHERE id = ?`, sharedChan); n != 1 {
		t.Fatalf("la fila de canal compartida debe seguir, obtuve %d", n)
	}
	if n := f.count(`SELECT COUNT(*) FROM conversations c JOIN contact_channels ch ON ch.id = c.active_channel_id WHERE c.id = 'telegram2:c1_topic_7'`); n != 1 {
		t.Fatalf("active_channel_id de la otra instancia debe resolver, obtuve %d", n)
	}

	// Al reconciliar también la otra instancia, la fila ya no se usa y se borra.
	other := pluginConfig{Name: "telegram2", Provider: domain.ProviderTelegram}
	if got := handleChannelReconcile(f.ctx, f.db, f.bus, other, reconcileEvent("c1", "c1_topic_1")); len(got) != 1 {
		t.Fatalf("la segunda instancia debería borrar su conversación, obtuve %v", got)
	}
	if n := f.count(`SELECT COUNT(*) FROM contact_channels WHERE id = ?`, sharedChan); n != 0 {
		t.Fatalf("sin conversaciones que la usen, la fila de canal debería borrarse, obtuve %d", n)
	}
}
