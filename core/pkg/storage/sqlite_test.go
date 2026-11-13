package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/cestila/ConduitOmniChat/core/pkg/domain"
	"github.com/google/uuid"
)

func TestStorageInitAndSearch(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_conduit.db")

	db, err := NewDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	// Crear contacto previo
	contactID := uuid.New().String()
	_, err = db.conn.Exec("INSERT INTO contacts (id, display_name) VALUES (?, ?)", contactID, "Juan Pérez")
	if err != nil {
		t.Fatalf("error creating contact: %v", err)
	}

	channelID := uuid.New().String()
	_, err = db.conn.Exec("INSERT INTO contact_channels (id, contact_id, provider, remote_id) VALUES (?, ?, ?, ?)",
		channelID, contactID, "telegram", "@juanperez")
	if err != nil {
		t.Fatalf("error creating channel: %v", err)
	}

	convID := uuid.New().String()
	_, err = db.conn.Exec("INSERT INTO conversations (id, contact_id, title) VALUES (?, ?, ?)",
		convID, contactID, "Chat con Juan")
	if err != nil {
		t.Fatalf("error creating conversation: %v", err)
	}

	// Guardar mensaje
	msg := &domain.Message{
		ID:              uuid.New().String(),
		ConversationID:  convID,
		ChannelID:       channelID,
		RemoteMessageID: "msg-101",
		SenderType:      domain.SenderContact,
		Body:            "Hola! Este es un mensaje de prueba con ConduitOmniChat para buscar.",
		ContentType:     domain.ContentTypeText,
		Status:          domain.StatusDelivered,
		SentAt:          time.Now(),
	}

	err = db.SaveMessage(ctx, msg)
	if err != nil {
		t.Fatalf("failed to save message: %v", err)
	}

	// Probar búsqueda FTS5
	results, err := db.SearchMessages(ctx, "ConduitOmniChat", 10)
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}

	if len(results) == 0 {
		t.Fatalf("expected at least 1 search result, got 0")
	}

	if results[0].RemoteMessageID != "msg-101" {
		t.Errorf("expected remote message id msg-101, got %s", results[0].RemoteMessageID)
	}
}

func TestGetMessagesAndConversations(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_conduit_convs.db")

	db, err := NewDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	convID := uuid.New().String()
	contactID := uuid.New().String()
	channelID := uuid.New().String()

	_, err = db.conn.Exec("INSERT INTO contacts (id, display_name) VALUES (?, ?)", contactID, "Test User")
	if err != nil {
		t.Fatalf("insert contact failed: %v", err)
	}
	_, err = db.conn.Exec("INSERT INTO contact_channels (id, contact_id, provider, remote_id) VALUES (?, ?, 'whatsapp', '+123456')", channelID, contactID)
	if err != nil {
		t.Fatalf("insert channel failed: %v", err)
	}

	err = db.EnsureConversation(ctx, &domain.Conversation{
		ID:        convID,
		ContactID: &contactID,
		Title:     "Chat Test",
	})
	if err != nil {
		t.Fatalf("EnsureConversation failed: %v", err)
	}

	msg := &domain.Message{
		ID:              uuid.New().String(),
		ConversationID:  convID,
		ChannelID:       channelID,
		RemoteMessageID: "rm-1",
		SenderType:      domain.SenderContact,
		Body:            "Hola mundo!",
		ContentType:     domain.ContentTypeText,
		Status:          domain.StatusDelivered,
		SentAt:          time.Now(),
	}
	if err := db.SaveMessage(ctx, msg); err != nil {
		t.Fatalf("SaveMessage failed: %v", err)
	}

	convs, err := db.ListConversations(ctx, 10, 0)
	if err != nil {
		t.Fatalf("ListConversations failed: %v", err)
	}
	if len(convs) != 1 {
		t.Fatalf("expected 1 conversation, got %d", len(convs))
	}
	if convs[0].LastMessagePreview != "Hola mundo!" {
		t.Errorf("expected snippet 'Hola mundo!', got '%s'", convs[0].LastMessagePreview)
	}

	msgs, err := db.GetMessagesByConversation(ctx, convID, 10, "")
	if err != nil {
		t.Fatalf("GetMessagesByConversation failed: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Body != "Hola mundo!" {
		t.Errorf("expected msg body 'Hola mundo!', got '%s'", msgs[0].Body)
	}
}

func TestSaveMessageUpdatesContentTypeAndMetadata(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_poll_update.db")
	db, err := NewDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer db.Close()
	ctx := context.Background()

	contactID := uuid.New().String()
	_, _ = db.conn.Exec("INSERT INTO contacts (id, display_name) VALUES (?, ?)", contactID, "Test")
	channelID := uuid.New().String()
	_, _ = db.conn.Exec("INSERT INTO contact_channels (id, contact_id, provider, remote_id) VALUES (?, ?, ?, ?)",
		channelID, contactID, "whatsapp", "123456@s.whatsapp.net")
	convID := uuid.New().String()
	_, _ = db.conn.Exec("INSERT INTO conversations (id, contact_id, title) VALUES (?, ?, ?)",
		convID, contactID, "Test Chat")

	remoteID := "msg-poll-123"
	// 1. Guardar mensaje originalmente como texto vacío
	msg1 := &domain.Message{
		ID:              uuid.New().String(),
		ConversationID:  convID,
		ChannelID:       channelID,
		RemoteMessageID: remoteID,
		SenderType:      domain.SenderContact,
		Body:            "",
		ContentType:     domain.ContentTypeText,
		Status:          domain.StatusDelivered,
		SentAt:          time.Now(),
	}
	if err := db.SaveMessage(ctx, msg1); err != nil {
		t.Fatalf("error guardando msg1: %v", err)
	}

	// 2. Re-guardar el mismo mensaje con content_type = "poll" y metadata de encuesta
	msg2 := &domain.Message{
		ID:              uuid.New().String(),
		ConversationID:  convID,
		ChannelID:       channelID,
		RemoteMessageID: remoteID,
		SenderType:      domain.SenderContact,
		Body:            "¿Pregunta?",
		ContentType:     domain.ContentTypePoll,
		Status:          domain.StatusDelivered,
		SentAt:          time.Now(),
		Poll: &domain.Poll{
			Question: "¿Pregunta?",
			Options: []domain.PollOption{
				{ID: "0", Text: "Opción A"},
				{ID: "1", Text: "Opción B"},
			},
			SelectableCount: 1,
		},
	}
	if err := db.SaveMessage(ctx, msg2); err != nil {
		t.Fatalf("error re-guardando msg2: %v", err)
	}

	// 3. Verificar que se actualizó el registro existente
	found, err := db.FindMessageByRemoteID(ctx, channelID, remoteID)
	if err != nil || found == nil {
		t.Fatalf("mensaje no encontrado: %v", err)
	}
	if found.ContentType != domain.ContentTypePoll {
		t.Errorf("ContentType esperado %q, obtenido %q", domain.ContentTypePoll, found.ContentType)
	}
	if found.Poll == nil || found.Poll.Question != "¿Pregunta?" {
		t.Errorf("Poll esperado no nulo con pregunta, obtenido: %+v", found.Poll)
	}
}
