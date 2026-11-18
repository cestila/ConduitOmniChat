package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/cestila/ConduitOmniChat/core/pkg/storage"
	conduitv1 "github.com/cestila/ConduitOmniChat/sdk/conduitv1"
)

func TestIsTopicOrGroupTargetExcludesNonPersonTargets(t *testing.T) {
	cases := []struct {
		name        string
		remoteID    string
		body        string
		serverID    string
		channelType string
		want        bool
	}{
		{"whatsapp_group", "120363000@g.us", "", "", "", true},
		{"telegram_topic", "c123_topic_4", "", "", "", true},
		{"whatsapp_broadcast", "status@broadcast", "", "", "", true},
		{"whatsapp_newsletter", "12345@newsletter", "", "", "", true},
		{"channel_type_group", "some-id", "", "", "group", true},
		{"channel_type_forum", "some-id", "", "", "forum", true},
		{"server_id_present", "chan-1", "", "srv-1", "", true},
		{"person_dm", "5491112345678@s.whatsapp.net", "", "", "", false},
		{"person_telegram", "123456789", "", "", "dm", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isTopicOrGroupTarget(tc.remoteID, tc.body, tc.serverID, tc.channelType)
			if got != tc.want {
				t.Fatalf("isTopicOrGroupTarget(%q, %q, %q, %q) = %v, want %v",
					tc.remoteID, tc.body, tc.serverID, tc.channelType, got, tc.want)
			}
		})
	}
}

func TestResolveAccountIDForProvider(t *testing.T) {
	db, err := storage.NewDB(filepath.Join(t.TempDir(), "test_conduitd.db"))
	if err != nil {
		t.Fatalf("failed to create test db: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	_ = db.SaveAccount(ctx, &conduitv1.ProtoAccount{
		Id:       "acc-wa-disc",
		Provider: conduitv1.Provider_PROVIDER_WHATSAPP,
		Status:   "disconnected",
	})
	_ = db.SaveAccount(ctx, &conduitv1.ProtoAccount{
		Id:       "acc-wa-conn",
		Provider: conduitv1.Provider_PROVIDER_WHATSAPP,
		Status:   "connected",
	})

	if got := resolveAccountIDForProvider(ctx, db, conduitv1.Provider_PROVIDER_WHATSAPP); got != "acc-wa-conn" {
		t.Fatalf("expected connected account acc-wa-conn, got %q", got)
	}
	if got := resolveAccountIDForProvider(ctx, db, conduitv1.Provider_PROVIDER_TELEGRAM); got != "" {
		t.Fatalf("expected empty account for unconfigured provider, got %q", got)
	}
}
