package rpc

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/cestila/ConduitOmniChat/core/pkg/eventbus"
	conduitv1 "github.com/cestila/ConduitOmniChat/sdk/conduitv1"
)

// StreamEvents debe reenviar EventConversationDeleted como un envelope "conversation.deleted"
// cuyo channel_status_update es el JSON {"conversation_id","server_id"}.
func TestStreamEventsForwardsConversationDeleted(t *testing.T) {
	_, _, bus, client := setupTestRPCServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := client.StreamEvents(ctx, &conduitv1.StreamEventsRequest{})
	if err != nil {
		t.Fatalf("StreamEvents: %v", err)
	}
	// Dar tiempo a StreamEvents para suscribirse antes de publicar.
	time.Sleep(50 * time.Millisecond)

	bus.Publish(eventbus.EventConversationDeleted, eventbus.ConversationDeleted{ConversationID: "telegram:c1_topic_7", ServerID: "c1"})

	for {
		env, err := stream.Recv()
		if err != nil {
			t.Fatalf("stream.Recv: %v", err)
		}
		if env.GetType() != "conversation.deleted" {
			continue
		}
		raw := env.GetChannelStatusUpdate()
		if raw == "" {
			t.Fatalf("conversation.deleted sin channel_status_update: %+v", env)
		}
		var fields map[string]any
		if err := json.Unmarshal([]byte(raw), &fields); err != nil {
			t.Fatalf("channel_status_update no es JSON: %q: %v", raw, err)
		}
		if fields["conversation_id"] != "telegram:c1_topic_7" || fields["server_id"] != "c1" || len(fields) != 2 {
			t.Fatalf("payload inesperado: %v", fields)
		}
		return
	}
}
