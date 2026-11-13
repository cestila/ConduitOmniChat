package pluginmgr

// CatalogEntry describe un plugin oficial instalable desde GitHub Releases.
type CatalogEntry struct {
	ID          string
	Name        string
	Description string
	Repository  string // owner/repo
}

// Catalog lista los plugins oficiales conocidos, instalados o no.
var Catalog = []CatalogEntry{
	{ID: "whatsapp", Name: "WhatsApp Multi-Device", Description: "Conexión directa vía protocolo Noise/E2EE y código QR", Repository: "cestila/conduit-plugin-whatsapp"},
	{ID: "telegram", Name: "Telegram MTProto", Description: "Cliente de usuario personal de alta velocidad con 2FA", Repository: "cestila/conduit-plugin-telegram"},
}

// CatalogEntryFor devuelve la entrada oficial para un id de plugin.
func CatalogEntryFor(id string) (CatalogEntry, bool) {
	for _, e := range Catalog {
		if e.ID == id {
			return e, true
		}
	}
	return CatalogEntry{}, false
}
