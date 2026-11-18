package storage

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/cestila/ConduitOmniChat/core/pkg/domain"
)

// Verifica que una base existente sin columnas de identidad canónica migre sin perder datos
// y que email/teléfono se persistan después de la migración.
func TestLegacyContactsTableMigratesIdentityColumns(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy_conduit.db")

	raw, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("error creando base legacy: %v", err)
	}
	legacySchema := `
	CREATE TABLE contacts (
		id TEXT PRIMARY KEY,
		display_name TEXT NOT NULL,
		avatar_url TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE contact_channels (
		id TEXT PRIMARY KEY,
		contact_id TEXT NOT NULL REFERENCES contacts(id) ON DELETE CASCADE,
		provider TEXT NOT NULL,
		remote_id TEXT NOT NULL,
		handle TEXT,
		is_preferred BOOLEAN DEFAULT FALSE,
		metadata TEXT,
		UNIQUE(provider, remote_id)
	);
	INSERT INTO contacts (id, display_name) VALUES ('legacy-1', 'Contacto Legacy');
	INSERT INTO contact_channels (id, contact_id, provider, remote_id, handle)
		VALUES ('legacy-ch-1', 'legacy-1', 'whatsapp', '+5491100000001', 'Legacy');
	`
	if _, err := raw.Exec(legacySchema); err != nil {
		raw.Close()
		t.Fatalf("error sembrando base legacy: %v", err)
	}
	raw.Close()

	db, err := NewDB(dbPath)
	if err != nil {
		t.Fatalf("error migrando base legacy: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	legacy, err := db.GetContactByID(ctx, "legacy-1")
	if err != nil {
		t.Fatalf("el contacto legacy se perdió en la migración: %v", err)
	}
	if legacy.DisplayName != "Contacto Legacy" {
		t.Fatalf("se esperaba conservar el nombre legacy, obtuvo '%s'", legacy.DisplayName)
	}
	if legacy.Email != "" || legacy.Phone != "" {
		t.Fatalf("un contacto legacy no debería tener identidad canónica: email='%s' phone='%s'", legacy.Email, legacy.Phone)
	}
	if len(legacy.Channels) != 1 {
		t.Fatalf("se esperaba conservar el canal legacy, obtuve %d", len(legacy.Channels))
	}

	// La identidad canónica debe persistir en la base migrada.
	legacy.Email = "legacy@example.com"
	legacy.Phone = "+5491100000001"
	if err := db.SaveContact(ctx, legacy); err != nil {
		t.Fatalf("error guardando identidad en base migrada: %v", err)
	}

	reloaded, err := db.GetContactByID(ctx, "legacy-1")
	if err != nil {
		t.Fatalf("error recargando contacto migrado: %v", err)
	}
	if reloaded.Email != "legacy@example.com" || reloaded.Phone != "+5491100000001" {
		t.Fatalf("la identidad no persistió tras la migración: email='%s' phone='%s'", reloaded.Email, reloaded.Phone)
	}

	// El índice único parcial debe impedir duplicar la identidad en otro contacto.
	other := &domain.Contact{ID: "legacy-2", DisplayName: "Otro", Email: "legacy@example.com"}
	if err := db.SaveContact(ctx, other); err == nil {
		t.Fatalf("se esperaba un error al duplicar el email canónico")
	}
}

func TestRenameContactSetsNameLockedAndUpdatesConversation(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "rename.db")
	db, err := NewDB(dbPath)
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	now := time.Now()
	if err := db.SaveContact(ctx, &domain.Contact{
		ID:          "c-1",
		DisplayName: ".",
		Phone:       "+5492664000001",
		CreatedAt:   now,
		UpdatedAt:   now,
	}); err != nil {
		t.Fatalf("SaveContact: %v", err)
	}
	if err := db.SaveChannel(ctx, &domain.ContactChannel{
		ID:        "ch-1",
		ContactID: "c-1",
		Provider:  domain.ProviderWhatsApp,
		RemoteID:  "+5492664000001",
	}); err != nil {
		t.Fatalf("SaveChannel: %v", err)
	}
	cID, chID := "c-1", "ch-1"
	if err := db.EnsureConversation(ctx, &domain.Conversation{
		ID:              "conv-1",
		ContactID:       &cID,
		IsGroup:         false,
		Title:           ".",
		ActiveChannelID: &chID,
	}); err != nil {
		t.Fatalf("EnsureConversation: %v", err)
	}

	renamed, err := db.RenameContact(ctx, "c-1", "Maximiliano Collado")
	if err != nil {
		t.Fatalf("RenameContact: %v", err)
	}
	if renamed.DisplayName != "Maximiliano Collado" || !renamed.NameLocked {
		t.Fatalf("unexpected renamed contact: %+v", renamed)
	}

	convs, err := db.ListConversations(ctx, 10, 0)
	if err != nil || len(convs) != 1 {
		t.Fatalf("ListConversations: %v len=%d", err, len(convs))
	}
	if convs[0].Title != "Maximiliano Collado" {
		t.Fatalf("expected conversation title updated, got %q", convs[0].Title)
	}
	if convs[0].ContactPhone != "+5492664000001" {
		t.Fatalf("expected ContactPhone '+5492664000001', got %q", convs[0].ContactPhone)
	}
}

func TestUpdateOutgoingMessageStatusByRemoteIDMonotonic(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "msg_status.db")
	db, err := NewDB(dbPath)
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	now := time.Now()
	_ = db.SaveContact(ctx, &domain.Contact{ID: "c-1", DisplayName: "Ana", CreatedAt: now, UpdatedAt: now})
	_ = db.SaveChannel(ctx, &domain.ContactChannel{ID: "ch-1", ContactID: "c-1", Provider: domain.ProviderWhatsApp, RemoteID: "+549111"})
	cID, chID := "c-1", "ch-1"
	_ = db.EnsureConversation(ctx, &domain.Conversation{ID: "conv-1", ContactID: &cID, IsGroup: false, Title: "Ana", ActiveChannelID: &chID})
	_ = db.SaveMessage(ctx, &domain.Message{
		ID:              "m-1",
		ConversationID:  "conv-1",
		ChannelID:       "ch-1",
		RemoteMessageID: "wa-msg-123",
		SenderType:      domain.SenderMe,
		Body:            "hola",
		ContentType:     domain.ContentTypeText,
		Status:          domain.StatusSent,
		SentAt:          now,
		ReceivedAt:      now,
	})

	id, convID, updated, err := db.UpdateOutgoingMessageStatusByRemoteID(ctx, "whatsapp", "wa-msg-123", domain.StatusDelivered)
	if err != nil || !updated || id != "m-1" || convID != "conv-1" {
		t.Fatalf("expected delivered update, got id=%q conv=%q updated=%v err=%v", id, convID, updated, err)
	}
	id, convID, updated, err = db.UpdateOutgoingMessageStatusByRemoteID(ctx, "whatsapp", "wa-msg-123", domain.StatusRead)
	if err != nil || !updated {
		t.Fatalf("expected read update, got updated=%v err=%v", updated, err)
	}
	// Never downgrade from read back to delivered
	_, _, updated, err = db.UpdateOutgoingMessageStatusByRemoteID(ctx, "whatsapp", "wa-msg-123", domain.StatusDelivered)
	if err != nil || updated {
		t.Fatalf("expected no downgrade from read, got updated=%v err=%v", updated, err)
	}
}
