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

func TestSendVoteUpdatesPollAndReturns(t *testing.T) {
	srv, db := newMediaTestServer(t)
	ctx := context.Background()
	now := time.Now()

	contactID := uuid.New().String()
	channelID := uuid.New().String()
	msgID := uuid.New().String()

	_ = db.SaveContact(ctx, &domain.Contact{ID: contactID, DisplayName: "Grupo Amigos", CreatedAt: now, UpdatedAt: now})
	_ = db.SaveChannel(ctx, &domain.ContactChannel{ID: channelID, ContactID: contactID, Provider: domain.ProviderTelegram, RemoteID: "tg-grp"})
	_ = db.EnsureConversation(ctx, &domain.Conversation{ID: contactID, ContactID: &contactID, Title: "Grupo Amigos", ActiveChannelID: &channelID})

	initialPoll := &domain.Poll{
		Question:        "¿Almorzamos pizza o hamburguesa?",
		SelectableCount: 1,
		Options: []domain.PollOption{
			{ID: "0", Text: "Pizza", VoteCount: 2, Chosen: false},
			{ID: "1", Text: "Hamburguesa", VoteCount: 1, Chosen: false},
		},
		TotalVotes: 3,
	}
	pollJSON, _ := json.Marshal(initialPoll)

	_ = db.SaveMessage(ctx, &domain.Message{
		ID:              msgID,
		ConversationID:  contactID,
		ChannelID:       channelID,
		RemoteMessageID: "poll-rem-1",
		SenderType:      domain.SenderContact,
		Body:            initialPoll.Question,
		ContentType:     domain.ContentTypePoll,
		Status:          domain.StatusDelivered,
		SentAt:          now,
		Metadata:        string(pollJSON),
		Poll:            initialPoll,
	})

	// 1. Votar por Pizza (ID "0")
	resp, err := srv.SendVote(ctx, &conduitv1.SendVoteRequest{
		MessageId:       msgID,
		SelectedOptions: []string{"0"},
	})
	if err != nil {
		t.Fatalf("SendVote falló: %v", err)
	}
	if !resp.Success {
		t.Fatalf("se esperaba resp.Success = true")
	}
	poll := resp.Poll
	if poll.Question != "¿Almorzamos pizza o hamburguesa?" {
		t.Errorf("pregunta incorrecta: %s", poll.Question)
	}
	if len(poll.Options) != 2 {
		t.Fatalf("se esperaban 2 opciones, obtenidas %d", len(poll.Options))
	}
	if !poll.Options[0].Chosen || poll.Options[0].VoteCount != 3 {
		t.Errorf("opción 0 esperaba chosen=true y count=3, obtenido %+v", poll.Options[0])
	}
	if poll.Options[1].Chosen || poll.Options[1].VoteCount != 1 {
		t.Errorf("opción 1 esperaba chosen=false y count=1, obtenido %+v", poll.Options[1])
	}

	// 2. Cambiar voto a Hamburguesa (ID "1")
	resp2, err := srv.SendVote(ctx, &conduitv1.SendVoteRequest{
		MessageId:       msgID,
		SelectedOptions: []string{"1"},
	})
	if err != nil {
		t.Fatalf("segundo SendVote falló: %v", err)
	}
	poll2 := resp2.Poll
	if poll2.Options[0].Chosen || poll2.Options[0].VoteCount != 2 {
		t.Errorf("opción 0 tras cambio esperaba chosen=false y count=2, obtenido %+v", poll2.Options[0])
	}
	if !poll2.Options[1].Chosen || poll2.Options[1].VoteCount != 2 {
		t.Errorf("opción 1 tras cambio esperaba chosen=true y count=2, obtenido %+v", poll2.Options[1])
	}
}

func TestSendVoteRejectsEmptyOptionsOrMessageID(t *testing.T) {
	srv, _ := newMediaTestServer(t)
	ctx := context.Background()

	_, err := srv.SendVote(ctx, &conduitv1.SendVoteRequest{
		MessageId:       "",
		SelectedOptions: []string{"0"},
	})
	if err == nil {
		t.Fatal("se esperaba rechazo con message_id vacío")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.InvalidArgument {
		t.Errorf("esperado InvalidArgument, obtenido %v", st.Code())
	}

	_, err2 := srv.SendVote(ctx, &conduitv1.SendVoteRequest{
		MessageId:       "msg-123",
		SelectedOptions: []string{},
	})
	if err2 == nil {
		t.Fatal("se esperaba rechazo con selected_options vacío")
	}
	st2, _ := status.FromError(err2)
	if st2.Code() != codes.InvalidArgument {
		t.Errorf("esperado InvalidArgument, obtenido %v", st2.Code())
	}
}
