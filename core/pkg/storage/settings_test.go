package storage

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	conduitv1 "github.com/cestila/ConduitOmniChat/sdk/conduitv1"
)

// Verifica que las credenciales por proveedor y el mensaje de estado se persistan
// y se recuperen tanto por ID como en el listado.
func TestAccountCredentialsPersist(t *testing.T) {
	db, err := NewDB(filepath.Join(t.TempDir(), "accounts.db"))
	if err != nil {
		t.Fatalf("error creando base: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	acc := &conduitv1.ProtoAccount{
		Id:         "acc-tg",
		Provider:   conduitv1.Provider_PROVIDER_TELEGRAM,
		Label:      "TG Trabajo",
		Identifier: "+5491100000002",
		Status:     "error",
		Credentials: &conduitv1.ProviderCredentials{
			ApiId:   "123456",
			ApiHash: "abcdef0123456789",
			Phone:   "+5491100000002",
		},
		StatusMessage: "faltan credenciales de Telegram: api_hash",
	}
	if err := db.SaveAccount(ctx, acc); err != nil {
		t.Fatalf("error guardando cuenta: %v", err)
	}

	loaded, err := db.GetAccountByID(ctx, "acc-tg")
	if err != nil {
		t.Fatalf("error leyendo cuenta: %v", err)
	}
	if got := loaded.GetCredentials().GetApiId(); got != "123456" {
		t.Fatalf("se esperaba api_id 123456, obtuve %q", got)
	}
	if got := loaded.GetCredentials().GetApiHash(); got != "abcdef0123456789" {
		t.Fatalf("se esperaba api_hash persistido, obtuve %q", got)
	}
	if got := loaded.GetCredentials().GetPhone(); got != "+5491100000002" {
		t.Fatalf("se esperaba phone persistido, obtuve %q", got)
	}
	if loaded.StatusMessage != "faltan credenciales de Telegram: api_hash" {
		t.Fatalf("se esperaba conservar el mensaje de estado, obtuve %q", loaded.StatusMessage)
	}

	list, err := db.ListAccounts(ctx)
	if err != nil {
		t.Fatalf("error listando cuentas: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("se esperaba 1 cuenta, obtuve %d", len(list))
	}
	if got := list[0].GetCredentials().GetApiHash(); got != "abcdef0123456789" {
		t.Fatalf("el listado no conserva las credenciales, obtuve %q", got)
	}
}

// Verifica que una base con la tabla accounts sin columnas de credenciales migre
// conservando las cuentas existentes y acepte credenciales después.
func TestLegacyAccountsTableMigratesCredentialColumns(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy_accounts.db")

	raw, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("error creando base legacy: %v", err)
	}
	legacySchema := `
	CREATE TABLE accounts (
		id TEXT PRIMARY KEY,
		provider TEXT NOT NULL,
		label TEXT NOT NULL,
		identifier TEXT,
		status TEXT NOT NULL,
		qr_code TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	INSERT INTO accounts (id, provider, label, identifier, status)
		VALUES ('legacy-acc', 'telegram', 'TG Legacy', '+5491100000003', 'disconnected');
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

	acc, err := db.GetAccountByID(ctx, "legacy-acc")
	if err != nil {
		t.Fatalf("la cuenta legacy se perdió en la migración: %v", err)
	}
	if acc.Label != "TG Legacy" {
		t.Fatalf("se esperaba conservar la etiqueta legacy, obtuve %q", acc.Label)
	}
	if acc.GetCredentials().GetApiId() != "" {
		t.Fatalf("una cuenta legacy no debería tener credenciales, obtuve %q", acc.GetCredentials().GetApiId())
	}

	acc.Credentials = &conduitv1.ProviderCredentials{ApiId: "999", ApiHash: "hash", Phone: "+5491100000003"}
	acc.Status = "demo"
	acc.StatusMessage = "modo demostración"
	if err := db.SaveAccount(ctx, acc); err != nil {
		t.Fatalf("error guardando credenciales en base migrada: %v", err)
	}

	reloaded, err := db.GetAccountByID(ctx, "legacy-acc")
	if err != nil {
		t.Fatalf("error recargando cuenta migrada: %v", err)
	}
	if reloaded.GetCredentials().GetApiId() != "999" || reloaded.StatusMessage != "modo demostración" {
		t.Fatalf("las credenciales no persistieron tras la migración: %+v", reloaded)
	}
}
