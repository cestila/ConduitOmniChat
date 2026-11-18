package storage

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

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

func TestCleanupLegacyNonPersonContacts(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "cleanup_test.db")
	db, err := NewDB(dbPath)
	if err != nil {
		t.Fatalf("error creando db: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	// Contacto humano válido que debe permanecer intacto.
	if err := db.SaveContact(ctx, &domain.Contact{ID: "person-1", DisplayName: "Ana"}); err != nil {
		t.Fatalf("error guardando person-1: %v", err)
	}
	if err := db.SaveChannel(ctx, &domain.ContactChannel{
		ID: "ch-person-1", ContactID: "person-1", Provider: domain.ProviderWhatsApp, RemoteID: "5491112345678@s.whatsapp.net",
	}); err != nil {
		t.Fatalf("error guardando ch-person-1: %v", err)
	}

	// Contacto legacy de grupo (@g.us) y su conversación de grupo (contact_id = NULL).
	if err := db.SaveContact(ctx, &domain.Contact{ID: "group-contact-1", DisplayName: "Familia"}); err != nil {
		t.Fatalf("error guardando group-contact-1: %v", err)
	}
	if err := db.SaveChannel(ctx, &domain.ContactChannel{
		ID: "ch-group-1", ContactID: "group-contact-1", Provider: domain.ProviderWhatsApp, RemoteID: "12345@g.us",
	}); err != nil {
		t.Fatalf("error guardando ch-group-1: %v", err)
	}
	chGroupID := "ch-group-1"
	if err := db.EnsureConversation(ctx, &domain.Conversation{
		ID:              "whatsapp:12345@g.us",
		IsGroup:         true,
		Title:           "Familia",
		ActiveChannelID: &chGroupID,
	}); err != nil {
		t.Fatalf("error guardando conversación de grupo: %v", err)
	}
	if err := db.SaveMessage(ctx, &domain.Message{
		ID:              "msg-group-1",
		ConversationID:  "whatsapp:12345@g.us",
		ChannelID:       chGroupID,
		RemoteMessageID: "remote-msg-group-1",
		SenderType:      domain.SenderContact,
		Body:            "Hola grupo",
		ContentType:     domain.ContentTypeText,
		Status:          domain.StatusDelivered,
	}); err != nil {
		t.Fatalf("error guardando mensaje de grupo: %v", err)
	}

	deleted, err := db.CleanupNonPersonContacts(ctx)
	if err != nil {
		t.Fatalf("error en CleanupNonPersonContacts: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("se esperaba 1 contacto eliminado, obtuvo %d", deleted)
	}

	if _, err := db.GetContactByID(ctx, "group-contact-1"); err == nil {
		t.Fatalf("se esperaba que el contacto de grupo fuera eliminado")
	}
	if _, err := db.GetContactByID(ctx, "person-1"); err != nil {
		t.Fatalf("se esperaba que person-1 permaneciera intacto: %v", err)
	}

	conv, err := db.GetConversationByID(ctx, "whatsapp:12345@g.us")
	if err != nil {
		t.Fatalf("se esperaba que la conversación de grupo permaneciera intacta: %v", err)
	}
	if !conv.IsGroup || conv.Title != "Familia" {
		t.Fatalf("conversación de grupo alterada: %+v", conv)
	}
	if conv.GetActiveChannelId() != chGroupID {
		t.Fatalf("se esperaba active_channel_id=%q permaneciera intacto, obtuvo %q", chGroupID, conv.GetActiveChannelId())
	}

	detachedCh, err := db.GetChannelByID(ctx, chGroupID)
	if err != nil {
		t.Fatalf("se esperaba que el canal de grupo %q permaneciera en DB: %v", chGroupID, err)
	}
	if detachedCh.ContactID != "" {
		t.Fatalf("se esperaba que el canal de grupo quedara desacoplado (contact_id=''), obtuvo %q", detachedCh.ContactID)
	}

	var msgChannelID string
	if err := db.conn.QueryRowContext(ctx, `SELECT channel_id FROM messages WHERE id = 'msg-group-1'`).Scan(&msgChannelID); err != nil {
		t.Fatalf("error consultando mensaje de grupo: %v", err)
	}
	if msgChannelID != chGroupID {
		t.Fatalf("se esperaba messages.channel_id=%q, obtuvo %q", chGroupID, msgChannelID)
	}
}

func TestContactChannelsMigrationAndListContactsBatchLoad(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "migration_channels.db")
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
	INSERT INTO contacts (id, display_name) VALUES ('c1', 'Martín');
	INSERT INTO contact_channels (id, contact_id, provider, remote_id, handle)
		VALUES ('ch1', 'c1', 'whatsapp', '5491112345678@s.whatsapp.net', '');
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

	// Mismo (provider, remote_id) con distinto account_id debe permitirse tras la migración.
	if err := db.SaveChannel(ctx, &domain.ContactChannel{
		ID: "ch2", ContactID: "c1", Provider: domain.ProviderWhatsApp, AccountID: "wa-work", RemoteID: "5491112345678@s.whatsapp.net",
	}); err != nil {
		t.Fatalf("se esperaba permitir distinto account_id tras migración: %v", err)
	}

	gotCh, err := db.GetChannelByProviderAccountAndRemoteID(ctx, string(domain.ProviderWhatsApp), "wa-work", "5491112345678@s.whatsapp.net")
	if err != nil || gotCh.ID != "ch2" {
		t.Fatalf("error obteniendo canal por provider+account+remote: ch=%+v err=%v", gotCh, err)
	}

	contacts, err := db.ListContacts(ctx)
	if err != nil {
		t.Fatalf("error en ListContacts: %v", err)
	}
	if len(contacts) != 1 || len(contacts[0].Channels) != 2 {
		t.Fatalf("se esperaba 1 contacto con 2 canales en ListContacts, obtuvo %+v", contacts)
	}
}

func TestMigrateContactChannelsBackfillsEmptyAccountID(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "backfill_channels.db")
	raw, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("error creando db: %v", err)
	}
	seed := `
	CREATE TABLE accounts (
		id TEXT PRIMARY KEY,
		provider TEXT NOT NULL,
		label TEXT NOT NULL,
		identifier TEXT,
		status TEXT NOT NULL,
		qr_code TEXT,
		credentials TEXT,
		status_message TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
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
		account_id TEXT NOT NULL DEFAULT '',
		remote_id TEXT NOT NULL,
		handle TEXT,
		is_preferred BOOLEAN DEFAULT FALSE,
		metadata TEXT,
		UNIQUE(provider, account_id, remote_id)
	);
	INSERT INTO accounts (id, provider, label, identifier, status)
	VALUES ('acc-tg-uuid', 'TELEGRAM', 'Mi TG', 'default', 'connected');
	INSERT INTO contacts (id, display_name) VALUES ('c1', 'Ada');
	INSERT INTO contact_channels (id, contact_id, provider, account_id, remote_id, handle)
	VALUES ('ch-tg-legacy', 'c1', 'telegram', '', 'u1001', '@ada');
	`
	if _, err := raw.Exec(seed); err != nil {
		raw.Close()
		t.Fatalf("error sembrando db: %v", err)
	}
	raw.Close()

	db, err := NewDB(dbPath)
	if err != nil {
		t.Fatalf("error abriendo db con migración: %v", err)
	}
	defer db.Close()

	ch, err := db.GetChannelByID(context.Background(), "ch-tg-legacy")
	if err != nil {
		t.Fatalf("error leyendo ch-tg-legacy: %v", err)
	}
	if ch.AccountID != "acc-tg-uuid" {
		t.Fatalf("se esperaba account_id='acc-tg-uuid' tras backfill, obtuvo %q", ch.AccountID)
	}
}

func TestNewDBConcurrentWritesDoNotHitSQLiteBusy(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "concurrent.db")
	db, err := NewDB(dbPath)
	if err != nil {
		t.Fatalf("error abriendo db: %v", err)
	}
	defer db.Close()

	const workers = 10
	const writesPerWorker = 15
	errCh := make(chan error, workers*writesPerWorker)

	for w := 0; w < workers; w++ {
		go func(workerID int) {
			for i := 0; i < writesPerWorker; i++ {
				id := fmt.Sprintf("c-%d-%d", workerID, i)
				err := db.SaveContact(context.Background(), &domain.Contact{
					ID:          id,
					DisplayName: id,
				})
				errCh <- err
			}
		}(w)
	}

	for i := 0; i < workers*writesPerWorker; i++ {
		if err := <-errCh; err != nil {
			t.Fatalf("escritura concurrente falló (posible SQLITE_BUSY): %v", err)
		}
	}
}
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
