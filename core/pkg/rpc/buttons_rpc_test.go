package rpc

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/cestila/ConduitOmniChat/core/pkg/domain"
	conduitv1 "github.com/cestila/ConduitOmniChat/sdk/conduitv1"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestClickButtonRejectsEmptyIDs(t *testing.T) {
	srv, _ := newMediaTestServer(t)
	ctx := context.Background()

	_, err := srv.ClickButton(ctx, &conduitv1.ClickButtonRequest{
		MessageId: "",
		ButtonId:  "btn-1",
	})
	if err == nil {
		t.Fatal("se esperaba rechazo con message_id vacío")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.InvalidArgument {
		t.Errorf("esperado InvalidArgument, obtenido %v", st.Code())
	}

	_, err2 := srv.ClickButton(ctx, &conduitv1.ClickButtonRequest{
		MessageId: "msg-1",
		ButtonId:  "",
	})
	if err2 == nil {
		t.Fatal("se esperaba rechazo con button_id vacío")
	}
	st2, _ := status.FromError(err2)
	if st2.Code() != codes.InvalidArgument {
		t.Errorf("esperado InvalidArgument, obtenido %v", st2.Code())
	}
}

func TestClickButtonFindsMessageAndExecutes(t *testing.T) {
	srv, db := newMediaTestServer(t)
	ctx := context.Background()
	now := time.Now()

	contactID := uuid.New().String()
	channelID := uuid.New().String()
	msgID := uuid.New().String()

	_ = db.SaveContact(ctx, &domain.Contact{ID: contactID, DisplayName: "Bot Ayuda", CreatedAt: now, UpdatedAt: now})
	_ = db.SaveChannel(ctx, &domain.ContactChannel{ID: channelID, ContactID: contactID, Provider: domain.ProviderTelegram, RemoteID: "tg-bot"})
	_ = db.EnsureConversation(ctx, &domain.Conversation{ID: contactID, ContactID: &contactID, Title: "Bot Ayuda", ActiveChannelID: &channelID})

	buttonRows := []domain.ButtonRow{
		{
			Buttons: []domain.Button{
				{ID: "cb_confirmar", Text: "Confirmar", Type: "callback"},
				{ID: "url_web", Text: "Ver Web", Type: "url", URL: "https://conduit.chat"},
			},
		},
	}
	metaJSON, _ := json.Marshal(map[string]any{"button_rows": buttonRows})

	_ = db.SaveMessage(ctx, &domain.Message{
		ID:              msgID,
		ConversationID:  contactID,
		ChannelID:       channelID,
		RemoteMessageID: "rem-btn-1",
		SenderType:      domain.SenderContact,
		Body:            "Por favor elige una opción:",
		ContentType:     domain.ContentTypeText,
		Status:          domain.StatusDelivered,
		SentAt:          now,
		Metadata:        string(metaJSON),
		ButtonRows:      buttonRows,
	})

	resp, err := srv.ClickButton(ctx, &conduitv1.ClickButtonRequest{
		MessageId:  msgID,
		ButtonId:   "cb_confirmar",
		ButtonText: "Confirmar",
	})
	if err != nil {
		t.Fatalf("ClickButton falló: %v", err)
	}
	if !resp.Success {
		t.Fatalf("se esperaba resp.Success = true")
	}
}
