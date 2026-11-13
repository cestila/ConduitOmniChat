package rpc

import (
	"context"
	"testing"
	"time"

	"github.com/cestila/ConduitOmniChat/core/pkg/domain"
	conduitv1 "github.com/cestila/ConduitOmniChat/sdk/conduitv1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// S3.4: el core rechaza con InvalidArgument un canal que no pertenezca al contacto.
func TestSendMessageRejectsTargetChannelNotBelongingToContact(t *testing.T) {
	srv, db := newMediaTestServer(t)
	ctx := context.Background()
	now := time.Now()

	contactA := "contact-a"
	channelA := "channel-a"
	contactB := "contact-b"
	channelB := "channel-b"

	_ = db.SaveContact(ctx, &domain.Contact{ID: contactA, DisplayName: "Ana", CreatedAt: now, UpdatedAt: now})
	_ = db.SaveChannel(ctx, &domain.ContactChannel{ID: channelA, ContactID: contactA, Provider: domain.ProviderWhatsApp, RemoteID: "wa-a"})
	_ = db.EnsureConversation(ctx, &domain.Conversation{ID: contactA, ContactID: &contactA, Title: "Ana", ActiveChannelID: &channelA})

	_ = db.SaveContact(ctx, &domain.Contact{ID: contactB, DisplayName: "Beto", CreatedAt: now, UpdatedAt: now})
	_ = db.SaveChannel(ctx, &domain.ContactChannel{ID: channelB, ContactID: contactB, Provider: domain.ProviderTelegram, RemoteID: "tg-b"})

	_, err := srv.SendMessage(ctx, &conduitv1.SendMessageRequest{
		ConversationId:  contactA,
		TargetChannelId: channelB,
		Body:            "mensaje indebido",
	})
	if err == nil {
		t.Fatal("se esperaba rechazo con InvalidArgument al enviar a un canal de otro contacto")
	}
	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.InvalidArgument {
		t.Fatalf("se esperaba código InvalidArgument, obtuvo %v", err)
	}
}

// S3.4: el core rechaza un canal inexistente con InvalidArgument.
func TestSendMessageRejectsUnknownTargetChannel(t *testing.T) {
	srv, db := newMediaTestServer(t)
	ctx := context.Background()
	now := time.Now()

	contactA := "contact-a"
	channelA := "channel-a"
	_ = db.SaveContact(ctx, &domain.Contact{ID: contactA, DisplayName: "Ana", CreatedAt: now, UpdatedAt: now})
	_ = db.SaveChannel(ctx, &domain.ContactChannel{ID: channelA, ContactID: contactA, Provider: domain.ProviderWhatsApp, RemoteID: "wa-a"})
	_ = db.EnsureConversation(ctx, &domain.Conversation{ID: contactA, ContactID: &contactA, Title: "Ana", ActiveChannelID: &channelA})

	_, err := srv.SendMessage(ctx, &conduitv1.SendMessageRequest{
		ConversationId:  contactA,
		TargetChannelId: "canal-fantasma",
		Body:            "hola",
	})
	if err == nil {
		t.Fatal("se esperaba rechazo al enviar a un canal inexistente")
	}
	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.InvalidArgument {
		t.Fatalf("se esperaba código InvalidArgument, obtuvo %v", err)
	}
}

// S3.5: enviar por un canal manual lo fija como sticky de la conversación.
func TestSendMessageUsesTargetChannelAndUpdatesSticky(t *testing.T) {
	srv, db := newMediaTestServer(t)
	ctx := context.Background()
	now := time.Now()

	contactID := "contact-multi"
	waChannelID := "channel-wa"
	tgChannelID := "channel-tg"

	_ = db.SaveContact(ctx, &domain.Contact{ID: contactID, DisplayName: "Persona", CreatedAt: now, UpdatedAt: now})
	_ = db.SaveChannel(ctx, &domain.ContactChannel{ID: waChannelID, ContactID: contactID, Provider: domain.ProviderWhatsApp, RemoteID: "wa-1"})
	_ = db.SaveChannel(ctx, &domain.ContactChannel{ID: tgChannelID, ContactID: contactID, Provider: domain.ProviderTelegram, RemoteID: "tg-1"})
	_ = db.EnsureConversation(ctx, &domain.Conversation{ID: contactID, ContactID: &contactID, Title: "Persona", ActiveChannelID: &waChannelID})

	resp, err := srv.SendMessage(ctx, &conduitv1.SendMessageRequest{
		ConversationId:  contactID,
		TargetChannelId: tgChannelID,
		Body:            "enviado por Telegram",
	})
	if err != nil {
		t.Fatalf("error enviando mensaje: %v", err)
	}
	if resp.GetMessage().GetChannelId() != tgChannelID {
		t.Fatalf("el mensaje debió usar el canal %s, obtuvo %s", tgChannelID, resp.GetMessage().GetChannelId())
	}

	conv, err := db.GetConversationByID(ctx, contactID)
	if err != nil {
		t.Fatalf("error leyendo conversación: %v", err)
	}
	if conv.ActiveChannelId != tgChannelID {
		t.Fatalf("el sticky debió actualizarse a %s, obtuvo %s", tgChannelID, conv.ActiveChannelId)
	}
}
