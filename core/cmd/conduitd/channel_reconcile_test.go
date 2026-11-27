package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cestila/ConduitOmniChat/core/pkg/domain"
	"github.com/cestila/ConduitOmniChat/core/pkg/eventbus"
	"github.com/cestila/ConduitOmniChat/core/pkg/storage"
	conduitv1 "github.com/cestila/ConduitOmniChat/sdk/conduitv1"
)

type reconcileFixture struct {
	t     *testing.T
	db    *storage.DB
	bus   *eventbus.Bus
	ctx   context.Context
	media string // directorio de media de la prueba
}

func newReconcileFixture(t *testing.T) *reconcileFixture {
	t.Helper()
	db, err := storage.NewDB(filepath.Join(t.TempDir(), "reconcile.db"))
	if err != nil {
		t.Fatalf("error creando base: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return &reconcileFixture{t: t, db: db, bus: eventbus.NewBus(), ctx: context.Background(), media: t.TempDir()}
}

// addConv crea una conversación de canal/tema con su fila contact_channels (sin contacto),
// un mensaje, un adjunto y una reacción. convID y remoteID son independientes para poder
// simular otros plugins con los mismos remote ids.
func (f *reconcileFixture) addConv(convID, provider, remoteID, serverID string, withChannel bool) {
	f.t.Helper()
	f.addConvAt(convID, provider, remoteID, serverID, withChannel, time.Unix(1000, 0))
}

// addConvAt es addConv con la última actividad (conversación y mensaje) en `at`.
func (f *reconcileFixture) addConvAt(convID, provider, remoteID, serverID string, withChannel bool, at time.Time) {
	f.t.Helper()
	chanID := "ch-" + convID
	if withChannel {
		if err := f.db.SaveChannel(f.ctx, &domain.ContactChannel{ID: chanID, Provider: domain.Provider(provider), RemoteID: remoteID, Handle: convID, IsPreferred: true}); err != nil {
			f.t.Fatalf("SaveChannel: %v", err)
		}
	}
	if err := f.db.EnsureConversation(f.ctx, &domain.Conversation{
		ID: convID, IsGroup: true, Title: convID, LastMessageAt: at,
		ActiveChannelID: &chanID, ServerID: serverID, ParentChannelID: serverID, ChannelType: "forum",
	}); err != nil {
		f.t.Fatalf("EnsureConversation: %v", err)
	}
	msgID := "msg-" + convID
	if err := f.db.SaveMessage(f.ctx, &domain.Message{
		ID: msgID, ConversationID: convID, ChannelID: chanID, RemoteMessageID: "r-" + convID,
		SenderType: domain.SenderContact, Body: "hola " + convID, ContentType: domain.ContentTypeText,
		Status: domain.StatusDelivered, SentAt: at,
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

// snapshotAt es la hora (unix) a la que los tests dicen haber obtenido la lista viva de temas:
// mayor que la actividad de las conversaciones por defecto (1000).
const snapshotAt = 2000

func reconcileEvent(serverID, body string) *conduitv1.PluginEvent {
	return reconcileEventAt(serverID, body, snapshotAt)
}

func reconcileEventAt(serverID, body string, sentAtUnix int64) *conduitv1.PluginEvent {
	return &conduitv1.PluginEvent{EventType: "channel.reconcile", ServerId: serverID, Body: body, SentAtUnix: sentAtUnix}
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

	got := handleChannelReconcile(f.ctx, f.db, f.bus, telegramCfg, f.media, reconcileEvent("c1", "c1_topic_1"))

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

	got := handleChannelReconcile(f.ctx, f.db, f.bus, telegramCfg, f.media, reconcileEvent("c1", "c1_topic_1\nc1_topic_7"))

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

	got := handleChannelReconcile(f.ctx, f.db, f.bus, telegramCfg, f.media, reconcileEvent("c1", "c1_topic_1"))

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

			got := handleChannelReconcile(f.ctx, f.db, f.bus, telegramCfg, f.media, ev)

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

	got := handleChannelReconcile(f.ctx, f.db, f.bus, telegramCfg, f.media, reconcileEvent("c1", "c1_topic_1"))

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

	handleChannelReconcile(f.ctx, f.db, f.bus, telegramCfg, f.media, reconcileEvent("c1", "c1_topic_1"))

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

	handleChannelReconcile(f.ctx, f.db, f.bus, telegramCfg, f.media, reconcileEvent("c1", "c1_topic_1"))

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

	if got := handleChannelReconcile(f.ctx, f.db, f.bus, telegramCfg, f.media, ev); len(got) != 1 {
		t.Fatalf("la primera pasada debería borrar 1, obtuve %v", got)
	}
	if got := handleChannelReconcile(f.ctx, f.db, f.bus, telegramCfg, f.media, ev); len(got) != 0 {
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

	handleChannelReconcile(f.ctx, f.db, f.bus, telegramCfg, f.media, reconcileEvent("c1", "c1_topic_7"))

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

	got := handleChannelReconcile(f.ctx, f.db, f.bus, telegramCfg, f.media, reconcileEvent("c1", "c1_topic_1"))

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
	if got := handleChannelReconcile(f.ctx, f.db, f.bus, other, f.media, reconcileEvent("c1", "c1_topic_1")); len(got) != 1 {
		t.Fatalf("la segunda instancia debería borrar su conversación, obtuve %v", got)
	}
	if n := f.count(`SELECT COUNT(*) FROM contact_channels WHERE id = ?`, sharedChan); n != 0 {
		t.Fatalf("sin conversaciones que la usen, la fila de canal debería borrarse, obtuve %d", n)
	}
}

// --- T6: endurecimiento ---

func TestReconcileSkipsTopicsActiveAfterSnapshot(t *testing.T) {
	f := newReconcileFixture(t)
	sub := f.bus.Subscribe(eventbus.EventConversationDeleted)
	f.addConv("telegram:c1_topic_1", "telegram", "c1_topic_1", "c1", true)
	f.addConvAt("telegram:c1_topic_7", "telegram", "c1_topic_7", "c1", true, time.Unix(1000, 0))       // anterior: se borra
	f.addConvAt("telegram:c1_topic_8", "telegram", "c1_topic_8", "c1", true, time.Unix(1500, 0))       // exactamente en el snapshot: se borra
	f.addConvAt("telegram:c1_topic_9", "telegram", "c1_topic_9", "c1", true, time.Unix(2001, 0))       // posterior: sobrevive
	f.addConvAt("telegram:c1_topic_10", "telegram", "c1_topic_10", "c1", true, time.Unix(2000, 500e6)) // 0,5 s después: sobrevive

	got := handleChannelReconcile(f.ctx, f.db, f.bus, telegramCfg, f.media, reconcileEventAt("c1", "c1_topic_1", 1500))

	if len(got) != 2 {
		t.Fatalf("esperaba borrar topic_7 y topic_8, obtuve %v", got)
	}
	f.requireGone("telegram:c1_topic_7")
	f.requireGone("telegram:c1_topic_8")
	f.requireIntact("telegram:c1_topic_9")
	f.requireIntact("telegram:c1_topic_10")
	f.requireIntact("telegram:c1_topic_1")
	if evs := drainDeleted(sub); len(evs) != 2 {
		t.Fatalf("solo debería haber 2 eventos, obtuve %v", evs)
	}
}

func TestReconcileRefusesMissingSnapshotTime(t *testing.T) {
	for name, at := range map[string]int64{"cero": 0, "negativo": -5} {
		t.Run(name, func(t *testing.T) {
			f := newReconcileFixture(t)
			sub := f.bus.Subscribe(eventbus.EventConversationDeleted)
			f.addConv("telegram:c1_topic_1", "telegram", "c1_topic_1", "c1", true)
			f.addConv("telegram:c1_topic_7", "telegram", "c1_topic_7", "c1", true)

			got := handleChannelReconcile(f.ctx, f.db, f.bus, telegramCfg, f.media, reconcileEventAt("c1", "c1_topic_1", at))

			if len(got) != 0 {
				t.Fatalf("sin hora de snapshot no debería borrar nada, obtuve %v", got)
			}
			f.requireIntact("telegram:c1_topic_1")
			f.requireIntact("telegram:c1_topic_7")
			if evs := drainDeleted(sub); len(evs) != 0 {
				t.Fatalf("no debería publicar eventos, obtuve %v", evs)
			}
		})
	}
}

// El prefijo `<plugin>:` se compara en caracteres, no en bytes: un nombre con tildes debe coincidir.
func TestReconcileMatchesNonASCIIPluginName(t *testing.T) {
	f := newReconcileFixture(t)
	cfg := pluginConfig{Name: "telegrám", Provider: domain.ProviderTelegram}
	f.addConv("telegrám:c1_topic_1", "telegram", "c1_topic_1", "c1", true)
	f.addConv("telegrám:c1_topic_7", "telegram", "c1_topic_7", "c1", true)
	f.addConv("telegram:c1_topic_7", "discord", "c1_topic_7", "c1", true) // otro plugin (canal propio): no se toca

	got := handleChannelReconcile(f.ctx, f.db, f.bus, cfg, f.media, reconcileEvent("c1", "c1_topic_1"))

	if len(got) != 1 || got[0] != "telegrám:c1_topic_7" {
		t.Fatalf("debería borrar solo telegrám:c1_topic_7, obtuve %v", got)
	}
	f.requireGone("telegrám:c1_topic_7")
	f.requireIntact("telegrám:c1_topic_1")
	f.requireIntact("telegram:c1_topic_7")
}

// Despacho: el loop de eventos del plugin delega en routePluginEvent; el evento channel.reconcile
// debe llegar al handler (borra) y otros tipos no deben consumirse.
func TestRoutePluginEventDispatchesChannelReconcile(t *testing.T) {
	f := newReconcileFixture(t)
	sub := f.bus.Subscribe(eventbus.EventConversationDeleted)
	f.addConv("telegram:c1_topic_1", "telegram", "c1_topic_1", "c1", true)
	f.addConv("telegram:c1_topic_7", "telegram", "c1_topic_7", "c1", true)

	if !routePluginEvent(f.ctx, f.db, f.bus, telegramCfg, f.media, reconcileEvent("c1", "c1_topic_1")) {
		t.Fatalf("channel.reconcile debería consumirse")
	}
	f.requireGone("telegram:c1_topic_7")
	f.requireIntact("telegram:c1_topic_1")
	if evs := drainDeleted(sub); len(evs) != 1 || evs[0].ConversationID != "telegram:c1_topic_7" || evs[0].ServerID != "c1" {
		t.Fatalf("evento inesperado: %v", evs)
	}

	other := &conduitv1.PluginEvent{EventType: "message.received", ServerId: "c1", Body: "c1_topic_1"}
	if routePluginEvent(f.ctx, f.db, f.bus, telegramCfg, f.media, other) {
		t.Fatalf("otros tipos de evento no deben consumirse")
	}
}

// --- media en disco ---

func (f *reconcileFixture) setLocalPath(convID, path string) {
	f.t.Helper()
	p := path
	if err := f.db.UpdateAttachmentDownload(f.ctx, "att-"+convID, domain.MediaCached, &p, 1); err != nil {
		f.t.Fatalf("UpdateAttachmentDownload: %v", err)
	}
}

func (f *reconcileFixture) writeFile(path string) {
	f.t.Helper()
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		f.t.Fatalf("WriteFile: %v", err)
	}
}

func fileExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func TestReconcileRemovesMediaFilesOfDeletedAttachmentsOnlyInsideMediaDir(t *testing.T) {
	f := newReconcileFixture(t)
	outside := t.TempDir()
	f.addConv("telegram:c1_topic_1", "telegram", "c1_topic_1", "c1", true)
	f.addConv("telegram:c1_topic_7", "telegram", "c1_topic_7", "c1", true)
	f.addConv("telegram:c1_topic_8", "telegram", "c1_topic_8", "c1", true)
	f.addConv("telegram:c1_topic_9", "telegram", "c1_topic_9", "c1", true)
	f.addConv("telegram:c1_topic_11", "telegram", "c1_topic_11", "c1", true)

	inside := filepath.Join(f.media, "7_foto.png")
	outsideFile := filepath.Join(outside, "ajeno.png")
	traversal := filepath.Join(f.media, "..", filepath.Base(outside), "traversal.png")
	live := filepath.Join(f.media, "1_viva.png")
	for _, p := range []string{inside, outsideFile, filepath.Join(outside, "traversal.png"), live} {
		f.writeFile(p)
	}
	f.setLocalPath("telegram:c1_topic_7", inside)
	f.setLocalPath("telegram:c1_topic_8", outsideFile)
	f.setLocalPath("telegram:c1_topic_9", traversal)
	f.setLocalPath("telegram:c1_topic_1", live)
	f.setLocalPath("telegram:c1_topic_11", filepath.Join(f.media, "no_existe.png")) // archivo ya ausente

	got := handleChannelReconcile(f.ctx, f.db, f.bus, telegramCfg, f.media, reconcileEvent("c1", "c1_topic_1"))

	if len(got) != 4 {
		t.Fatalf("esperaba 4 temas borrados (7, 8, 9, 11), obtuve %v", got)
	}
	if fileExists(inside) {
		t.Fatalf("el archivo dentro del directorio de media debería borrarse")
	}
	if !fileExists(outsideFile) {
		t.Fatalf("un archivo fuera del directorio de media nunca debe borrarse")
	}
	if !fileExists(filepath.Join(outside, "traversal.png")) {
		t.Fatalf("una ruta con .. que sale del directorio de media nunca debe borrarse")
	}
	if !fileExists(live) {
		t.Fatalf("el archivo de un tema vivo no debe borrarse")
	}
}

func TestReconcileKeepsMediaFileStillReferencedByOtherAttachment(t *testing.T) {
	f := newReconcileFixture(t)
	f.addConv("telegram:c1_topic_1", "telegram", "c1_topic_1", "c1", true)
	f.addConv("telegram:c1_topic_7", "telegram", "c1_topic_7", "c1", true)
	shared := filepath.Join(f.media, "5_compartido.png")
	f.writeFile(shared)
	f.setLocalPath("telegram:c1_topic_7", shared)
	f.setLocalPath("telegram:c1_topic_1", shared) // el tema vivo usa el mismo archivo

	handleChannelReconcile(f.ctx, f.db, f.bus, telegramCfg, f.media, reconcileEvent("c1", "c1_topic_1"))

	f.requireGone("telegram:c1_topic_7")
	if !fileExists(shared) {
		t.Fatalf("un archivo aún referenciado por otro adjunto no debe borrarse")
	}
}

func TestReconcileKeepsMediaFilesWhenNothingIsDeleted(t *testing.T) {
	f := newReconcileFixture(t)
	f.addConv("telegram:c1_topic_1", "telegram", "c1_topic_1", "c1", true)
	f.addConvAt("telegram:c1_topic_7", "telegram", "c1_topic_7", "c1", true, time.Unix(3000, 0)) // posterior al snapshot
	file := filepath.Join(f.media, "7_foto.png")
	f.writeFile(file)
	f.setLocalPath("telegram:c1_topic_7", file)

	handleChannelReconcile(f.ctx, f.db, f.bus, telegramCfg, f.media, reconcileEvent("c1", "c1_topic_1"))

	f.requireIntact("telegram:c1_topic_7")
	if !fileExists(file) {
		t.Fatalf("el archivo de un tema que sobrevive no debe borrarse")
	}
}

func TestRemoveMediaFilesRefusesPathsThroughSymlinkedSubdir(t *testing.T) {
	base := t.TempDir()
	media := filepath.Join(base, "media")
	outside := filepath.Join(base, "outside")
	for _, d := range []string{media, outside} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	victim := filepath.Join(outside, "o.png")
	if err := os.WriteFile(victim, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(media, "linkdir")); err != nil {
		t.Fatal(err)
	}

	removeMediaFiles(media, []string{filepath.Join(media, "linkdir", "o.png")})

	if _, err := os.Lstat(victim); err != nil {
		t.Fatalf("a file outside mediaDir reached through a symlinked subdir must not be deleted")
	}
}

// Un hermano con el mismo prefijo textual (`media-evil`) no está dentro de `media`.
func TestRemoveMediaFilesRefusesSiblingWithSamePrefix(t *testing.T) {
	base := t.TempDir()
	media := filepath.Join(base, "media")
	evil := filepath.Join(base, "media-evil")
	for _, d := range []string{media, evil} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	victim := filepath.Join(evil, "f")
	if err := os.WriteFile(victim, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	removeMediaFiles(media, []string{victim})

	if _, err := os.Lstat(victim); err != nil {
		t.Fatalf("media-evil/f no debe borrarse al limpiar media")
	}
}

// Si el propio mediaDir es un enlace simbólico, las rutas léxicamente dentro de él siguen funcionando.
func TestRemoveMediaFilesWorksWhenMediaDirIsSymlink(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "real")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "media")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(real, "f.png")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	removeMediaFiles(link, []string{filepath.Join(link, "f.png")})

	if fileExists(file) {
		t.Fatalf("el archivo dentro de un mediaDir simbólico debería borrarse")
	}
}

// --- T4: reparación del pseudo tema con nombre de grupo (S3) ---

// addGroupNamedForumConv simula el pseudo tema que creaba el fallback antiguo del plugin: la
// conversación toma el id y el remote id del propio grupo (sin `_topic_`), con channel_type
// 'forum' y sin parent_channel_id.
func (f *reconcileFixture) addGroupNamedForumConv(pluginName, provider, serverID, title string) {
	f.addGroupNamedForumConvAt(pluginName, provider, serverID, title, "forum", time.Unix(1000, 0))
}

func (f *reconcileFixture) addGroupNamedForumConvAt(pluginName, provider, serverID, title, channelType string, at time.Time) {
	f.t.Helper()
	convID := pluginName + ":" + serverID
	chanID := "ch-" + convID
	if err := f.db.SaveChannel(f.ctx, &domain.ContactChannel{ID: chanID, Provider: domain.Provider(provider), RemoteID: serverID, Handle: title, IsPreferred: true}); err != nil {
		f.t.Fatalf("SaveChannel: %v", err)
	}
	if err := f.db.EnsureConversation(f.ctx, &domain.Conversation{
		ID: convID, IsGroup: true, Title: title, LastMessageAt: at,
		ActiveChannelID: &chanID, ServerID: serverID, ParentChannelID: "", ChannelType: channelType,
	}); err != nil {
		f.t.Fatalf("EnsureConversation: %v", err)
	}
	msgID := "msg-" + convID
	if err := f.db.SaveMessage(f.ctx, &domain.Message{
		ID: msgID, ConversationID: convID, ChannelID: chanID, RemoteMessageID: "r-" + convID,
		SenderType: domain.SenderContact, Body: "hola " + convID, ContentType: domain.ContentTypeText,
		Status: domain.StatusDelivered, SentAt: at,
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

type convFields struct {
	title, serverID, parent, channelType, contactID, activeChannel string
	isGroup                                                        int
	lastMessageUnix                                                int64
}

// conv devuelve las columnas relevantes de una conversación, o falla si no existe.
func (f *reconcileFixture) conv(convID string) convFields {
	f.t.Helper()
	var c convFields
	var last time.Time
	if err := f.db.QueryRowContext(f.ctx, `
		SELECT title, coalesce(server_id, ''), coalesce(parent_channel_id, ''), coalesce(channel_type, ''),
		       coalesce(contact_id, ''), coalesce(active_channel_id, ''), is_group, last_message_at
		FROM conversations WHERE id = ?`, convID).
		Scan(&c.title, &c.serverID, &c.parent, &c.channelType, &c.contactID, &c.activeChannel, &c.isGroup, &last); err != nil {
		f.t.Fatalf("no se pudo leer la conversación %s: %v", convID, err)
	}
	c.lastMessageUnix = last.Unix()
	return c
}

func TestReconcileRehomesGroupNamedForumConversationIntoGeneral(t *testing.T) {
	f := newReconcileFixture(t)
	sub := f.bus.Subscribe(eventbus.EventConversationDeleted)
	f.addGroupNamedForumConv("telegram", "telegram", "c1", "Grupo Familia")

	got := handleChannelReconcile(f.ctx, f.db, f.bus, telegramCfg, f.media, reconcileEvent("c1", "c1_topic_1"))

	if len(got) != 1 || got[0] != "telegram:c1" {
		t.Fatalf("esperaba que se informe la conversación reubicada, obtuve %v", got)
	}
	if n := f.count(`SELECT COUNT(*) FROM conversations WHERE id = 'telegram:c1'`); n != 0 {
		t.Fatalf("la conversación con nombre de grupo debería desaparecer, obtuve %d", n)
	}
	if n := f.count(`SELECT COUNT(*) FROM contact_channels WHERE provider = 'telegram' AND remote_id = 'c1'`); n != 0 {
		t.Fatalf("la fila de canal del grupo, ya sin referencias, debería borrarse, obtuve %d", n)
	}
	g := f.conv("telegram:c1_topic_1")
	if g.title != "General" || g.parent != "c1" || g.channelType != "forum" || g.serverID != "c1" || g.isGroup != 1 || g.contactID != "" {
		t.Fatalf("General mal creado: %+v", g)
	}
	if n := f.count(`SELECT COUNT(*) FROM contact_channels WHERE id = ? AND remote_id = 'c1_topic_1'`, g.activeChannel); n != 1 {
		t.Fatalf("General debe apuntar al canal del tema 1, obtuve %q", g.activeChannel)
	}
	if n := f.count(`SELECT COUNT(*) FROM messages WHERE conversation_id = 'telegram:c1_topic_1'`); n != 1 {
		t.Fatalf("el mensaje del grupo debería estar en General, obtuve %d", n)
	}
	if n := f.count(`SELECT COUNT(*) FROM messages WHERE conversation_id = 'telegram:c1'`); n != 0 {
		t.Fatalf("no debería quedar ningún mensaje en la conversación vieja, obtuve %d", n)
	}
	if n := f.count(`SELECT COUNT(*) FROM messages m JOIN contact_channels ch ON ch.id = m.channel_id WHERE m.conversation_id = 'telegram:c1_topic_1' AND ch.remote_id = 'c1_topic_1'`); n != 1 {
		t.Fatalf("los mensajes movidos deben usar el canal del tema General, obtuve %d", n)
	}
	if evs := drainDeleted(sub); len(evs) != 1 || evs[0].ConversationID != "telegram:c1" || evs[0].ServerID != "c1" {
		t.Fatalf("debería avisarse el borrado de la conversación vieja, obtuve %v", evs)
	}
}

func TestReconcileAppendsToExistingGeneralAndKeepsNewestActivity(t *testing.T) {
	f := newReconcileFixture(t)
	f.addConvAt("telegram:c1_topic_1", "telegram", "c1_topic_1", "c1", true, time.Unix(1200, 0))
	f.addGroupNamedForumConvAt("telegram", "telegram", "c1", "Grupo Familia", "forum", time.Unix(1500, 0))

	handleChannelReconcile(f.ctx, f.db, f.bus, telegramCfg, f.media, reconcileEvent("c1", "c1_topic_1"))

	if n := f.count(`SELECT COUNT(*) FROM messages WHERE conversation_id = 'telegram:c1_topic_1'`); n != 2 {
		t.Fatalf("los mensajes deberían sumarse a General, obtuve %d", n)
	}
	if n := f.count(`SELECT COUNT(*) FROM conversations WHERE id = 'telegram:c1'`); n != 0 {
		t.Fatalf("la conversación con nombre de grupo debería desaparecer")
	}
	if g := f.conv("telegram:c1_topic_1"); g.lastMessageUnix != 1500 {
		t.Fatalf("last_message_at debería ser la actividad más nueva (1500), obtuve %d", g.lastMessageUnix)
	}
}

func TestReconcileLeavesNonForumGroupConversationUntouched(t *testing.T) {
	f := newReconcileFixture(t)
	f.addGroupNamedForumConvAt("telegram", "telegram", "c1", "Grupo Familia", "group", time.Unix(1000, 0))

	got := handleChannelReconcile(f.ctx, f.db, f.bus, telegramCfg, f.media, reconcileEvent("c1", "c1_topic_1"))

	if len(got) != 0 {
		t.Fatalf("no debería reubicar ni borrar nada, obtuve %v", got)
	}
	f.requireIntact("telegram:c1")
	if n := f.count(`SELECT COUNT(*) FROM conversations WHERE id = 'telegram:c1_topic_1'`); n != 0 {
		t.Fatalf("no debería crearse General para un grupo real, obtuve %d", n)
	}
}

func TestReconcileLeavesOtherServerAndProviderBareConversationsUntouched(t *testing.T) {
	f := newReconcileFixture(t)
	f.addGroupNamedForumConv("telegram", "telegram", "c2", "Otro grupo")  // otro servidor
	f.addGroupNamedForumConv("discord", "discord", "c1", "Grupo Discord") // otro plugin/proveedor

	got := handleChannelReconcile(f.ctx, f.db, f.bus, telegramCfg, f.media, reconcileEvent("c1", "c1_topic_1"))

	if len(got) != 0 {
		t.Fatalf("no debería reubicar ni borrar nada, obtuve %v", got)
	}
	f.requireIntact("telegram:c2")
	f.requireIntact("discord:c1")
	if n := f.count(`SELECT COUNT(*) FROM conversations WHERE id IN ('telegram:c1_topic_1', 'discord:c1_topic_1')`); n != 0 {
		t.Fatalf("no debería crearse General para servidores ajenos, obtuve %d", n)
	}
}

func TestReconcileRefusedDoesNotRehomeGroupNamedForumConversation(t *testing.T) {
	cases := map[string]*conduitv1.PluginEvent{
		"lista vacía":    reconcileEvent("c1", ""),
		"lista inválida": reconcileEvent("c1", "c1_topic_1\nc1"),
		"server vacío":   reconcileEvent("", "c1_topic_1"),
		"sin snapshot":   reconcileEventAt("c1", "c1_topic_1", 0),
	}
	for name, ev := range cases {
		t.Run(name, func(t *testing.T) {
			f := newReconcileFixture(t)
			sub := f.bus.Subscribe(eventbus.EventConversationDeleted)
			f.addGroupNamedForumConv("telegram", "telegram", "c1", "Grupo Familia")

			got := handleChannelReconcile(f.ctx, f.db, f.bus, telegramCfg, f.media, ev)

			if len(got) != 0 {
				t.Fatalf("un reconcile rechazado no debe reubicar ni borrar, obtuve %v", got)
			}
			f.requireIntact("telegram:c1")
			if n := f.count(`SELECT COUNT(*) FROM messages WHERE conversation_id = 'telegram:c1'`); n != 1 {
				t.Fatalf("los mensajes deben seguir en la conversación vieja, obtuve %d", n)
			}
			if n := f.count(`SELECT COUNT(*) FROM conversations WHERE id = 'telegram:c1_topic_1'`); n != 0 {
				t.Fatalf("no debería crearse General, obtuve %d", n)
			}
			if evs := drainDeleted(sub); len(evs) != 0 {
				t.Fatalf("no debería publicar eventos, obtuve %v", evs)
			}
		})
	}
}

func TestReconcileRehomeKeepsAttachmentsAndReactions(t *testing.T) {
	f := newReconcileFixture(t)
	f.addGroupNamedForumConv("telegram", "telegram", "c1", "Grupo Familia")

	handleChannelReconcile(f.ctx, f.db, f.bus, telegramCfg, f.media, reconcileEvent("c1", "c1_topic_1"))

	if n := f.count(`SELECT COUNT(*) FROM attachments a JOIN messages m ON m.id = a.message_id WHERE a.id = 'att-telegram:c1' AND m.conversation_id = 'telegram:c1_topic_1'`); n != 1 {
		t.Fatalf("el adjunto debe seguir colgado del mensaje movido, obtuve %d", n)
	}
	if n := f.count(`SELECT COUNT(*) FROM message_reactions r JOIN messages m ON m.id = r.message_id WHERE r.message_id = 'msg-telegram:c1' AND m.conversation_id = 'telegram:c1_topic_1'`); n != 1 {
		t.Fatalf("la reacción debe seguir colgada del mensaje movido, obtuve %d", n)
	}
	if n := f.count(`SELECT COUNT(*) FROM messages WHERE conversation_id = 'telegram:c1_topic_1'`); n != 1 {
		t.Fatalf("no debería perderse ningún mensaje, obtuve %d", n)
	}
}

// Si last_message_at de la conversación vieja no es legible, General no puede
// quedar con NULL: la lectura de la conversación fallaría.
func TestReconcileRehomeUnreadableLastMessageAtKeepsGeneralReadable(t *testing.T) {
	f := newReconcileFixture(t)
	f.addGroupNamedForumConv("telegram", "telegram", "c1", "Grupo Familia")
	var touched int
	if err := f.db.QueryRowContext(f.ctx, `UPDATE conversations SET last_message_at = 'garbage' WHERE id = 'telegram:c1' RETURNING 1`).Scan(&touched); err != nil {
		t.Fatalf("no se pudo corromper last_message_at: %v", err)
	}

	handleChannelReconcile(f.ctx, f.db, f.bus, telegramCfg, f.media, reconcileEvent("c1", "c1_topic_1"))

	if _, err := f.db.GetConversationByID(f.ctx, "telegram:c1_topic_1"); err != nil {
		t.Fatalf("General debe poder leerse, obtuve error: %v", err)
	}
	if n := f.count(`SELECT COUNT(*) FROM conversations WHERE id = 'telegram:c1_topic_1' AND last_message_at IS NOT NULL`); n != 1 {
		t.Fatalf("General debe tener last_message_at, obtuve %d", n)
	}
}
