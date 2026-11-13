package storage

import (
	"context"
	"fmt"
	"strconv"

	conduitv1 "github.com/cestila/ConduitOmniChat/sdk/conduitv1"
	"google.golang.org/protobuf/encoding/protojson"
)

// EnsureAccountAndSettingsTables crea las tablas para multicuenta y configuración global.
func (d *DB) EnsureAccountAndSettingsTables() error {
	query := `
	CREATE TABLE IF NOT EXISTS accounts (
		id TEXT PRIMARY KEY,
		provider TEXT NOT NULL,
		label TEXT NOT NULL,
		identifier TEXT,
		status TEXT NOT NULL,
		qr_code TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS app_settings (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	);
	`
	if _, err := d.conn.Exec(query); err != nil {
		return err
	}
	// Migración segura si la tabla ya existía sin las columnas nuevas
	_, _ = d.conn.Exec("ALTER TABLE accounts ADD COLUMN qr_code TEXT;")
	_, _ = d.conn.Exec("ALTER TABLE accounts ADD COLUMN credentials TEXT;")
	_, _ = d.conn.Exec("ALTER TABLE accounts ADD COLUMN status_message TEXT;")
	return nil
}

// encodeCredentials serializa las credenciales a JSON con nombres snake_case estables.
func encodeCredentials(creds *conduitv1.ProviderCredentials) (string, error) {
	if creds == nil {
		return "", nil
	}
	raw, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(creds)
	if err != nil {
		return "", fmt.Errorf("error serializando credenciales: %w", err)
	}
	return string(raw), nil
}

// decodeCredentials reconstruye las credenciales desde el JSON persistido.
func decodeCredentials(raw string) (*conduitv1.ProviderCredentials, error) {
	if raw == "" {
		return nil, nil
	}
	creds := &conduitv1.ProviderCredentials{}
	if err := protojson.Unmarshal([]byte(raw), creds); err != nil {
		return nil, fmt.Errorf("error leyendo credenciales guardadas: %w", err)
	}
	return creds, nil
}

