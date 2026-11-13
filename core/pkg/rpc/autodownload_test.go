package rpc

import (
	"context"
	"testing"
	"time"

	"github.com/cestila/ConduitOmniChat/core/pkg/domain"
	conduitv1 "github.com/cestila/ConduitOmniChat/sdk/conduitv1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShouldAutoDownload(t *testing.T) {
	cases := []struct {
		mime        string
		media, docs bool
		want        bool
	}{
		{"image/jpeg", true, false, true},
		{"image/webp", true, false, true}, // sticker
		{"image/png", false, true, false},
		{"audio/ogg", true, false, true},
		{"audio/mpeg", false, true, false},
		{"video/mp4", true, false, false},
		{"video/mp4", false, true, true},
		{"application/pdf", true, false, false},
		{"application/pdf", false, true, true},
		{"", true, false, false},
		{"", false, true, true},
		{"IMAGE/JPEG", true, false, true},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, ShouldAutoDownload(c.mime, c.media, c.docs), "%s media=%v docs=%v", c.mime, c.media, c.docs)
	}
}

func TestAutoDownloadHonorsSettings(t *testing.T) {
	e := newDLEnv(t)
	ctx := context.Background()
	for _, a := range []domain.Attachment{
		{ID: "img", MessageID: "msg", MimeType: "image/jpeg", DownloadStatus: domain.MediaRemoteOnly, RemoteRef: "r1"},
		{ID: "pdf", MessageID: "msg", MimeType: "application/pdf", DownloadStatus: domain.MediaRemoteOnly, RemoteRef: "r2"},
		{ID: "noref", MessageID: "msg", MimeType: "image/png", DownloadStatus: domain.MediaRemoteOnly},
		{ID: "done", MessageID: "msg", MimeType: "image/png", DownloadStatus: domain.MediaCached, RemoteRef: "r3"},
	} {
		require.NoError(t, e.db.SaveAttachment(ctx, &a))
	}
	p := &mediaPlugin{resp: &conduitv1.PluginDownloadMediaResponse{Success: true, LocalPath: "/m/x", FileSize: 1}}
	e.register(p)
	msg, err := e.db.GetMessageWithAttachments(ctx, "msg")
	require.NoError(t, err)

	// Valores por defecto: media sí, documentos no.
	e.srv.downloader.AutoDownload(ctx, msg)
	require.Eventually(t, func() bool { return e.stored(t, "img").DownloadStatus == domain.MediaCached }, 3*time.Second, 10*time.Millisecond)
	assert.Equal(t, domain.MediaRemoteOnly, e.stored(t, "pdf").DownloadStatus)
	assert.Equal(t, domain.MediaRemoteOnly, e.stored(t, "noref").DownloadStatus)
	assert.Equal(t, int32(1), p.calls.Load())

	// Activar documentos y desactivar media.
	require.NoError(t, e.db.SetSetting(ctx, "auto_download_documents", "true"))
	require.NoError(t, e.db.SetSetting(ctx, "auto_download_media", "false"))
	e.srv.downloader.AutoDownload(ctx, msg)
	require.Eventually(t, func() bool { return e.stored(t, "pdf").DownloadStatus == domain.MediaCached }, 3*time.Second, 10*time.Millisecond)
	assert.Equal(t, int32(2), p.calls.Load())
}
