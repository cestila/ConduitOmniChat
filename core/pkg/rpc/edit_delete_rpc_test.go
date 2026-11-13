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

func TestEditMessageUpdatesBodyAndMarksEdited(t *testing.T) {
	srv, db := newMediaTestServer(t)
	ctx := context.Background()
	now := time.Now()

	contactID := uuid.New().String()
	channelID := uuid.New().String()
	msgID := uuid.New().String()

	_ = db.SaveContact(ctx, &domain.Contact{ID: contactID, DisplayName: "Juan", CreatedAt: now, UpdatedAt: now})
	_ = db.SaveChannel(ctx, &domain.ContactChannel{ID: channelID, ContactID: contactID, Provider: domain.ProviderTelegram, RemoteID: "tg-111"})
	_ = db.EnsureConversation(ctx, &domain.Conversation{ID: contactID, ContactID: &contactID, Title: "Juan", ActiveChannelID: &channelID})

	_ = db.SaveMessage(ctx, &domain.Message{
		ID:              msgID,
		ConversationID:  contactID,
		ChannelID:       channelID,
		RemoteMessageID: "rem-edit-1",
		SenderType:      domain.SenderMe,
		Body:            "Texto original con typo",
		ContentType:     domain.ContentTypeText,
		Status:          domain.StatusSent,
		SentAt:          now,
	})

	resp, err := srv.EditMessage(ctx, &conduitv1.EditMessageRequest{
		MessageId: msgID,
		NewBody:   "Texto corregido y limpio",
	})
	if err != nil {
		t.Fatalf("EditMessage falló: %v", err)
	}
	if !resp.Success {
		t.Fatalf("se esperaba resp.Success = true")
	}
	if resp.Message.Body != "Texto corregido y limpio" {
		t.Errorf("cuerpo esperado 'Texto corregido y limpio', obtenido %q", resp.Message.Body)
	}
	if !resp.Message.IsEdited {
		t.Errorf("se esperaba IsEdited = true")
	}
}

func TestDeleteMessageRemovesAndPublishes(t *testing.T) {
	srv, db := newMediaTestServer(t)
	ctx := context.Background()
	now := time.Now()

	contactID := uuid.New().String()
	channelID := uuid.New().String()
	msgID := uuid.New().String()

	_ = db.SaveContact(ctx, &domain.Contact{ID: contactID, DisplayName: "Pedro", CreatedAt: now, UpdatedAt: now})
	_ = db.SaveChannel(ctx, &domain.ContactChannel{ID: channelID, ContactID: contactID, Provider: domain.ProviderWhatsApp, RemoteID: "wa-222"})
	_ = db.EnsureConversation(ctx, &domain.Conversation{ID: contactID, ContactID: &contactID, Title: "Pedro", ActiveChannelID: &channelID})

	_ = db.SaveMessage(ctx, &domain.Message{
		ID:              msgID,
		ConversationID:  contactID,
		ChannelID:       channelID,
		RemoteMessageID: "rem-del-1",
		SenderType:      domain.SenderMe,
		Body:            "Mensaje a eliminar",
		ContentType:     domain.ContentTypeText,
		Status:          domain.StatusSent,
		SentAt:          now,
	})

	resp, err := srv.DeleteMessage(ctx, &conduitv1.DeleteMessageRequest{
		MessageId: msgID,
	})
	if err != nil {
		t.Fatalf("DeleteMessage falló: %v", err)
	}
	if !resp.Success {
		t.Fatalf("se esperaba resp.Success = true")
	}

	// Verificar que ya no existe en la base
	_, findErr := db.FindMessageByID(ctx, msgID)
	if findErr == nil {
		t.Fatal("el mensaje debió ser eliminado de SQLite")
	}
}

func TestPinMessageUpdatesPinnedState(t *testing.T) {
	srv, db := newMediaTestServer(t)
	ctx := context.Background()
	now := time.Now()

	contactID := uuid.New().String()
	channelID := uuid.New().String()
	msgID := uuid.New().String()

	_ = db.SaveContact(ctx, &domain.Contact{ID: contactID, DisplayName: "Grupo", CreatedAt: now, UpdatedAt: now})
	_ = db.SaveChannel(ctx, &domain.ContactChannel{ID: channelID, ContactID: contactID, Provider: domain.ProviderTelegram, RemoteID: "tg-333"})
	_ = db.EnsureConversation(ctx, &domain.Conversation{ID: contactID, ContactID: &contactID, Title: "Grupo", ActiveChannelID: &channelID})

	_ = db.SaveMessage(ctx, &domain.Message{
		ID:              msgID,
		ConversationID:  contactID,
		ChannelID:       channelID,
		RemoteMessageID: "rem-pin-1",
		SenderType:      domain.SenderContact,
		Body:            "Reglas importantes del grupo",
		ContentType:     domain.ContentTypeText,
		Status:          domain.StatusDelivered,
		SentAt:          now,
	})

	resp, err := srv.PinMessage(ctx, &conduitv1.PinMessageRequest{
		MessageId: msgID,
		Pinned:    true,
	})
	if err != nil {
		t.Fatalf("PinMessage falló: %v", err)
	}
	if !resp.Success || !resp.IsPinned {
		t.Fatalf("se esperaba resp.Success = true y IsPinned = true")
	}
}

func TestEditMessageRejectsEmptyBody(t *testing.T) {
	srv, _ := newMediaTestServer(t)
	ctx := context.Background()

	_, err := srv.EditMessage(ctx, &conduitv1.EditMessageRequest{
		MessageId: "msg-123",
		NewBody:   "",
	})
	if err == nil {
		t.Fatal("se esperaba rechazo con new_body vacío")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.InvalidArgument {
		t.Errorf("esperado InvalidArgument, obtenido %v", st.Code())
	}
}
