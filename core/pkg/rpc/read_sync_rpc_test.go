package rpc

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/cestila/ConduitOmniChat/core/pkg/domain"
	"github.com/cestila/ConduitOmniChat/core/pkg/eventbus"
	"github.com/cestila/ConduitOmniChat/core/pkg/identity"
	"github.com/cestila/ConduitOmniChat/core/pkg/pluginmgr"
	"github.com/cestila/ConduitOmniChat/core/pkg/storage"
	conduitv1 "github.com/cestila/ConduitOmniChat/sdk/conduitv1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// syncPlugin es un plugin falso seguro para uso concurrente que registra cada MarkRead.
type syncPlugin struct {
	conduitv1.PluginServiceClient
	mu   sync.Mutex
	reqs []*conduitv1.PluginMarkReadRequest
	err  error
	ok   bool
}

func (p *syncPlugin) MarkRead(_ context.Context, req *conduitv1.PluginMarkReadRequest, _ ...grpc.CallOption) (*conduitv1.PluginMarkReadResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.reqs = append(p.reqs, req)
	if p.err != nil {
		return nil, p.err
	}
	return &conduitv1.PluginMarkReadResponse{Success: p.ok}, nil
}

func (p *syncPlugin) setResult(ok bool, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ok, p.err = ok, err
}

func (p *syncPlugin) requests() []*conduitv1.PluginMarkReadRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]*conduitv1.PluginMarkReadRequest(nil), p.reqs...)
}

type syncEnv struct {
	db  *storage.DB
	mgr *pluginmgr.Manager
	srv *Server
}

func newSyncEnv(t *testing.T) *syncEnv {
	t.Helper()
	db, err := storage.NewDB(filepath.Join(t.TempDir(), "read-sync.db"))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	bus := eventbus.NewBus()
	t.Cleanup(bus.Close)
	mgr := pluginmgr.NewManager()
	return &syncEnv{db: db, mgr: mgr, srv: NewServer(db, bus, identity.NewGraph(db), mgr)}
}

func (e *syncEnv) channel(t *testing.T, id string, provider domain.Provider, remote string) {
	t.Helper()
	require.NoError(t, e.db.SaveChannel(context.Background(), &domain.ContactChannel{
		ID: id, Provider: provider, RemoteID: remote, Handle: remote, IsPreferred: true,
	}))
}

func (e *syncEnv) conversation(t *testing.T, id, channelID string) {
	t.Helper()
	require.NoError(t, e.db.EnsureConversation(context.Background(), &domain.Conversation{
		ID: id, Title: id, ActiveChannelID: &channelID,
	}))
}

var syncSeq int

func (e *syncEnv) incoming(t *testing.T, convID, channelID, remoteMsgID, sender string) {
	t.Helper()
	syncSeq++
	require.NoError(t, e.db.SaveMessage(context.Background(), &domain.Message{
		ID:              "m-" + remoteMsgID,
		ConversationID:  convID,
		ChannelID:       channelID,
		RemoteMessageID: remoteMsgID,
		SenderType:      domain.SenderContact,
		SenderRemoteID:  sender,
		Body:            "x",
		Status:          domain.StatusDelivered,
		SentAt:          time.Unix(1700000000+int64(syncSeq), 0),
	}))
}

func (e *syncEnv) register(name string, p *syncPlugin) {
	e.mgr.RegisterInstance(name, &pluginmgr.Instance{Name: name, RPC: p})
}

func (e *syncEnv) markLocal(t *testing.T, convID string) {
	t.Helper()
	_, err := e.db.MarkConversationReadForSync(context.Background(), convID)
	require.NoError(t, err)
}

func (e *syncEnv) pendingIDs(t *testing.T, convID string) []string {
	t.Helper()
	refs, err := e.db.PendingReadSync(context.Background(), convID)
	require.NoError(t, err)
	var ids []string
	for _, r := range refs {
		ids = append(ids, r.RemoteMessageID)
	}
	return ids
}

func TestFlushReadSyncGroupSendsParallelSenders(t *testing.T) {
	e := newSyncEnv(t)
	e.channel(t, "ch-g", domain.ProviderWhatsApp, "group@g.us")
	e.conversation(t, "conv-g", "ch-g")
	e.incoming(t, "conv-g", "ch-g", "g1", "alice@s.whatsapp.net")
	e.incoming(t, "conv-g", "ch-g", "g2", "bob@s.whatsapp.net")
	e.incoming(t, "conv-g", "ch-g", "g3", "alice@s.whatsapp.net")
	wa := &syncPlugin{ok: true}
	e.register("whatsapp", wa)
	e.markLocal(t, "conv-g")

	FlushReadSync(context.Background(), e.db, e.mgr, "conv-g")

	reqs := wa.requests()
	require.Len(t, reqs, 1)
	assert.Equal(t, "group@g.us", reqs[0].RemoteChatId)
	assert.Equal(t, []string{"g1", "g2", "g3"}, reqs[0].RemoteMessageIds)
	assert.Equal(t, []string{"alice@s.whatsapp.net", "bob@s.whatsapp.net", "alice@s.whatsapp.net"}, reqs[0].RemoteSenderIds)
	assert.Equal(t, "g3", reqs[0].MaxRemoteId)
	assert.Empty(t, reqs[0].SenderJid, "con remitentes distintos no hay remitente común")
	assert.Empty(t, e.pendingIDs(t, "conv-g"))
}

