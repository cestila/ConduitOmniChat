package rpc

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
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

// mediaPlugin es un plugin falso que registra cada DownloadMedia.
type mediaPlugin struct {
	conduitv1.PluginServiceClient
	calls  atomic.Int32
	mu     sync.Mutex
	reqs   []*conduitv1.PluginDownloadMediaRequest
	gate   chan struct{} // si no es nil, DownloadMedia espera a que se cierre
	resp   *conduitv1.PluginDownloadMediaResponse
	rpcErr error
}

func (p *mediaPlugin) DownloadMedia(_ context.Context, req *conduitv1.PluginDownloadMediaRequest, _ ...grpc.CallOption) (*conduitv1.PluginDownloadMediaResponse, error) {
	p.calls.Add(1)
	p.mu.Lock()
	p.reqs = append(p.reqs, req)
	p.mu.Unlock()
	if p.gate != nil {
		<-p.gate
	}
	return p.resp, p.rpcErr
}

type dlEnv struct {
	db  *storage.DB
	bus *eventbus.Bus
	mgr *pluginmgr.Manager
	srv *Server
	upd eventbus.Subscription
}

func newDLEnv(t *testing.T) *dlEnv {
	t.Helper()
	db, err := storage.NewDB(filepath.Join(t.TempDir(), "dl.db"))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	bus := eventbus.NewBus()
	t.Cleanup(bus.Close)
	mgr := pluginmgr.NewManager()
	e := &dlEnv{db: db, bus: bus, mgr: mgr, srv: NewServer(db, bus, identity.NewGraph(db), mgr)}
	e.upd = bus.Subscribe(eventbus.EventMessageUpdated)
	ctx := context.Background()
	require.NoError(t, db.SaveChannel(ctx, &domain.ContactChannel{ID: "ch", Provider: domain.ProviderWhatsApp, RemoteID: "chat", Handle: "chat", IsPreferred: true}))
	require.NoError(t, db.EnsureConversation(ctx, &domain.Conversation{ID: "conv", Title: "c"}))
	require.NoError(t, db.SaveMessage(ctx, &domain.Message{
		ID: "msg", ConversationID: "conv", ChannelID: "ch", RemoteMessageID: "rm1",
		SenderType: domain.SenderContact, Body: "x", Status: domain.StatusDelivered, SentAt: time.Unix(1700000000, 0),
	}))
	return e
}

func (e *dlEnv) attachment(t *testing.T, id string, st domain.DownloadStatus, localPath *string, ref string) {
	t.Helper()
	require.NoError(t, e.db.SaveAttachment(context.Background(), &domain.Attachment{
		ID: id, MessageID: "msg", FileName: "photo.jpg", MimeType: "image/jpeg", FileSize: 10,
		LocalPath: localPath, DownloadStatus: st, RemoteRef: ref,
	}))
}

func (e *dlEnv) register(p *mediaPlugin) {
	e.mgr.RegisterInstance("whatsapp", &pluginmgr.Instance{Name: "whatsapp", RPC: p})
}

// nextUpdate espera el siguiente message_updated y devuelve el estado del adjunto a1.
func (e *dlEnv) nextUpdate(t *testing.T) domain.DownloadStatus {
	t.Helper()
	select {
	case ev := <-e.upd:
		msg, ok := ev.Payload.(*domain.Message)
		require.True(t, ok)
		assert.Equal(t, "msg", msg.ID)
		require.NotEmpty(t, msg.Attachments)
		return msg.Attachments[0].DownloadStatus
	case <-time.After(3 * time.Second):
		t.Fatal("no llegó message_updated")
		return ""
	}
}

func (e *dlEnv) noMoreUpdates(t *testing.T) {
	t.Helper()
	select {
	case <-e.upd:
		t.Fatal("message_updated inesperado")
	case <-time.After(100 * time.Millisecond):
	}
}

func (e *dlEnv) stored(t *testing.T, id string) *domain.Attachment {
	t.Helper()
	a, err := e.db.GetAttachmentByID(context.Background(), id)
	require.NoError(t, err)
	return a
}

