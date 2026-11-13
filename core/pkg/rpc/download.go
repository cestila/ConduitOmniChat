package rpc

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"os"
	"sync"
	"time"

	"github.com/cestila/ConduitOmniChat/core/pkg/domain"
	"github.com/cestila/ConduitOmniChat/core/pkg/eventbus"
	"github.com/cestila/ConduitOmniChat/core/pkg/pluginmgr"
	"github.com/cestila/ConduitOmniChat/core/pkg/storage"
	conduitv1 "github.com/cestila/ConduitOmniChat/sdk/conduitv1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// downloadTimeout acota cada llamada DownloadMedia al plugin.
const downloadTimeout = 5 * time.Minute

// Downloader es el motor de descargas de adjuntos a demanda. Lo comparten el RPC
// DownloadAttachment y la autodescarga del daemon, de modo que los pedidos simultáneos
// del mismo adjunto producen una sola descarga.
type Downloader struct {
	db      *storage.DB
	bus     *eventbus.Bus
	plugins *pluginmgr.Manager

	mu       sync.Mutex
	inflight map[string]chan struct{} // id de adjunto -> canal que se cierra al terminar
}

// NewDownloader crea un motor de descargas.
func NewDownloader(db *storage.DB, bus *eventbus.Bus, plugins *pluginmgr.Manager) *Downloader {
	return &Downloader{db: db, bus: bus, plugins: plugins, inflight: make(map[string]chan struct{})}
}

var closedChan = func() chan struct{} { c := make(chan struct{}); close(c); return c }()

// Start inicia (o se une a) la descarga del adjunto. Devuelve el adjunto tal como quedó al
// responder y un canal que se cierra cuando la descarga termina. Los errores son status gRPC.
func (d *Downloader) Start(ctx context.Context, id string) (*domain.Attachment, <-chan struct{}, error) {
	att, err := d.db.GetAttachmentByID(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, status.Error(codes.NotFound, "adjunto no encontrado")
	}
	if err != nil {
		return nil, nil, status.Errorf(codes.Internal, "error leyendo adjunto: %v", err)
	}

	if att.DownloadStatus == domain.MediaCached && att.LocalPath != nil {
		if _, statErr := os.Stat(*att.LocalPath); statErr == nil {
			return att, closedChan, nil
		}
	}
	if att.RemoteRef == "" {
		return nil, nil, status.Error(codes.FailedPrecondition, "el adjunto no tiene referencia remota para descargar")
	}

	d.mu.Lock()
	if done, ok := d.inflight[id]; ok {
		d.mu.Unlock()
		return att, done, nil
	}
	done := make(chan struct{})
	d.inflight[id] = done
	d.mu.Unlock()

	if err := d.db.UpdateAttachmentDownload(ctx, id, domain.MediaDownloading, nil, 0); err != nil {
		d.finish(id, done)
		return nil, nil, status.Errorf(codes.Internal, "error actualizando adjunto: %v", err)
	}
	att.DownloadStatus = domain.MediaDownloading
	att.LocalPath = nil
	d.publish(att.MessageID)

	go d.run(*att, done)
	return att, done, nil
}

// finish libera el registro de descarga en curso y despierta a los que esperan.
func (d *Downloader) finish(id string, done chan struct{}) {
	d.mu.Lock()
	delete(d.inflight, id)
	d.mu.Unlock()
	close(done)
}

// run ejecuta la descarga en el plugin del proveedor y persiste el resultado.
func (d *Downloader) run(att domain.Attachment, done chan struct{}) {
	defer d.finish(att.ID, done)
	// Contexto propio: la descarga sobrevive al pedido que la inició.
	ctx := context.Background()

	localPath, size, ok := d.fetch(ctx, att)
	finalStatus := domain.MediaFailed
	var pathPtr *string
	if ok {
		finalStatus, pathPtr = domain.MediaCached, &localPath
	}
	if err := d.db.UpdateAttachmentDownload(ctx, att.ID, finalStatus, pathPtr, size); err != nil {
		log.Printf("download: error guardando resultado de %s: %v", att.ID, err)
	}
	d.publish(att.MessageID)
}

