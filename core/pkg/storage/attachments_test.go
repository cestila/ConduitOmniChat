package storage

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/cestila/ConduitOmniChat/core/pkg/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newAttachmentDB(t *testing.T) *DB {
	t.Helper()
	db, err := NewDB(filepath.Join(t.TempDir(), "att.db"))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	require.NoError(t, db.SaveChannel(ctx, &domain.ContactChannel{ID: "ch", Provider: domain.ProviderWhatsApp, RemoteID: "r", Handle: "r"}))
	require.NoError(t, db.EnsureConversation(ctx, &domain.Conversation{ID: "conv", Title: "c"}))
	require.NoError(t, db.SaveMessage(ctx, &domain.Message{
		ID: "msg", ConversationID: "conv", ChannelID: "ch", RemoteMessageID: "rm1",
		SenderType: domain.SenderContact, Body: "x", Status: domain.StatusDelivered, SentAt: time.Unix(1700000000, 0),
	}))
	return db
}

func strPtr(s string) *string { return &s }

func TestSaveAttachmentCachedIsSticky(t *testing.T) {
	db := newAttachmentDB(t)
	ctx := context.Background()
	require.NoError(t, db.SaveAttachment(ctx, &domain.Attachment{
		ID: "a1", MessageID: "msg", FileName: "f.jpg", MimeType: "image/jpeg", FileSize: 10,
		DownloadStatus: domain.MediaRemoteOnly, RemoteRef: "ref-1",
	}))
	require.NoError(t, db.UpdateAttachmentDownload(ctx, "a1", domain.MediaCached, strPtr("/tmp/f.jpg"), 99))

	for _, st := range []domain.DownloadStatus{domain.MediaRemoteOnly, domain.MediaFailed, domain.MediaDownloading} {
		require.NoError(t, db.SaveAttachment(ctx, &domain.Attachment{
			ID: "a1", MessageID: "msg", FileName: "f.jpg", MimeType: "image/jpeg", FileSize: 10,
			DownloadStatus: st, RemoteRef: "ref-2",
		}))
		got, err := db.GetAttachmentByID(ctx, "a1")
		require.NoError(t, err)
		assert.Equal(t, domain.MediaCached, got.DownloadStatus, st)
		require.NotNil(t, got.LocalPath)
		assert.Equal(t, "/tmp/f.jpg", *got.LocalPath)
		assert.Equal(t, int64(99), got.FileSize)
		assert.Equal(t, "ref-1", got.RemoteRef, "una remote_ref existente no se pisa")
	}
}

func TestSaveAttachmentCachedWithNewFileOverwrites(t *testing.T) {
	db := newAttachmentDB(t)
	ctx := context.Background()
	require.NoError(t, db.SaveAttachment(ctx, &domain.Attachment{
		ID: "a1", MessageID: "msg", MimeType: "image/jpeg", LocalPath: strPtr("/old"), DownloadStatus: domain.MediaCached,
	}))
	require.NoError(t, db.SaveAttachment(ctx, &domain.Attachment{
		ID: "a1", MessageID: "msg", MimeType: "image/jpeg", LocalPath: strPtr("/new"), DownloadStatus: domain.MediaCached,
	}))
	got, err := db.GetAttachmentByID(ctx, "a1")
	require.NoError(t, err)
	assert.Equal(t, "/new", *got.LocalPath)
}

func TestSaveAttachmentFillsMissingRemoteRef(t *testing.T) {
	db := newAttachmentDB(t)
	ctx := context.Background()
	require.NoError(t, db.SaveAttachment(ctx, &domain.Attachment{
		ID: "a1", MessageID: "msg", MimeType: "image/jpeg", DownloadStatus: domain.MediaRemoteOnly,
	}))
	require.NoError(t, db.SaveAttachment(ctx, &domain.Attachment{
		ID: "a1", MessageID: "msg", MimeType: "image/jpeg", DownloadStatus: domain.MediaRemoteOnly, RemoteRef: "late-ref",
	}))
	got, err := db.GetAttachmentByID(ctx, "a1")
	require.NoError(t, err)
	assert.Equal(t, "late-ref", got.RemoteRef)
	assert.Equal(t, domain.MediaRemoteOnly, got.DownloadStatus)
	assert.Nil(t, got.LocalPath)
}

