package rpc

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cestila/ConduitOmniChat/core/pkg/domain"
	"github.com/cestila/ConduitOmniChat/core/pkg/eventbus"
	"github.com/cestila/ConduitOmniChat/core/pkg/storage"
	conduitv1 "github.com/cestila/ConduitOmniChat/sdk/conduitv1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// newMediaTestServer crea un servidor con base temporal y sin plugins:
// el envío de texto y la persistencia no necesitan un proveedor real.
func newMediaTestServer(t *testing.T) (*Server, *storage.DB) {
	t.Helper()
	db, err := storage.NewDB(filepath.Join(t.TempDir(), "test_conduit.db"))
	if err != nil {
		t.Fatalf("no se pudo crear la base: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewServer(db, eventbus.NewBus(), nil, nil), db
}

// writeTempFile escribe bytes en un archivo temporal con la extensión pedida.
func writeTempFile(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("no se pudo escribir %s: %v", name, err)
	}
	return path
}

// S10: el envío de solo texto mantiene su comportamiento y sigue siendo content_type=text.
func TestSendMessageTextOnlyStaysText(t *testing.T) {
	srv, db := newMediaTestServer(t)
	ctx := context.Background()

	resp, err := srv.SendMessage(ctx, &conduitv1.SendMessageRequest{
		ConversationId: "conv-texto",
		Body:           "hola",
	})
	if err != nil {
		t.Fatalf("envío de texto falló: %v", err)
	}
	if got := resp.GetMessage().GetContentType(); got != string(domain.ContentTypeText) {
		t.Fatalf("content_type = %q, se esperaba %q", got, domain.ContentTypeText)
	}
	if len(resp.GetMessage().GetAttachments()) != 0 {
		t.Fatalf("un mensaje de texto no debería tener adjuntos: %v", resp.GetMessage().GetAttachments())
	}
	if resp.GetMessage().GetBody() != "hola" {
		t.Fatalf("body = %q, se esperaba %q", resp.GetMessage().GetBody(), "hola")
	}
	_ = db
}

// S4 + S5: una imagen local se persiste como adjunto cacheado y deriva content_type=image.
func TestSendMessageWithImageAttachmentPersistsAndDerivesContentType(t *testing.T) {
	srv, db := newMediaTestServer(t)
	ctx := context.Background()

	png := writeTempFile(t, "foto.png", []byte("\x89PNG\r\n\x1a\n"))

	resp, err := srv.SendMessage(ctx, &conduitv1.SendMessageRequest{
		ConversationId: "conv-media",
		Body:           "mirá esto",
		Attachments: []*conduitv1.OutgoingAttachment{
			{FilePath: png},
		},
	})
	if err != nil {
		t.Fatalf("envío con adjunto falló: %v", err)
	}

	msg := resp.GetMessage()
	if msg.GetContentType() != string(domain.ContentTypeImage) {
		t.Fatalf("content_type = %q, se esperaba image", msg.GetContentType())
	}
	if len(msg.GetAttachments()) != 1 {
		t.Fatalf("se esperaba 1 adjunto, hay %d", len(msg.GetAttachments()))
	}
	att := msg.GetAttachments()[0]
	if att.GetMimeType() != "image/png" {
		t.Fatalf("mime_type = %q, se esperaba image/png", att.GetMimeType())
	}
	if att.GetLocalPath() != png {
		t.Fatalf("local_path = %q, se esperaba %q", att.GetLocalPath(), png)
	}
	if att.GetDownloadStatus() != string(domain.MediaCached) {
		t.Fatalf("download_status = %q, se esperaba cached", att.GetDownloadStatus())
	}
	if att.GetFileName() != "foto.png" {
		t.Fatalf("file_name = %q, se esperaba foto.png", att.GetFileName())
	}

	// Se persiste en la base, no solo en la respuesta.
	saved, err := db.GetMessagesByConversation(ctx, "conv-media", 10, "")
	if err != nil {
		t.Fatalf("no se pudieron leer los mensajes: %v", err)
	}
	if len(saved) != 1 || len(saved[0].GetAttachments()) != 1 {
		t.Fatalf("adjunto no persistido: %+v", saved)
	}
}

// S4: un mensaje sin texto pero con adjuntos es válido.
func TestSendMessageAttachmentWithoutBodyIsValid(t *testing.T) {
	srv, _ := newMediaTestServer(t)
	ctx := context.Background()

	audio := writeTempFile(t, "nota.ogg", []byte("OggS\x00\x02"))

	resp, err := srv.SendMessage(ctx, &conduitv1.SendMessageRequest{
		ConversationId: "conv-audio",
		Attachments:    []*conduitv1.OutgoingAttachment{{FilePath: audio}},
	})
	if err != nil {
		t.Fatalf("un adjunto sin texto debería ser válido: %v", err)
	}
	if resp.GetMessage().GetContentType() != string(domain.ContentTypeAudio) {
		t.Fatalf("content_type = %q, se esperaba audio", resp.GetMessage().GetContentType())
	}
}

// S4: sin texto y sin adjuntos se rechaza nombrando lo que falta.
func TestSendMessageWithoutBodyOrAttachmentsIsRejected(t *testing.T) {
	srv, _ := newMediaTestServer(t)

	_, err := srv.SendMessage(context.Background(), &conduitv1.SendMessageRequest{
		ConversationId: "conv-vacia",
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("code = %v, se esperaba InvalidArgument", status.Code(err))
	}
	if err == nil || !contains(err.Error(), "adjunto") {
		t.Fatalf("el error debería nombrar el adjunto faltante: %v", err)
	}
}

// S4: un adjunto local inexistente se rechaza antes de tocar la base.
func TestSendMessageMissingAttachmentFileIsRejected(t *testing.T) {
	srv, db := newMediaTestServer(t)
	ctx := context.Background()

	_, err := srv.SendMessage(ctx, &conduitv1.SendMessageRequest{
		ConversationId: "conv-fantasma",
		Attachments:    []*conduitv1.OutgoingAttachment{{FilePath: "/tmp/no-existe-de-verdad.png"}},
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("code = %v, se esperaba InvalidArgument", status.Code(err))
	}
	if err == nil || !contains(err.Error(), "/tmp/no-existe-de-verdad.png") {
		t.Fatalf("el error debería nombrar el archivo: %v", err)
	}
	if msgs, _ := db.GetMessagesByConversation(ctx, "conv-fantasma", 10, ""); len(msgs) != 0 {
		t.Fatalf("no se debería persistir nada con un adjunto inválido: %+v", msgs)
	}
}

// S5: clasificación de MIME a content_type del dominio.
func TestContentTypeForAttachment(t *testing.T) {
	cases := []struct {
		mime string
		want domain.ContentType
	}{
		{"image/png", domain.ContentTypeImage},
		{"image/gif", domain.ContentTypeImage},
		{"audio/ogg", domain.ContentTypeAudio},
		{"video/mp4", domain.ContentTypeVideo},
		{"application/pdf", domain.ContentTypeDocument},
		{"application/octet-stream", domain.ContentTypeDocument},
	}
	for _, c := range cases {
		if got := contentTypeForAttachment(c.mime); got != c.want {
			t.Errorf("contentTypeForAttachment(%q) = %q, se esperaba %q", c.mime, got, c.want)
		}
	}
}

// contains evita depender de strings en el test por claridad de lectura.
func contains(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}
