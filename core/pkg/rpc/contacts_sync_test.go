package rpc

import (
	"context"
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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type mockSyncPluginClient struct {
	conduitv1.PluginServiceClient
	resp *conduitv1.PluginSyncContactsResponse
	err  error
}

func (m *mockSyncPluginClient) SyncContacts(ctx context.Context, in *conduitv1.SyncContactsRequest, opts ...grpc.CallOption) (*conduitv1.PluginSyncContactsResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.resp, nil
}

func newContactsTestServer(t *testing.T) (*Server, *storage.DB, *eventbus.Bus, *pluginmgr.Manager) {
	t.Helper()
	db, err := storage.NewDB(filepath.Join(t.TempDir(), "test_contacts.db"))
	if err != nil {
		t.Fatalf("failed to create test db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	bus := eventbus.NewBus()
	t.Cleanup(bus.Close)

	graph := identity.NewGraph(db)
	mgr := pluginmgr.NewManager()
	srv := NewServer(db, bus, graph, mgr)
	return srv, db, bus, mgr
}

func TestSyncContactsSkipsUnimplementedPlugin(t *testing.T) {
	srv, _, _, mgr := newContactsTestServer(t)

	mgr.RegisterInstance("whatsapp", &pluginmgr.Instance{
		Name: "whatsapp",
		RPC: &mockSyncPluginClient{
			resp: &conduitv1.PluginSyncContactsResponse{
				Contacts: []*conduitv1.PluginContact{
					{
						AccountId:   "acc-wa-1",
						RemoteId:    "5491112345678@s.whatsapp.net",
						DisplayName: "Alice",
						Phone:       "+5491112345678",
					},
					{
						AccountId:   "acc-wa-1",
						RemoteId:    "5491187654321@s.whatsapp.net",
						DisplayName: "Bob",
						Phone:       "+5491187654321",
					},
				},
			},
		},
	})
	mgr.RegisterInstance("discord", &pluginmgr.Instance{
		Name: "discord",
		RPC: &mockSyncPluginClient{
			err: status.Error(codes.Unimplemented, "SyncContacts not implemented"),
		},
	})

	resp, err := srv.SyncContacts(context.Background(), &conduitv1.SyncContactsRequest{})
	if err != nil {
		t.Fatalf("expected nil error when skipping Unimplemented plugin, got: %v", err)
	}
	if resp.GetSyncedCount() != 2 {
		t.Fatalf("expected synced_count=2, got %d", resp.GetSyncedCount())
	}
}

func TestSyncContactsExcludesGroupsAndTopics(t *testing.T) {
	srv, db, _, mgr := newContactsTestServer(t)

	mgr.RegisterInstance("whatsapp", &pluginmgr.Instance{
		Name: "whatsapp",
		RPC: &mockSyncPluginClient{
			resp: &conduitv1.PluginSyncContactsResponse{
				Contacts: []*conduitv1.PluginContact{
					{
						AccountId:   "acc-wa-1",
						RemoteId:    "5491112345678@s.whatsapp.net",
						DisplayName: "Alice",
						Phone:       "+5491112345678",
					},
					{
						AccountId:   "acc-wa-1",
						RemoteId:    "120363000000000@g.us",
						DisplayName: "Family Group",
					},
					{
						AccountId:   "acc-wa-1",
						RemoteId:    "c123_topic_4",
						DisplayName: "Forum Topic",
					},
					{
						AccountId:   "acc-wa-1",
						RemoteId:    "status@broadcast",
						DisplayName: "Status Broadcast",
					},
				},
			},
		},
	})

	resp, err := srv.SyncContacts(context.Background(), &conduitv1.SyncContactsRequest{})
	if err != nil {
		t.Fatalf("SyncContacts failed: %v", err)
	}
	if resp.GetSyncedCount() != 1 {
		t.Fatalf("expected synced_count=1, got %d", resp.GetSyncedCount())
	}

	contacts, err := db.ListContacts(context.Background())
	if err != nil {
		t.Fatalf("ListContacts failed: %v", err)
	}
	if len(contacts) != 1 {
		t.Fatalf("expected exactly 1 contact in DB, got %d", len(contacts))
	}
	if contacts[0].DisplayName != "Alice" {
		t.Fatalf("expected contact Alice, got %q", contacts[0].DisplayName)
	}
}

func TestSyncContactsPublishesContactUpdatedEventWithAccountID(t *testing.T) {
	srv, _, bus, mgr := newContactsTestServer(t)

	sub := bus.Subscribe(eventbus.EventContactUpdated)
	defer bus.Unsubscribe(eventbus.EventContactUpdated, sub)

	mgr.RegisterInstance("telegram", &pluginmgr.Instance{
		Name: "telegram",
		RPC: &mockSyncPluginClient{
			resp: &conduitv1.PluginSyncContactsResponse{
				Contacts: []*conduitv1.PluginContact{
					{
						AccountId:   "acc-tg-99",
						RemoteId:    "tg-user-42",
						DisplayName: "Carol",
						Handle:      "@carol",
						Phone:       "+5491155550000",
					},
				},
			},
		},
	})

	_, err := srv.SyncContacts(context.Background(), &conduitv1.SyncContactsRequest{Provider: "telegram"})
	if err != nil {
		t.Fatalf("SyncContacts failed: %v", err)
	}

	select {
	case ev := <-sub:
		contact, ok := ev.Payload.(*domain.Contact)
		if !ok || contact == nil {
			t.Fatalf("expected *domain.Contact payload, got %T", ev.Payload)
		}
		proto := protoContactFromDomain(contact)
		if len(proto.GetChannels()) != 1 {
			t.Fatalf("expected 1 channel on contact, got %d", len(proto.GetChannels()))
		}
		if proto.GetChannels()[0].GetAccountId() != "acc-tg-99" {
			t.Fatalf("expected AccountId=acc-tg-99, got %q", proto.GetChannels()[0].GetAccountId())
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for EventContactUpdated")
	}
}

func TestStartChatRejectsForeignChannel(t *testing.T) {
	srv, db, _, _ := newContactsTestServer(t)
	ctx := context.Background()
	now := time.Now()

	_ = db.SaveContact(ctx, &domain.Contact{ID: "contact-a", DisplayName: "Alice", CreatedAt: now, UpdatedAt: now})
	_ = db.SaveChannel(ctx, &domain.ContactChannel{ID: "channel-a", ContactID: "contact-a", Provider: domain.ProviderWhatsApp, AccountID: "acc-1", RemoteID: "wa-a"})

	_ = db.SaveContact(ctx, &domain.Contact{ID: "contact-b", DisplayName: "Bob", CreatedAt: now, UpdatedAt: now})
	_ = db.SaveChannel(ctx, &domain.ContactChannel{ID: "channel-b", ContactID: "contact-b", Provider: domain.ProviderTelegram, AccountID: "acc-2", RemoteID: "tg-b"})

	_, err := srv.StartChat(ctx, &conduitv1.StartChatRequest{
		ContactId: "contact-a",
		ChannelId: "channel-b",
	})
	if err == nil {
		t.Fatal("expected InvalidArgument when starting chat with another contact's channel")
	}
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected codes.InvalidArgument, got %v", status.Code(err))
	}
}

func TestStartChatCreatesConversationWithSelectedChannel(t *testing.T) {
	srv, db, _, _ := newContactsTestServer(t)
	ctx := context.Background()
	now := time.Now()

	_ = db.SaveContact(ctx, &domain.Contact{ID: "contact-a", DisplayName: "Alice", CreatedAt: now, UpdatedAt: now})
	_ = db.SaveChannel(ctx, &domain.ContactChannel{ID: "channel-a", ContactID: "contact-a", Provider: domain.ProviderWhatsApp, AccountID: "acc-1", RemoteID: "wa-a"})

	resp, err := srv.StartChat(ctx, &conduitv1.StartChatRequest{
		ContactId: "contact-a",
		ChannelId: "channel-a",
	})
	if err != nil {
		t.Fatalf("StartChat failed: %v", err)
	}
	conv := resp.GetConversation()
	if conv == nil {
		t.Fatal("expected non-nil conversation")
	}
	if conv.GetId() != "contact-a" {
		t.Fatalf("expected Conversation.Id=contact-a, got %q", conv.GetId())
	}
	if conv.GetActiveChannelId() != "channel-a" {
		t.Fatalf("expected ActiveChannelId=channel-a, got %q", conv.GetActiveChannelId())
	}
}