func TestSaveAttachmentNonCachedStillUpdates(t *testing.T) {
	db := newAttachmentDB(t)
	ctx := context.Background()
	require.NoError(t, db.SaveAttachment(ctx, &domain.Attachment{
		ID: "a1", MessageID: "msg", MimeType: "image/jpeg", DownloadStatus: domain.MediaFailed,
	}))
	require.NoError(t, db.SaveAttachment(ctx, &domain.Attachment{
		ID: "a1", MessageID: "msg", MimeType: "image/jpeg", DownloadStatus: domain.MediaRemoteOnly, FileSize: 5,
	}))
	got, err := db.GetAttachmentByID(ctx, "a1")
	require.NoError(t, err)
	assert.Equal(t, domain.MediaRemoteOnly, got.DownloadStatus)
	assert.Equal(t, int64(5), got.FileSize)
}

func TestGetAttachmentByID(t *testing.T) {
	db := newAttachmentDB(t)
	ctx := context.Background()
	require.NoError(t, db.SaveAttachment(ctx, &domain.Attachment{
		ID: "a1", MessageID: "msg", FileName: "doc.pdf", MimeType: "application/pdf", FileSize: 7,
		DownloadStatus: domain.MediaRemoteOnly, Checksum: "c", RemoteRef: "ref",
	}))
	got, err := db.GetAttachmentByID(ctx, "a1")
	require.NoError(t, err)
	assert.Equal(t, "msg", got.MessageID)
	assert.Equal(t, "doc.pdf", got.FileName)
	assert.Equal(t, "application/pdf", got.MimeType)
	assert.Equal(t, int64(7), got.FileSize)
	assert.Equal(t, "ref", got.RemoteRef)
	assert.Equal(t, "c", got.Checksum)

	_, err = db.GetAttachmentByID(ctx, "nope")
	assert.ErrorIs(t, err, sql.ErrNoRows)
}

func TestUpdateAttachmentDownload(t *testing.T) {
	db := newAttachmentDB(t)
	ctx := context.Background()
	require.NoError(t, db.SaveAttachment(ctx, &domain.Attachment{
		ID: "a1", MessageID: "msg", MimeType: "image/jpeg", FileSize: 10, DownloadStatus: domain.MediaRemoteOnly, RemoteRef: "ref",
	}))
	require.NoError(t, db.UpdateAttachmentDownload(ctx, "a1", domain.MediaDownloading, nil, 0))
	got, _ := db.GetAttachmentByID(ctx, "a1")
	assert.Equal(t, domain.MediaDownloading, got.DownloadStatus)
	assert.Nil(t, got.LocalPath)
	assert.Equal(t, int64(10), got.FileSize, "tamaño 0 conserva el almacenado")

	require.NoError(t, db.UpdateAttachmentDownload(ctx, "a1", domain.MediaCached, strPtr("/x/y"), 42))
	got, _ = db.GetAttachmentByID(ctx, "a1")
	assert.Equal(t, domain.MediaCached, got.DownloadStatus)
	assert.Equal(t, "/x/y", *got.LocalPath)
	assert.Equal(t, int64(42), got.FileSize)
	assert.Equal(t, "ref", got.RemoteRef)
}

func TestGetMessageWithAttachments(t *testing.T) {
	db := newAttachmentDB(t)
	ctx := context.Background()
	require.NoError(t, db.SaveAttachment(ctx, &domain.Attachment{
		ID: "a1", MessageID: "msg", MimeType: "image/jpeg", DownloadStatus: domain.MediaRemoteOnly, RemoteRef: "ref",
	}))
	m, err := db.GetMessageWithAttachments(ctx, "msg")
	require.NoError(t, err)
	assert.Equal(t, "msg", m.ID)
	require.Len(t, m.Attachments, 1)
	assert.Equal(t, "a1", m.Attachments[0].ID)
	assert.Equal(t, "ref", m.Attachments[0].RemoteRef)
}
