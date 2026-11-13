package rpc

import (
	"context"
	"testing"
	"time"

	"github.com/cestila/ConduitOmniChat/core/pkg/domain"
	conduitv1 "github.com/cestila/ConduitOmniChat/sdk/conduitv1"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestSendReactionTogglesAndReturnsUpdated(t *testing.T) {
	srv, db := newMediaTestServer(t)
	ctx := context.Background()
	now := time.Now()

	contactID := uuid.New().String()
	channelID := uuid.New().String()
	msgID := uuid.New().String()

	_ = db.SaveContact(ctx, &domain.Contact{ID: contactID, DisplayName: "Carlos", CreatedAt: now, UpdatedAt: now})
	_ = db.SaveChannel(ctx, &domain.ContactChannel{ID: channelID, ContactID: contactID, Provider: domain.ProviderTelegram, RemoteID: "tg-999"})
	_ = db.EnsureConversation(ctx, &domain.Conversation{ID: contactID, ContactID: &contactID, Title: "Carlos", ActiveChannelID: &channelID})

	_ = db.SaveMessage(ctx, &domain.Message{
		ID:              msgID,
		ConversationID:  contactID,
		ChannelID:       channelID,
		RemoteMessageID: "rem-999",
		SenderType:      domain.SenderContact,
		Body:            "Un mensaje para reaccionar",
		ContentType:     domain.ContentTypeText,
		Status:          domain.StatusDelivered,
		SentAt:          now,
	})

	// 1. Enviar reacción
	resp, err := srv.SendReaction(ctx, &conduitv1.SendReactionRequest{
		MessageId: msgID,
		Emoji:     "👍",
	})
	if err != nil {
		t.Fatalf("SendReaction falló: %v", err)
	}
	if !resp.Success {
		t.Fatalf("se esperaba resp.Success = true")
	}
	if len(resp.Reactions) != 1 {
		t.Fatalf("se esperaba 1 reacción, obtenidas %d", len(resp.Reactions))
	}
	if resp.Reactions[0].Emoji != "👍" || !resp.Reactions[0].IncludesMe || resp.Reactions[0].Count != 1 {
		t.Errorf("datos de reacción incorrectos: %+v", resp.Reactions[0])
	}

	// 2. Enviar la misma reacción para togglear (remover)
	resp2, err := srv.SendReaction(ctx, &conduitv1.SendReactionRequest{
		MessageId: msgID,
		Emoji:     "👍",
	})
	if err != nil {
		t.Fatalf("segundo SendReaction falló: %v", err)
	}
	if !resp2.Success {
		t.Fatalf("se esperaba resp2.Success = true")
	}
	if len(resp2.Reactions) != 0 {
		t.Fatalf("se esperaba 0 reacciones tras toggle, hay %d", len(resp2.Reactions))
	}
}

func TestSendReactionRejectsEmptyMessageID(t *testing.T) {
	srv, _ := newMediaTestServer(t)
	ctx := context.Background()

	_, err := srv.SendReaction(ctx, &conduitv1.SendReactionRequest{
		MessageId: "",
		Emoji:     "👍",
	})
	if err == nil {
		t.Fatal("se esperaba rechazo al enviar message_id vacío")
	}
	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.InvalidArgument {
		t.Fatalf("se esperaba código InvalidArgument, se obtuvo %v", err)
	}
}

func TestSendMessagePropagatesReply(t *testing.T) {
	srv, db := newMediaTestServer(t)
	ctx := context.Background()
	now := time.Now()

	contactID := uuid.New().String()
	channelID := uuid.New().String()
	parentID := uuid.New().String()

	_ = db.SaveContact(ctx, &domain.Contact{ID: contactID, DisplayName: "Maria", CreatedAt: now, UpdatedAt: now})
	_ = db.SaveChannel(ctx, &domain.ContactChannel{ID: channelID, ContactID: contactID, Provider: domain.ProviderWhatsApp, RemoteID: "wa-555"})
	_ = db.EnsureConversation(ctx, &domain.Conversation{ID: contactID, ContactID: &contactID, Title: "Maria", ActiveChannelID: &channelID})

	_ = db.SaveMessage(ctx, &domain.Message{
		ID:              parentID,
		ConversationID:  contactID,
		ChannelID:       channelID,
		RemoteMessageID: "rem-parent-1",
		SenderType:      domain.SenderContact,
		Body:            "Pregunta original",
		ContentType:     domain.ContentTypeText,
		Status:          domain.StatusDelivered,
		SentAt:          now.Add(-10 * time.Second),
	})

	resp, err := srv.SendMessage(ctx, &conduitv1.SendMessageRequest{
		ConversationId:   contactID,
		Body:             "Respuesta a la pregunta",
		ReplyToMessageId: parentID,
	})
	if err != nil {
		t.Fatalf("SendMessage falló: %v", err)
	}
	msg := resp.Message
	if msg.ReplyToMessageId != parentID {
		t.Errorf("ReplyToMessageId esperado %s, obtenido %s", parentID, msg.ReplyToMessageId)
	}
	if msg.ReplyToSender != string(domain.SenderContact) {
		t.Errorf("ReplyToSender esperado %q, obtenido %q", domain.SenderContact, msg.ReplyToSender)
	}
	if msg.ReplyToBody != "Pregunta original" {
		t.Errorf("ReplyToBody esperado 'Pregunta original', obtenido %q", msg.ReplyToBody)
	}
}