func TestFlushReadSyncCommonSenderSetsSenderJid(t *testing.T) {
	e := newSyncEnv(t)
	e.channel(t, "ch-d", domain.ProviderWhatsApp, "dm@s.whatsapp.net")
	e.conversation(t, "conv-d", "ch-d")
	e.incoming(t, "conv-d", "ch-d", "d1", "dm@s.whatsapp.net")
	e.incoming(t, "conv-d", "ch-d", "d2", "dm@s.whatsapp.net")
	wa := &syncPlugin{ok: true}
	e.register("whatsapp", wa)
	e.markLocal(t, "conv-d")

	FlushReadSync(context.Background(), e.db, e.mgr, "conv-d")

	reqs := wa.requests()
	require.Len(t, reqs, 1)
	assert.Equal(t, "dm@s.whatsapp.net", reqs[0].SenderJid)
	assert.Len(t, reqs[0].RemoteSenderIds, len(reqs[0].RemoteMessageIds))
}

func TestFlushReadSyncEmptySendersStayParallel(t *testing.T) {
	e := newSyncEnv(t)
	e.channel(t, "ch-e", domain.ProviderWhatsApp, "dm@s.whatsapp.net")
	e.conversation(t, "conv-e", "ch-e")
	e.incoming(t, "conv-e", "ch-e", "e1", "")
	e.incoming(t, "conv-e", "ch-e", "e2", "")
	wa := &syncPlugin{ok: true}
	e.register("whatsapp", wa)
	e.markLocal(t, "conv-e")

	FlushReadSync(context.Background(), e.db, e.mgr, "conv-e")

	reqs := wa.requests()
	require.Len(t, reqs, 1)
	assert.Equal(t, []string{"", ""}, reqs[0].RemoteSenderIds)
	assert.Empty(t, reqs[0].SenderJid)
}

func TestFlushReadSyncFailureKeepsPendingThenRetrySucceeds(t *testing.T) {
	e := newSyncEnv(t)
	e.channel(t, "ch-r", domain.ProviderWhatsApp, "chat@s.whatsapp.net")
	e.conversation(t, "conv-r", "ch-r")
	e.incoming(t, "conv-r", "ch-r", "r1", "chat@s.whatsapp.net")
	wa := &syncPlugin{err: errors.New("desconectado")}
	e.register("whatsapp", wa)
	e.markLocal(t, "conv-r")

	FlushReadSync(context.Background(), e.db, e.mgr, "conv-r")
	assert.Equal(t, []string{"r1"}, e.pendingIDs(t, "conv-r"))

	// success=false tampoco limpia
	wa.setResult(false, nil)
	FlushReadSync(context.Background(), e.db, e.mgr, "conv-r")
	assert.Equal(t, []string{"r1"}, e.pendingIDs(t, "conv-r"))

	// S2: segunda llamada por el RPC sin nuevos no leídos reenvía lo pendiente
	wa.setResult(true, nil)
	res, err := e.srv.MarkConversationRead(context.Background(), &conduitv1.MarkConversationReadRequest{ConversationId: "conv-r"})
	require.NoError(t, err)
	assert.True(t, res.Success)
	assert.Equal(t, int32(0), res.MarkedCount)
	assert.Eventually(t, func() bool { return len(e.pendingIDs(t, "conv-r")) == 0 }, time.Second, 10*time.Millisecond)

	reqs := wa.requests()
	last := reqs[len(reqs)-1]
	assert.Equal(t, []string{"r1"}, last.RemoteMessageIds)
}

func TestFlushReadSyncUnregisteredPluginKeepsPending(t *testing.T) {
	e := newSyncEnv(t)
	e.channel(t, "ch-u", domain.ProviderWhatsApp, "chat@s.whatsapp.net")
	e.conversation(t, "conv-u", "ch-u")
	e.incoming(t, "conv-u", "ch-u", "u1", "")
	e.markLocal(t, "conv-u")

	FlushReadSync(context.Background(), e.db, e.mgr, "conv-u")

	assert.Equal(t, []string{"u1"}, e.pendingIDs(t, "conv-u"))
}