// fetch resuelve mensaje -> canal -> plugin y pide la descarga.
func (d *Downloader) fetch(ctx context.Context, att domain.Attachment) (string, int64, bool) {
	msg, err := d.db.FindMessageByID(ctx, att.MessageID)
	if err != nil {
		log.Printf("download: mensaje de %s no disponible: %v", att.ID, err)
		return "", 0, false
	}
	ch, err := d.db.GetChannelByID(ctx, msg.ChannelID)
	if err != nil || ch == nil {
		log.Printf("download: canal de %s no disponible: %v", att.ID, err)
		return "", 0, false
	}
	inst, found := d.plugins.GetInstance(providerName(ch.Provider))
	if !found || inst == nil || inst.RPC == nil {
		log.Printf("download: plugin %s no conectado para %s", ch.Provider, att.ID)
		return "", 0, false
	}

	callCtx, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()
	resp, err := inst.RPC.DownloadMedia(callCtx, &conduitv1.PluginDownloadMediaRequest{
		RemoteRef:    att.RemoteRef,
		FileName:     att.FileName,
		MimeType:     att.MimeType,
		AttachmentId: att.ID,
	})
	if err != nil || resp == nil || !resp.Success || resp.LocalPath == "" {
		reason := ""
		if resp != nil {
			reason = resp.ErrorMessage
		}
		log.Printf("download: fallo bajando %s: err=%v plugin=%q", att.ID, err, reason)
		return "", 0, false
	}
	return resp.LocalPath, resp.FileSize, true
}

// publish emite message_updated con el mensaje completo (adjuntos incluidos).
func (d *Downloader) publish(messageID string) {
	msg, err := d.db.GetMessageWithAttachments(context.Background(), messageID)
	if err != nil {
		log.Printf("download: no se pudo cargar el mensaje %s: %v", messageID, err)
		return
	}
	d.bus.Publish(eventbus.EventMessageUpdated, msg)
}

// DownloadAttachment descarga un adjunto a demanda. Con wait=false responde apenas el adjunto
// pasa a 'downloading'; con wait=true espera al estado final.
func (s *Server) DownloadAttachment(ctx context.Context, req *conduitv1.DownloadAttachmentRequest) (*conduitv1.DownloadAttachmentResponse, error) {
	if req.GetAttachmentId() == "" {
		return nil, status.Error(codes.InvalidArgument, "attachment_id es obligatorio")
	}
	att, done, err := s.downloader.Start(ctx, req.AttachmentId)
	if err != nil {
		return nil, err
	}
	if req.Wait {
		select {
		case <-done:
		case <-ctx.Done():
			return nil, status.FromContextError(ctx.Err()).Err()
		}
		if att, err = s.db.GetAttachmentByID(ctx, req.AttachmentId); err != nil {
			return nil, status.Errorf(codes.Internal, "error leyendo adjunto: %v", err)
		}
	}
	return &conduitv1.DownloadAttachmentResponse{Attachment: attachmentToProto(att)}, nil
}

// attachmentToProto convierte un adjunto de dominio sin exponer la remote_ref.
func attachmentToProto(a *domain.Attachment) *conduitv1.ProtoAttachment {
	locPath := ""
	if a.LocalPath != nil {
		locPath = *a.LocalPath
	}
	return &conduitv1.ProtoAttachment{
		Id:             a.ID,
		MessageId:      a.MessageID,
		FileName:       a.FileName,
		MimeType:       a.MimeType,
		FileSize:       a.FileSize,
		LocalPath:      locPath,
		DownloadStatus: string(a.DownloadStatus),
		Checksum:       a.Checksum,
	}
}