func TestDownloadAttachmentSuccessWait(t *testing.T) {
	e := newDLEnv(t)
	e.attachment(t, "a1", domain.MediaRemoteOnly, nil, "REF")
	p := &mediaPlugin{resp: &conduitv1.PluginDownloadMediaResponse{Success: true, LocalPath: "/media/a1_photo.jpg", FileSize: 1234}}
	e.register(p)

	resp, err := e.srv.DownloadAttachment(context.Background(), &conduitv1.DownloadAttachmentRequest{AttachmentId: "a1", Wait: true})
	require.NoError(t, err)
	assert.Equal(t, "cached", resp.Attachment.DownloadStatus)
	assert.Equal(t, "/media/a1_photo.jpg", resp.Attachment.LocalPath)
	assert.Equal(t, int64(1234), resp.Attachment.FileSize)
	assert.Empty(t, resp.Attachment.RemoteRef, "la remote_ref nunca sale al cliente")

	assert.Equal(t, domain.MediaDownloading, e.nextUpdate(t))
	assert.Equal(t, domain.MediaCached, e.nextUpdate(t))
	e.noMoreUpdates(t)

	require.Len(t, p.reqs, 1)
	assert.Equal(t, "REF", p.reqs[0].RemoteRef)
	assert.Equal(t, "photo.jpg", p.reqs[0].FileName)
	assert.Equal(t, "image/jpeg", p.reqs[0].MimeType)
	assert.Equal(t, "a1", p.reqs[0].AttachmentId)

	got := e.stored(t, "a1")
	assert.Equal(t, domain.MediaCached, got.DownloadStatus)
	assert.Equal(t, "/media/a1_photo.jpg", *got.LocalPath)
	assert.Equal(t, int64(1234), got.FileSize)
}

func TestDownloadAttachmentNoWaitReturnsDownloading(t *testing.T) {
	e := newDLEnv(t)
	e.attachment(t, "a1", domain.MediaRemoteOnly, nil, "REF")
	p := &mediaPlugin{gate: make(chan struct{}), resp: &conduitv1.PluginDownloadMediaResponse{Success: true, LocalPath: "/m/x", FileSize: 5}}
	e.register(p)

	resp, err := e.srv.DownloadAttachment(context.Background(), &conduitv1.DownloadAttachmentRequest{AttachmentId: "a1"})
	require.NoError(t, err)
	assert.Equal(t, "downloading", resp.Attachment.DownloadStatus)
	assert.Equal(t, domain.MediaDownloading, e.nextUpdate(t))
	assert.Equal(t, domain.MediaDownloading, e.stored(t, "a1").DownloadStatus)

	close(p.gate)
	assert.Equal(t, domain.MediaCached, e.nextUpdate(t))
	assert.Equal(t, domain.MediaCached, e.stored(t, "a1").DownloadStatus)
}

func TestDownloadAttachmentPluginFailure(t *testing.T) {
	cases := map[string]*mediaPlugin{
		"success=false": {resp: &conduitv1.PluginDownloadMediaResponse{Success: false, ErrorMessage: "expiró"}},
		"error rpc":     {rpcErr: errors.New("caído")},
	}
	for name, p := range cases {
		t.Run(name, func(t *testing.T) {
			e := newDLEnv(t)
			e.attachment(t, "a1", domain.MediaRemoteOnly, nil, "REF")
			e.register(p)
			resp, err := e.srv.DownloadAttachment(context.Background(), &conduitv1.DownloadAttachmentRequest{AttachmentId: "a1", Wait: true})
			require.NoError(t, err)
			assert.Equal(t, "failed", resp.Attachment.DownloadStatus)
			assert.Empty(t, resp.Attachment.LocalPath)
			assert.Equal(t, domain.MediaDownloading, e.nextUpdate(t))
			assert.Equal(t, domain.MediaFailed, e.nextUpdate(t))
			got := e.stored(t, "a1")
			assert.Equal(t, domain.MediaFailed, got.DownloadStatus)
			assert.Nil(t, got.LocalPath)
		})
	}
}

func TestDownloadAttachmentPluginNotConnectedFails(t *testing.T) {
	e := newDLEnv(t)
	e.attachment(t, "a1", domain.MediaRemoteOnly, nil, "REF")
	resp, err := e.srv.DownloadAttachment(context.Background(), &conduitv1.DownloadAttachmentRequest{AttachmentId: "a1", Wait: true})
	require.NoError(t, err)
	assert.Equal(t, "failed", resp.Attachment.DownloadStatus)
	assert.Equal(t, domain.MediaFailed, e.stored(t, "a1").DownloadStatus)
	assert.Equal(t, domain.MediaDownloading, e.nextUpdate(t))
	assert.Equal(t, domain.MediaFailed, e.nextUpdate(t))
}