func TestFlushReadSyncUnimplementedClearsPending(t *testing.T) {
	e := newSyncEnv(t)
	e.channel(t, "ch-i", domain.ProviderWhatsApp, "chat@s.whatsapp.net")
	e.conversation(t, "conv-i", "ch-i")
	e.incoming(t, "conv-i", "ch-i", "i1", "")
	wa := &syncPlugin{err: status.Error(codes.Unimplemented, "no soportado")}
	e.register("whatsapp", wa)
	e.markLocal(t, "conv-i")

	FlushReadSync(context.Background(), e.db, e.mgr, "conv-i")

	assert.Len(t, wa.requests(), 1)
	assert.Empty(t, e.pendingIDs(t, "conv-i"))
}

func TestFlushReadSyncUnifiedContactRoutesPerProvider(t *testing.T) {
	e := newSyncEnv(t)
	e.channel(t, "ch-wa", domain.ProviderWhatsApp, "111@s.whatsapp.net")
	e.channel(t, "ch-tg", domain.ProviderTelegram, "tg-222")
	e.conversation(t, "conv-u2", "ch-wa")
	e.incoming(t, "conv-u2", "ch-wa", "wa1", "111@s.whatsapp.net")
	e.incoming(t, "conv-u2", "ch-tg", "tg1", "tg-222")
	e.incoming(t, "conv-u2", "ch-wa", "wa2", "111@s.whatsapp.net")
	wa, tg := &syncPlugin{ok: true}, &syncPlugin{ok: true}
	e.register("whatsapp", wa)
	e.register("telegram", tg)
	e.markLocal(t, "conv-u2")

	FlushReadSync(context.Background(), e.db, e.mgr, "conv-u2")

	waReqs, tgReqs := wa.requests(), tg.requests()
	require.Len(t, waReqs, 1)
	require.Len(t, tgReqs, 1)
	assert.Equal(t, "111@s.whatsapp.net", waReqs[0].RemoteChatId)
	assert.Equal(t, []string{"wa1", "wa2"}, waReqs[0].RemoteMessageIds)
	assert.Equal(t, "tg-222", tgReqs[0].RemoteChatId)
	assert.Equal(t, []string{"tg1"}, tgReqs[0].RemoteMessageIds)
	assert.Empty(t, e.pendingIDs(t, "conv-u2"))
}

func TestFlushPendingReadSyncForProviderOnlyTouchesProvider(t *testing.T) {
	e := newSyncEnv(t)
	e.channel(t, "ch-wa", domain.ProviderWhatsApp, "111@s.whatsapp.net")
	e.channel(t, "ch-tg", domain.ProviderTelegram, "tg-222")
	e.conversation(t, "conv-w", "ch-wa")
	e.conversation(t, "conv-t", "ch-tg")
	e.incoming(t, "conv-w", "ch-wa", "w1", "")
	e.incoming(t, "conv-t", "ch-tg", "t1", "")
	wa, tg := &syncPlugin{ok: true}, &syncPlugin{ok: true}
	e.register("whatsapp", wa)
	e.register("telegram", tg)
	e.markLocal(t, "conv-w")
	e.markLocal(t, "conv-t")

	FlushPendingReadSyncForProvider(context.Background(), e.db, e.mgr, domain.ProviderWhatsApp)

	assert.Len(t, wa.requests(), 1)
	assert.Empty(t, tg.requests())
	assert.Empty(t, e.pendingIDs(t, "conv-w"))
	assert.Equal(t, []string{"t1"}, e.pendingIDs(t, "conv-t"))
}

func TestMarkConversationReadRPCFlushesAsync(t *testing.T) {
	e := newSyncEnv(t)
	e.channel(t, "ch-a", domain.ProviderWhatsApp, "chat@s.whatsapp.net")
	e.conversation(t, "conv-a", "ch-a")
	e.incoming(t, "conv-a", "ch-a", "a1", "chat@s.whatsapp.net")
	wa := &syncPlugin{ok: true}
	e.register("whatsapp", wa)

	res, err := e.srv.MarkConversationRead(context.Background(), &conduitv1.MarkConversationReadRequest{ConversationId: "conv-a"})
	require.NoError(t, err)
	assert.True(t, res.Success)
	assert.Equal(t, int32(1), res.MarkedCount)

	assert.Eventually(t, func() bool { return len(wa.requests()) == 1 }, time.Second, 10*time.Millisecond)
	assert.Eventually(t, func() bool { return len(e.pendingIDs(t, "conv-a")) == 0 }, time.Second, 10*time.Millisecond)
	unreads, err := e.db.GetUnreadRemoteMessageIDs(context.Background(), "conv-a")
	require.NoError(t, err)
	assert.Empty(t, unreads)

	_, err = e.srv.MarkConversationRead(context.Background(), &conduitv1.MarkConversationReadRequest{})
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}
