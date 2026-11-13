package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/cestila/ConduitOmniChat/core/pkg/domain"
)

// El driver guarda time.Time con time.Time.String(), así que conviven valores
// con lectura monotónica (time.Now) y sin ella (time.Unix). Todos deben
// interpretarse en el mismo instante y en la zona correcta.
func TestParseStoredTimeFormatosHistoricos(t *testing.T) {
	local := time.Date(2026, 10, 21, 12, 40, 5, 236151593, time.FixedZone("-03", -3*3600))
	cases := []struct {
		name string
		raw  string
	}{
		{"string con monotónico", "2026-10-21 12:40:05.236151593 -0300 -03 m=+774.810804251"},
		{"string sin monotónico", "2026-10-21 12:40:05.236151593 -0300 -03"},
		{"sin fracción", "2026-10-21 12:40:05 -0300 -03"},
		{"rfc3339nano", "2026-10-21T12:40:05.236151593-03:00"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseStoredTime(tc.raw)
			if !ok {
				t.Fatalf("no se pudo interpretar %q", tc.raw)
			}
			if got.Unix() != local.Unix() {
				t.Fatalf("unix = %d, se esperaba %d", got.Unix(), local.Unix())
			}
		})
	}
}

func TestParseStoredTimeRechazaBasura(t *testing.T) {
	if _, ok := parseStoredTime("no es una fecha"); ok {
		t.Fatal("un valor inválido no debe interpretarse como fecha")
	}
}

// Un mensaje enviado desde la GUI usa time.Now() (con lectura monotónica). La
// conversación debe subir al tope y su timestamp debe ser posterior al de la
// última conversación con actividad: el bug era que se interpretaba como UTC y
// caía tres horas al pasado.
func TestListConversationsOrdenaMensajeSalienteReciente(t *testing.T) {
	db, err := NewDB(filepath.Join(t.TempDir(), "orden.db"))
	if err != nil {
		t.Fatalf("error creando base: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	base := time.Now().Add(-2 * time.Hour)

	old := &domain.Conversation{ID: "conv-vieja", Title: "Vieja", LastMessageAt: base}
	other := &domain.Conversation{ID: "conv-otra", Title: "Otra", LastMessageAt: base.Add(time.Hour)}
	for _, conv := range []*domain.Conversation{old, other} {
		if err := db.EnsureConversation(ctx, conv); err != nil {
			t.Fatalf("error creando conversación: %v", err)
		}
		if err := db.SaveChannel(ctx, &domain.ContactChannel{
			ID:       "canal-" + conv.ID,
			Provider: domain.ProviderTelegram,
			RemoteID: conv.ID,
		}); err != nil {
			t.Fatalf("error creando canal: %v", err)
		}
	}

	for i, conv := range []*domain.Conversation{old, other} {
		msg := &domain.Message{
			ID:              "msg-" + conv.ID,
			ConversationID:  conv.ID,
			ChannelID:       "canal-" + conv.ID,
			RemoteMessageID: "remote-" + conv.ID,
			SenderType:      domain.SenderContact,
			Body:            "hola",
			ContentType:     domain.ContentTypeText,
			Status:          domain.StatusDelivered,
			SentAt:          base.Add(time.Duration(i) * time.Hour),
		}
		if err := db.SaveMessage(ctx, msg); err != nil {
			t.Fatalf("error guardando mensaje: %v", err)
		}
	}

	// Envío desde la GUI: el mensaje más nuevo de todos, con reloj monotónico.
	sent := &domain.Message{
		ID:              "msg-enviado",
		ConversationID:  old.ID,
		ChannelID:       "canal-" + old.ID,
		RemoteMessageID: "out-msg-enviado",
		SenderType:      domain.SenderMe,
		Body:            "respuesta",
		ContentType:     domain.ContentTypeText,
		Status:          domain.StatusSent,
		SentAt:          time.Now(),
	}
	if err := db.SaveMessage(ctx, sent); err != nil {
		t.Fatalf("error guardando mensaje saliente: %v", err)
	}

	list, err := db.ListConversations(ctx, 10, 0)
	if err != nil {
		t.Fatalf("error listando: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("se esperaban 2 conversaciones, llegaron %d", len(list))
	}
	if list[0].Id != old.ID {
		t.Fatalf("la conversación con el mensaje recién enviado debe ir primera, llegó %q", list[0].Id)
	}
	if list[0].LastMessageAtUnix < list[1].LastMessageAtUnix {
		t.Fatalf("el timestamp del envío (%d) quedó antes que el de la otra conversación (%d)",
			list[0].LastMessageAtUnix, list[1].LastMessageAtUnix)
	}
}