func TestDownloadAttachmentCachedExistingFileSkipsPlugin(t *testing.T) {
	e := newDLEnv(t)
	file := filepath.Join(t.TempDir(), "f.jpg")
	require.NoError(t, os.WriteFile(file, []byte("x"), 0o600))
	e.attachment(t, "a1", domain.MediaCached, &file, "REF")
	p := &mediaPlugin{resp: &conduitv1.PluginDownloadMediaResponse{Success: true, LocalPath: "/otro", FileSize: 1}}
	e.register(p)

	resp, err := e.srv.DownloadAttachment(context.Background(), &conduitv1.DownloadAttachmentRequest{AttachmentId: "a1", Wait: true})
	require.NoError(t, err)
	assert.Equal(t, "cached", resp.Attachment.DownloadStatus)
	assert.Equal(t, file, resp.Attachment.LocalPath)
	assert.Equal(t, int32(0), p.calls.Load())
	e.noMoreUpdates(t)
}

func TestDownloadAttachmentCachedMissingFileRedownloads(t *testing.T) {
	e := newDLEnv(t)
	gone := filepath.Join(t.TempDir(), "gone.jpg")
	e.attachment(t, "a1", domain.MediaCached, &gone, "REF")
	p := &mediaPlugin{resp: &conduitv1.PluginDownloadMediaResponse{Success: true, LocalPath: "/new/path", FileSize: 3}}
	e.register(p)

	resp, err := e.srv.DownloadAttachment(context.Background(), &conduitv1.DownloadAttachmentRequest{AttachmentId: "a1", Wait: true})
	require.NoError(t, err)
	assert.Equal(t, "/new/path", resp.Attachment.LocalPath)
	assert.Equal(t, int32(1), p.calls.Load())
}

func TestDownloadAttachmentConcurrentDedupe(t *testing.T) {
	e := newDLEnv(t)
	e.attachment(t, "a1", domain.MediaRemoteOnly, nil, "REF")
	p := &mediaPlugin{gate: make(chan struct{}), resp: &conduitv1.PluginDownloadMediaResponse{Success: true, LocalPath: "/m/x", FileSize: 5}}
	e.register(p)

	const n = 5
	var wg sync.WaitGroup
	results := make([]string, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			resp, err := e.srv.DownloadAttachment(context.Background(), &conduitv1.DownloadAttachmentRequest{AttachmentId: "a1", Wait: true})
			if assert.NoError(t, err) {
				results[i] = resp.Attachment.DownloadStatus
			}
		}(i)
	}
	// Dar tiempo a que todos los pedidos entren antes de liberar al plugin.
	require.Eventually(t, func() bool { return p.calls.Load() == 1 }, 3*time.Second, 5*time.Millisecond)
	time.Sleep(100 * time.Millisecond)
	close(p.gate)
	wg.Wait()

	assert.Equal(t, int32(1), p.calls.Load())
	for _, r := range results {
		assert.Equal(t, "cached", r)
	}
}

func TestDownloadAttachmentErrors(t *testing.T) {
	e := newDLEnv(t)
	e.attachment(t, "noref", domain.MediaRemoteOnly, nil, "")
	p := &mediaPlugin{resp: &conduitv1.PluginDownloadMediaResponse{Success: true}}
	e.register(p)

	_, err := e.srv.DownloadAttachment(context.Background(), &conduitv1.DownloadAttachmentRequest{})
	assert.Equal(t, codes.InvalidArgument, status.Code(err))

	_, err = e.srv.DownloadAttachment(context.Background(), &conduitv1.DownloadAttachmentRequest{AttachmentId: "nope"})
	assert.Equal(t, codes.NotFound, status.Code(err))

	_, err = e.srv.DownloadAttachment(context.Background(), &conduitv1.DownloadAttachmentRequest{AttachmentId: "noref"})
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))

	assert.Equal(t, int32(0), p.calls.Load())
	assert.Equal(t, domain.MediaRemoteOnly, e.stored(t, "noref").DownloadStatus)
	e.noMoreUpdates(t)
}

func TestDomainMsgToProtoHidesRemoteRef(t *testing.T) {
	m := domainMsgToProto(&domain.Message{ID: "m", Attachments: []domain.Attachment{{ID: "a", RemoteRef: "SECRET"}}})
	require.Len(t, m.Attachments, 1)
	assert.Empty(t, m.Attachments[0].RemoteRef)
}
