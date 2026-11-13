package storage

import (
	"context"
	"database/sql"
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
