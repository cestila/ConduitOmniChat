package rpc

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/cestila/ConduitOmniChat/core/pkg/domain"
	"github.com/cestila/ConduitOmniChat/core/pkg/eventbus"
	"github.com/cestila/ConduitOmniChat/core/pkg/identity"
	"github.com/cestila/ConduitOmniChat/core/pkg/pluginmgr"
	"github.com/cestila/ConduitOmniChat/core/pkg/storage"
	conduitv1 "github.com/cestila/ConduitOmniChat/sdk/conduitv1"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

type mockPluginReadServiceClient struct {
	conduitv1.PluginServiceClient
	mu              sync.Mutex
	lastMarkReadReq *conduitv1.PluginMarkReadRequest
}

func (m *mockPluginReadServiceClient) MarkRead(ctx context.Context, req *conduitv1.PluginMarkReadRequest, opts ...grpc.CallOption) (*conduitv1.PluginMarkReadResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastMarkReadReq = req
	return &conduitv1.PluginMarkReadResponse{Success: true}, nil
}

// lastRequest lee la última request bajo lock: el plugin se invoca desde una goroutine.
func (m *mockPluginReadServiceClient) lastRequest() *conduitv1.PluginMarkReadRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastMarkReadReq
}

func TestMarkConversationReadRPC(t *testing.T) {
	db, err := storage.NewDB(":memory:")
	require.NoError(t, err)
	defer db.Close()

	ctx := context.Background()
	bus := eventbus.NewBus()
	defer bus.Close()

	chanID := uuid.New().String()
	require.NoError(t, db.SaveChannel(ctx, &domain.ContactChannel{
		ID:          chanID,
		Provider:    domain.ProviderWhatsApp,
		RemoteID:    "123456789@s.whatsapp.net",
		Handle:      "Test WA",
		IsPreferred: true,
	}))

	convID := "conv-read-test"
	require.NoError(t, db.EnsureConversation(ctx, &domain.Conversation{
		ID:              convID,
		Title:           "Test WA",
		ActiveChannelID: &chanID,
	}))

	// Mensaje no leído
	require.NoError(t, db.SaveMessage(ctx, &domain.Message{
		ID:              uuid.New().String(),
		ConversationID:  convID,
		ChannelID:       chanID,
		RemoteMessageID: "msg-wa-100",
		SenderType:      domain.SenderContact,
		Body:            "Unread text",
		Status:          domain.StatusDelivered,
		SentAt:          time.Now(),
	}))

	mgr := pluginmgr.NewManager()
	mockPlugin := &mockPluginReadServiceClient{}
	mgr.RegisterInstance("whatsapp", &pluginmgr.Instance{
		Name: "whatsapp",
		RPC:  mockPlugin,
	})

	graph := identity.NewGraph(db)
	server := NewServer(db, bus, graph, mgr)

	res, err := server.MarkConversationRead(ctx, &conduitv1.MarkConversationReadRequest{
		ConversationId: convID,
	})
	require.NoError(t, err)
	assert.True(t, res.Success)
	assert.Equal(t, int32(1), res.MarkedCount)

	// Esperar a que la goroutine notifique al plugin
	assert.Eventually(t, func() bool {
		return mockPlugin.lastRequest() != nil
	}, 1*time.Second, 10*time.Millisecond)

	// Verificar que mockPlugin recibió el request
	req := mockPlugin.lastRequest()
	require.NotNil(t, req)
	assert.Equal(t, "123456789@s.whatsapp.net", req.RemoteChatId)
	assert.Contains(t, req.RemoteMessageIds, "msg-wa-100")

	// Verificar en SQLite que ya quedó marcado como 'read'
	unreads, err := db.GetUnreadRemoteMessageIDs(ctx, convID)
	require.NoError(t, err)
	assert.Empty(t, unreads)
}