// ListAccounts devuelve todas las cuentas configuradas.
func (d *DB) ListAccounts(ctx context.Context) ([]*conduitv1.ProtoAccount, error) {
	rows, err := d.conn.QueryContext(ctx, "SELECT id, provider, label, identifier, status, COALESCE(qr_code, ''), COALESCE(credentials, ''), COALESCE(status_message, '') FROM accounts ORDER BY created_at ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []*conduitv1.ProtoAccount
	for rows.Next() {
		var acc conduitv1.ProtoAccount
		var prov, ident, qr, creds, statusMsg string
		if err := rows.Scan(&acc.Id, &prov, &acc.Label, &ident, &acc.Status, &qr, &creds, &statusMsg); err != nil {
			return nil, err
		}
		acc.Identifier = ident
		acc.QrCode = qr
		acc.StatusMessage = statusMsg
		acc.Provider = conduitv1.Provider(conduitv1.Provider_value["PROVIDER_"+prov])
		if parsed, err := decodeCredentials(creds); err == nil {
			acc.Credentials = parsed
		}
		list = append(list, &acc)
	}
	return list, nil
}

// SaveAccount inserta o actualiza una cuenta.
func (d *DB) SaveAccount(ctx context.Context, acc *conduitv1.ProtoAccount) error {
	query := `
	INSERT INTO accounts (id, provider, label, identifier, status, qr_code, credentials, status_message)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		label=excluded.label,
		identifier=excluded.identifier,
		status=excluded.status,
		qr_code=excluded.qr_code,
		credentials=excluded.credentials,
		status_message=excluded.status_message,
		updated_at=CURRENT_TIMESTAMP;
	`
	provStr := acc.Provider.String()
	if len(provStr) > 9 && provStr[:9] == "PROVIDER_" {
		provStr = provStr[9:]
	}
	creds, err := encodeCredentials(acc.Credentials)
	if err != nil {
		return err
	}
	_, err = d.conn.ExecContext(ctx, query, acc.Id, provStr, acc.Label, acc.Identifier, acc.Status, acc.QrCode, creds, acc.StatusMessage)
	return err
}

// DeleteAccount elimina una cuenta registrada.
func (d *DB) DeleteAccount(ctx context.Context, id string) error {
	_, err := d.conn.ExecContext(ctx, "DELETE FROM accounts WHERE id = ?", id)
	return err
}

// GetAccountByID obtiene una cuenta por su ID.
func (d *DB) GetAccountByID(ctx context.Context, id string) (*conduitv1.ProtoAccount, error) {
	query := "SELECT id, provider, label, identifier, status, COALESCE(qr_code, ''), COALESCE(credentials, ''), COALESCE(status_message, '') FROM accounts WHERE id = ?"
	row := d.conn.QueryRowContext(ctx, query, id)

	var acc conduitv1.ProtoAccount
	var prov, ident, qr, creds, statusMsg string
	if err := row.Scan(&acc.Id, &prov, &acc.Label, &ident, &acc.Status, &qr, &creds, &statusMsg); err != nil {
		return nil, err
	}
	acc.Identifier = ident
	acc.QrCode = qr
	acc.StatusMessage = statusMsg
	acc.Provider = conduitv1.Provider(conduitv1.Provider_value["PROVIDER_"+prov])
	if parsed, err := decodeCredentials(creds); err == nil {
		acc.Credentials = parsed
	}
	return &acc, nil
}

// GetSetting obtiene un valor de configuración.
func (d *DB) GetSetting(ctx context.Context, key, defaultValue string) string {
	var val string
	err := d.conn.QueryRowContext(ctx, "SELECT value FROM app_settings WHERE key = ?", key).Scan(&val)
	if err != nil || val == "" {
		return defaultValue
	}
	return val
}

// SetSetting guarda un valor de configuración.
func (d *DB) SetSetting(ctx context.Context, key, val string) error {
	query := `INSERT INTO app_settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value;`
	_, err := d.conn.ExecContext(ctx, query, key, val)
	return err
}

// GetAppSettings retorna la configuración completa normalizada.
func (d *DB) GetAppSettings(ctx context.Context) *conduitv1.AppSettings {
	fontSizeStr := d.GetSetting(ctx, "message_font_size", "14")
	var fontSize int32 = 14
	if s, err := strconv.Atoi(fontSizeStr); err == nil && s > 0 {
		fontSize = int32(s)
	}

	return &conduitv1.AppSettings{
		Language:              d.GetSetting(ctx, "language", "es"),
		Timezone:              d.GetSetting(ctx, "timezone", "America/Argentina/Buenos_Aires"),
		Theme:                 d.GetSetting(ctx, "theme", "dark"),
		NotificationsEnabled:  d.GetSetting(ctx, "notifications_enabled", "true") == "true",
		NotifyPreview:         d.GetSetting(ctx, "notify_preview", "true") == "true",
		NotifySound:           d.GetSetting(ctx, "notify_sound", "true") == "true",
		NotifyFlashTaskbar:    d.GetSetting(ctx, "notify_flash_taskbar", "true") == "true",
		ShowUnreadBadge:       d.GetSetting(ctx, "show_unread_badge", "true") == "true",
		SpellcheckEnabled:     d.GetSetting(ctx, "spellcheck_enabled", "true") == "true",
		SpellcheckLanguage:    d.GetSetting(ctx, "spellcheck_language", "es-ES"),
		AutoDownloadMedia:     d.GetSetting(ctx, "auto_download_media", "true") == "true",
		AutoDownloadDocuments: d.GetSetting(ctx, "auto_download_documents", "false") == "true",
		DownloadDirectory:     d.GetSetting(ctx, "download_directory", "~/Downloads/Conduit"),
		MessageFontSize:       fontSize,
		CompactMessageMode:    d.GetSetting(ctx, "compact_message_mode", "false") == "true",
		EnableMarkdown:        d.GetSetting(ctx, "enable_markdown", "true") == "true",
		MinimizeToTray:        d.GetSetting(ctx, "minimize_to_tray", "true") == "true",
		StartMinimized:        d.GetSetting(ctx, "start_minimized", "false") == "true",
		HardwareAcceleration:  d.GetSetting(ctx, "hardware_acceleration", "true") == "true",
	}
}

// SaveAppSettings guarda la configuración completa.
func (d *DB) SaveAppSettings(ctx context.Context, s *conduitv1.AppSettings) error {
	_ = d.SetSetting(ctx, "language", s.Language)
	_ = d.SetSetting(ctx, "timezone", s.Timezone)
	_ = d.SetSetting(ctx, "theme", s.Theme)
	_ = d.SetSetting(ctx, "notifications_enabled", fmt.Sprintf("%t", s.NotificationsEnabled))
	_ = d.SetSetting(ctx, "notify_preview", fmt.Sprintf("%t", s.NotifyPreview))
	_ = d.SetSetting(ctx, "notify_sound", fmt.Sprintf("%t", s.NotifySound))
	_ = d.SetSetting(ctx, "notify_flash_taskbar", fmt.Sprintf("%t", s.NotifyFlashTaskbar))
	_ = d.SetSetting(ctx, "show_unread_badge", fmt.Sprintf("%t", s.ShowUnreadBadge))
	_ = d.SetSetting(ctx, "spellcheck_enabled", fmt.Sprintf("%t", s.SpellcheckEnabled))
	_ = d.SetSetting(ctx, "spellcheck_language", s.SpellcheckLanguage)
	_ = d.SetSetting(ctx, "auto_download_media", fmt.Sprintf("%t", s.AutoDownloadMedia))
	_ = d.SetSetting(ctx, "auto_download_documents", fmt.Sprintf("%t", s.AutoDownloadDocuments))
	_ = d.SetSetting(ctx, "download_directory", s.DownloadDirectory)
	_ = d.SetSetting(ctx, "message_font_size", fmt.Sprintf("%d", s.MessageFontSize))
	_ = d.SetSetting(ctx, "compact_message_mode", fmt.Sprintf("%t", s.CompactMessageMode))
	_ = d.SetSetting(ctx, "enable_markdown", fmt.Sprintf("%t", s.EnableMarkdown))
	_ = d.SetSetting(ctx, "minimize_to_tray", fmt.Sprintf("%t", s.MinimizeToTray))
	_ = d.SetSetting(ctx, "start_minimized", fmt.Sprintf("%t", s.StartMinimized))
	_ = d.SetSetting(ctx, "hardware_acceleration", fmt.Sprintf("%t", s.HardwareAcceleration))
	return nil
}
