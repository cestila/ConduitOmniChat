package rpc

import (
	"context"
	"strings"

	"github.com/cestila/ConduitOmniChat/core/pkg/domain"
)

// ShouldAutoDownload aplica la política de autodescarga según los ajustes:
// imágenes (incluye stickers) y audio dependen de auto_download_media; video y todo
// lo demás depende de auto_download_documents.
func ShouldAutoDownload(mime string, autoMedia, autoDocuments bool) bool {
	m := strings.ToLower(mime)
	if strings.HasPrefix(m, "image/") || strings.HasPrefix(m, "audio/") {
		return autoMedia
	}
	return autoDocuments
}

// AutoDownload inicia en segundo plano la descarga de los adjuntos remote_only con receta
// que la política de ajustes permite. Nunca bloquea ni devuelve error.
func (d *Downloader) AutoDownload(ctx context.Context, msg *domain.Message) {
	if msg == nil || len(msg.Attachments) == 0 {
		return
	}
	autoMedia := d.db.GetSetting(ctx, "auto_download_media", "true") == "true"
	autoDocs := d.db.GetSetting(ctx, "auto_download_documents", "false") == "true"
	for _, a := range msg.Attachments {
		if a.DownloadStatus != domain.MediaRemoteOnly || a.RemoteRef == "" {
			continue
		}
		if !ShouldAutoDownload(a.MimeType, autoMedia, autoDocs) {
			continue
		}
		// Start ya es asíncrono (la descarga corre en su propia goroutine).
		_, _, _ = d.Start(ctx, a.ID)
	}
}
