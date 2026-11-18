package rpc

import (
	"context"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/cestila/ConduitOmniChat/core/pkg/domain"
	"github.com/cestila/ConduitOmniChat/core/pkg/eventbus"
	"github.com/cestila/ConduitOmniChat/core/pkg/identity"
	"github.com/cestila/ConduitOmniChat/core/pkg/pluginmgr"
	"github.com/cestila/ConduitOmniChat/core/pkg/storage"
	conduitv1 "github.com/cestila/ConduitOmniChat/sdk/conduitv1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

func setupTestRPCServer(t *testing.T) (*Server, *storage.DB, *eventbus.Bus, conduitv1.ConduitServiceClient) {
	t.Helper()
	db, err := storage.NewDB(filepath.Join(t.TempDir(), "rpc_test.db"))
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	bus := eventbus.NewBus()
	graph := identity.NewGraph(db)
	pm := pluginmgr.NewManager()
	srv := NewServer(db, bus, graph, pm)

	lis := bufconn.Listen(1024 * 1024)
	grpcSrv := grpc.NewServer()
	conduitv1.RegisterConduitServiceServer(grpcSrv, srv)
	go func() { _ = grpcSrv.Serve(lis) }()
	t.Cleanup(grpcSrv.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	return srv, db, bus, conduitv1.NewConduitServiceClient(conn)
}

func TestSendMessageMissingPluginMarksFailedAndStreamEventsForwardsStatuses(t *testing.T) {
	_, db, bus, client := setupTestRPCServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	now := time.Now()
	_ = db.SaveContact(ctx, &domain.Contact{ID: "c-1", DisplayName: "Ana", CreatedAt: now, UpdatedAt: now})
	_ = db.SaveChannel(ctx, &domain.ContactChannel{ID: "ch-1", ContactID: "c-1", Provider: domain.ProviderWhatsApp, RemoteID: "+549111"})
	cID, chID := "c-1", "ch-1"
	_ = db.EnsureConversation(ctx, &domain.Conversation{ID: "conv-1", ContactID: &cID, IsGroup: false, Title: "Ana", ActiveChannelID: &chID})

	stream, err := client.StreamEvents(ctx, &conduitv1.StreamEventsRequest{})
	if err != nil {
		t.Fatalf("StreamEvents: %v", err)
	}
	// Give StreamEvents goroutine a brief moment to subscribe before publishing.
	time.Sleep(50 * time.Millisecond)

	resp, err := client.SendMessage(ctx, &conduitv1.SendMessageRequest{
		ConversationId:  "conv-1",
		TargetChannelId: "ch-1",
		Body:            "hola mundo",
	})
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if resp.Message.Status != string(domain.StatusFailed) {
		t.Fatalf("expected failed status when plugin missing, got %q", resp.Message.Status)
	}

	bus.Publish(eventbus.EventChannelConnected, "whatsapp:disconnected:eof")

	gotStatus := false
	gotChannel := false
	for !(gotStatus && gotChannel) {
		ev, err := stream.Recv()
		if err != nil {
			t.Fatalf("stream.Recv: %v", err)
		}
		if st := ev.GetMessageStatusUpdated(); st != nil && st.MessageId == resp.Message.Id && st.Status == "failed" {
			gotStatus = true
		}
		if ch := ev.GetChannelStatusUpdate(); ch == "whatsapp:disconnected:eof" {
			gotChannel = true
		}
	}
}

func TestRenameContactRPC(t *testing.T) {
	_, db, _, client := setupTestRPCServer(t)
	ctx := context.Background()
	now := time.Now()
	_ = db.SaveContact(ctx, &domain.Contact{ID: "c-rename", DisplayName: ".", Phone: "+5492664000001", CreatedAt: now, UpdatedAt: now})

	resp, err := client.RenameContact(ctx, &conduitv1.RenameContactRequest{
		ContactId:   "c-rename",
		DisplayName: "  Maximiliano Collado  ",
	})
	if err != nil {
		t.Fatalf("RenameContact: %v", err)
	}
	if resp.Contact.DisplayName != "Maximiliano Collado" || !resp.Contact.NameLocked {
		t.Fatalf("unexpected contact response: %+v", resp.Contact)
	}
}
