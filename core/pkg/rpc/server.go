package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cestila/ConduitOmniChat/core/pkg/domain"
	"github.com/cestila/ConduitOmniChat/core/pkg/eventbus"
	"github.com/cestila/ConduitOmniChat/core/pkg/identity"
	"github.com/cestila/ConduitOmniChat/core/pkg/plugininstall"
	"github.com/cestila/ConduitOmniChat/core/pkg/pluginmgr"
	"github.com/cestila/ConduitOmniChat/core/pkg/storage"
	conduitv1 "github.com/cestila/ConduitOmniChat/sdk/conduitv1"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// resolvedAttachment es un adjunto local ya validado, listo para persistir y enviar.
type resolvedAttachment struct {
	path     string
	name     string
	mimeType string
	size     int64
}

// resolveOutgoingAttachments valida los archivos locales que envía el cliente.
// Falla rápido si un archivo no existe: es un error del cliente, no del proveedor.
func resolveOutgoingAttachments(atts []*conduitv1.OutgoingAttachment) ([]resolvedAttachment, error) {
	resolved := make([]resolvedAttachment, 0, len(atts))
	for _, a := range atts {
		path := strings.TrimSpace(a.GetFilePath())
		if path == "" {
			return nil, status.Error(codes.InvalidArgument, "cada adjunto necesita file_path")
		}
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			return nil, status.Errorf(codes.InvalidArgument, "adjunto inexistente o inválido: %s", path)
		}
		name := strings.TrimSpace(a.GetFileName())
		if name == "" {
			name = filepath.Base(path)
		}
		mimeType := strings.TrimSpace(a.GetMimeType())
		if mimeType == "" {
			mimeType = detectMimeType(path)
		}
		size := a.GetFileSize()
		if size <= 0 {
			size = info.Size()
		}
		resolved = append(resolved, resolvedAttachment{path: path, name: name, mimeType: mimeType, size: size})
	}
	return resolved, nil
}

// detectMimeType infiere el MIME por extensión y, si no alcanza, por los primeros bytes.
func detectMimeType(path string) string {
	if byExt := mime.TypeByExtension(strings.ToLower(filepath.Ext(path))); byExt != "" {
		return stripMimeParams(byExt)
	}
	f, err := os.Open(path)
	if err != nil {
		return "application/octet-stream"
	}
	defer f.Close()
	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	if n == 0 {
		return "application/octet-stream"
	}
	return stripMimeParams(http.DetectContentType(buf[:n]))
}

// stripMimeParams quita parámetros como "; charset=utf-8" para clasificar por prefijo.
func stripMimeParams(mimeType string) string {
	if i := strings.Index(mimeType, ";"); i > 0 {
		return strings.TrimSpace(mimeType[:i])
	}
	return mimeType
}

// contentTypeForAttachment clasifica un MIME en el ContentType del dominio.
func contentTypeForAttachment(mimeType string) domain.ContentType {
	switch {
	case strings.HasPrefix(mimeType, "image/"):
		return domain.ContentTypeImage
	case strings.HasPrefix(mimeType, "audio/"):
		return domain.ContentTypeAudio
	case strings.HasPrefix(mimeType, "video/"):
		return domain.ContentTypeVideo
	default:
		return domain.ContentTypeDocument
	}
}

// outgoingProtoAttachments traduce los adjuntos resueltos al contrato del plugin.
func outgoingProtoAttachments(atts []resolvedAttachment) []*conduitv1.OutgoingAttachment {
	out := make([]*conduitv1.OutgoingAttachment, 0, len(atts))
	for _, a := range atts {
		out = append(out, &conduitv1.OutgoingAttachment{
			FilePath: a.path,
			FileName: a.name,
			MimeType: a.mimeType,
			FileSize: a.size,
		})
	}
	return out
}

// Server implementa la interfaz gRPC ConduitServiceServer.
type Server struct {
	conduitv1.UnimplementedConduitServiceServer
	db        *storage.DB
	bus       *eventbus.Bus
	graph     *identity.Graph
	plugins   *pluginmgr.Manager
	startTime time.Time

	// downloader es el motor de descargas de adjuntos a demanda (compartido con la autodescarga).
	downloader *Downloader

	historyMu       sync.Mutex
	historyCooldown map[string]time.Time

	// installer y loader permiten instalar plugins desde GitHub Releases y cargarlos en caliente.
	installer PluginInstaller
	loader    PluginLoader
}

// PluginInstaller descarga e instala plugins (lo implementa plugininstall.Installer).
type PluginInstaller interface {
	Install(ctx context.Context, spec, version string) (plugininstall.Result, error)
}

// PluginLoader lanza un plugin recién instalado sin reiniciar el daemon.
type PluginLoader func(ctx context.Context, path string) error

// SetPluginInstaller inyecta el instalador usado por InstallPlugin.
func (s *Server) SetPluginInstaller(i PluginInstaller) { s.installer = i }

// SetPluginLoader inyecta el callback que carga en caliente un plugin instalado.
func (s *Server) SetPluginLoader(l PluginLoader) { s.loader = l }

// NewServer inicializa una nueva instancia del servidor gRPC.
func NewServer(db *storage.DB, bus *eventbus.Bus, graph *identity.Graph, plugins *pluginmgr.Manager) *Server {
	return &Server{
		db:         db,
		bus:        bus,
		graph:      graph,
		plugins:    plugins,
		startTime:  time.Now(),
		downloader: NewDownloader(db, bus, plugins),
	}
}

// SetDownloader reemplaza el motor de descargas, para compartir la misma instancia con la
// autodescarga que corre antes de crear el servidor gRPC.
func (s *Server) SetDownloader(d *Downloader) {
	if d != nil {
		s.downloader = d
	}
}

// SendMessage almacena y enruta un mensaje de salida.
func (s *Server) SendMessage(ctx context.Context, req *conduitv1.SendMessageRequest) (*conduitv1.SendMessageResponse, error) {
	if req.ConversationId == "" {
		return nil, status.Error(codes.InvalidArgument, "conversation_id es obligatorio")
	}

	attachments, err := resolveOutgoingAttachments(req.GetAttachments())
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Body) == "" && len(attachments) == 0 {
		return nil, status.Error(codes.InvalidArgument, "el mensaje necesita texto o al menos un adjunto")
	}

	contentType := domain.ContentTypeText
	if len(attachments) > 0 {
		contentType = contentTypeForAttachment(attachments[0].mimeType)
	}

	msgID := uuid.New().String()
	now := time.Now()

	// Asegurar que exista la conversación (si el usuario abrió chat desde la lista de contactos)
	conv, _ := s.db.GetConversationByID(ctx, req.ConversationId)
	if conv == nil {
		// Verificar si el ConversationId corresponde a un Contacto existente
		contact, err := s.db.GetContactByID(ctx, req.ConversationId)
		if err == nil && contact != nil {
			var activeChanID *string
			if len(contact.Channels) > 0 {
				activeChanID = &contact.Channels[0].ID
			}
			_ = s.db.EnsureConversation(ctx, &domain.Conversation{
				ID:              contact.ID,
				ContactID:       &contact.ID,
				IsGroup:         false,
				Title:           contact.DisplayName,
				LastMessageAt:   now,
				ActiveChannelID: activeChanID,
			})
		} else {
			// Conversación directa nueva
			_ = s.db.EnsureConversation(ctx, &domain.Conversation{
				ID:            req.ConversationId,
				Title:         req.ConversationId,
				LastMessageAt: now,
			})
		}
		conv, _ = s.db.GetConversationByID(ctx, req.ConversationId)
	}

	channelID := req.TargetChannelId
	var targetChannel *domain.ContactChannel
	if channelID != "" {
		targetChannel, _ = s.db.GetChannelByID(ctx, channelID)
		if targetChannel == nil {
			return nil, status.Errorf(codes.InvalidArgument, "canal no encontrado: %s", channelID)
		}
		if conv != nil && conv.ContactId != "" && targetChannel.ContactID != "" && targetChannel.ContactID != conv.ContactId {
			return nil, status.Errorf(codes.InvalidArgument, "el canal %s no pertenece al contacto de la conversación", channelID)
		}
	} else {
		// Obtener canal activo de la conversación o canal preferido del contacto
		if conv != nil {
			if conv.ActiveChannelId != "" {
				channelID = conv.ActiveChannelId
				targetChannel, _ = s.db.GetChannelByID(ctx, channelID)
			}
			if targetChannel == nil && conv.ContactId != "" {
				channels, _ := s.db.GetChannelsByContactID(ctx, conv.ContactId)
				for _, ch := range channels {
					if ch.IsPreferred {
						targetChannel = &ch
						channelID = ch.ID
						break
					}
				}
				if targetChannel == nil && len(channels) > 0 {
					targetChannel = &channels[0]
					channelID = targetChannel.ID
				}
			}
		}
	}

	// Si aún no tenemos canal válido pero hay un contacto asociado, asegurar canal de WhatsApp
	if targetChannel == nil {
		channels, _ := s.db.GetChannelsByContactID(ctx, req.ConversationId)
		if len(channels) > 0 {
			targetChannel = &channels[0]
			channelID = targetChannel.ID
		}
	}

	// Fallback si la conversación no tiene channel_id vinculado todavía
	if channelID == "" {
		if targetChannel != nil {
			channelID = targetChannel.ID
		} else {
			// Crear un canal por defecto para evitar violación de FK
			fallbackChan := &domain.ContactChannel{
				ID:          uuid.New().String(),
				ContactID:   req.ConversationId,
				Provider:    domain.ProviderWhatsApp,
				RemoteID:    req.ConversationId,
				Handle:      req.ConversationId,
				IsPreferred: true,
			}
			_ = s.db.SaveChannel(ctx, fallbackChan)
			channelID = fallbackChan.ID
			targetChannel = fallbackChan
		}
	}

	domainMsg := &domain.Message{
		ID:              msgID,
		ConversationID:  req.ConversationId,
		ChannelID:       channelID,
		RemoteMessageID: fmt.Sprintf("out-%s", msgID),
		SenderType:      domain.SenderMe,
		Body:            req.Body,
		ContentType:     contentType,
		Status:          domain.StatusPending,
		SentAt:          now,
	}

	replyToRemoteID := ""
	if req.ReplyToMessageId != "" {
		if parentMsg, err := s.db.FindMessageByID(ctx, req.ReplyToMessageId); err == nil && parentMsg != nil {
			domainMsg.ReplyToMessageID = &parentMsg.ID
			domainMsg.ReplyToSender = string(parentMsg.SenderType)
			domainMsg.ReplyToBody = parentMsg.Body
			replyToRemoteID = parentMsg.RemoteMessageID
		} else {
			replyToRemoteID = req.ReplyToMessageId
			if pID, err := s.db.FindMessageIDByRemoteID(ctx, channelID, req.ReplyToMessageId); err == nil && pID != "" {
				domainMsg.ReplyToMessageID = &pID
				if pMsg, err := s.db.FindMessageByID(ctx, pID); err == nil && pMsg != nil {
					domainMsg.ReplyToSender = string(pMsg.SenderType)
					domainMsg.ReplyToBody = pMsg.Body
				}
			}
		}
	}

	// Los adjuntos salientes ya viven en disco: se persisten como cacheados junto al mensaje.
	for _, a := range attachments {
		localPath := a.path
		domainMsg.Attachments = append(domainMsg.Attachments, domain.Attachment{
			ID:             uuid.New().String(),
			MessageID:      msgID,
			FileName:       a.name,
			MimeType:       a.mimeType,
			FileSize:       a.size,
			LocalPath:      &localPath,
			DownloadStatus: domain.MediaCached,
		})
	}

	// Si hay plugin activo para este canal, despachar el mensaje real
	if targetChannel != nil && s.plugins != nil {
		pluginName := string(targetChannel.Provider)
		if inst, ok := s.plugins.GetInstance(pluginName); ok {
			go func() {
				// Subir media puede tardar mucho más que enviar texto.
				timeout := 10 * time.Second
				if len(attachments) > 0 {
					timeout = 2 * time.Minute
				}
				outCtx, cancel := context.WithTimeout(context.Background(), timeout)
				defer cancel()
				resp, err := inst.RPC.SendOutgoingMessage(outCtx, &conduitv1.OutgoingMessageRequest{
					RemoteRecipientId: targetChannel.RemoteID,
					Body:              req.Body,
					ReplyToRemoteId:   replyToRemoteID,
					Attachments:       outgoingProtoAttachments(attachments),
					ContentType:       string(contentType),
				})
				if err != nil {
					fmt.Printf("⚠️ Error enviando mensaje a plugin %s: %v\n", pluginName, err)
					domainMsg.Status = domain.StatusFailed
					_ = s.db.SaveMessage(context.Background(), domainMsg)
					s.bus.Publish(eventbus.EventMessageStatus, domainMsg)
				} else if !resp.Success {
					fmt.Printf("⚠️ Plugin %s rechazó el envío de mensaje: %s\n", pluginName, resp.ErrorMessage)
					domainMsg.Status = domain.StatusFailed
					_ = s.db.SaveMessage(context.Background(), domainMsg)
					s.bus.Publish(eventbus.EventMessageStatus, domainMsg)
				} else {
					domainMsg.Status = domain.StatusSent
					domainMsg.RemoteMessageID = resp.RemoteMessageId
					_ = s.db.SaveMessage(context.Background(), domainMsg)
					s.bus.Publish(eventbus.EventMessageStatus, domainMsg)
				}
			}()
		} else {
			fmt.Printf("⚠️ No se encontró plugin instanciado para '%s'\n", pluginName)
		}
	}

	if err := s.db.SaveMessage(ctx, domainMsg); err != nil {
		return nil, status.Errorf(codes.Internal, "error al guardar mensaje: %v", err)
	}

	// El canal usado para enviar pasa a ser la última cuenta usada en chats 1 a 1.
	if conv != nil && !conv.IsGroup && channelID != "" {
		_ = s.db.SetConversationActiveChannel(ctx, req.ConversationId, channelID)
	}

	// Publicar evento en el bus
	s.bus.Publish(eventbus.EventMessageSent, domainMsg)

	return &conduitv1.SendMessageResponse{
		Message: domainMsgToProto(domainMsg),
	}, nil
}

func domainMsgToProto(msg *domain.Message) *conduitv1.ProtoMessage {
	if msg == nil {
		return nil
	}
	var protoAtts []*conduitv1.ProtoAttachment
	for _, a := range msg.Attachments {
		locPath := ""
		if a.LocalPath != nil {
			locPath = *a.LocalPath
		}
		protoAtts = append(protoAtts, &conduitv1.ProtoAttachment{
			Id:             a.ID,
			MessageId:      a.MessageID,
			FileName:       a.FileName,
			MimeType:       a.MimeType,
			FileSize:       a.FileSize,
			LocalPath:      locPath,
			DownloadStatus: string(a.DownloadStatus),
			Checksum:       a.Checksum,
		})
	}
	replyToID := ""
	if msg.ReplyToMessageID != nil {
		replyToID = *msg.ReplyToMessageID
	}
	var protoReactions []*conduitv1.ProtoReaction
	for _, r := range msg.Reactions {
		protoReactions = append(protoReactions, &conduitv1.ProtoReaction{
			Emoji:      r.Emoji,
			Count:      int32(r.Count),
			Senders:    r.Senders,
			IncludesMe: r.IncludesMe,
		})
	}
	var protoPoll *conduitv1.ProtoPoll
	if msg.Poll != nil {
		protoPoll = storage.PollDomainToProto(msg.Poll)
	}
	var protoButtonRows []*conduitv1.ProtoButtonRow
	if len(msg.ButtonRows) > 0 {
		protoButtonRows = storage.ButtonRowsDomainToProto(msg.ButtonRows)
	}
	var protoLoc *conduitv1.ProtoLocation
	if msg.Location != nil {
		protoLoc = &conduitv1.ProtoLocation{
			Latitude:  msg.Location.Latitude,
			Longitude: msg.Location.Longitude,
			Title:     msg.Location.Title,
			Address:   msg.Location.Address,
		}
	}
	var protoContact *conduitv1.ProtoSharedContact
	if msg.SharedContact != nil {
		protoContact = &conduitv1.ProtoSharedContact{
			DisplayName: msg.SharedContact.DisplayName,
			PhoneNumber: msg.SharedContact.PhoneNumber,
			Vcard:       msg.SharedContact.Vcard,
		}
	}
	var editedAtUnix int64
	if msg.EditedAt != nil {
		editedAtUnix = msg.EditedAt.Unix()
	}
	return &conduitv1.ProtoMessage{
		Id:               msg.ID,
		ConversationId:   msg.ConversationID,
		ChannelId:        msg.ChannelID,
		RemoteMessageId:  msg.RemoteMessageID,
		SenderType:       string(msg.SenderType),
		Body:             msg.Body,
		ContentType:      string(msg.ContentType),
		Status:           string(msg.Status),
		SentAtUnix:       msg.SentAt.Unix(),
		Attachments:      protoAtts,
		ReplyToMessageId: replyToID,
		ReplyToSender:    msg.ReplyToSender,
		ReplyToBody:      msg.ReplyToBody,
		Reactions:        protoReactions,
		Poll:             protoPoll,
		ButtonRows:       protoButtonRows,
		IsEdited:         msg.IsEdited,
		EditedAtUnix:     editedAtUnix,
		IsPinned:         msg.IsPinned,
		Location:         protoLoc,
		SharedContact:    protoContact,
	}
}

// StreamEvents emite un flujo reactivo de eventos hacia los clientes conectados.
func (s *Server) StreamEvents(req *conduitv1.StreamEventsRequest, stream conduitv1.ConduitService_StreamEventsServer) error {
	subReceived := s.bus.Subscribe(eventbus.EventMessageReceived)
	subSent := s.bus.Subscribe(eventbus.EventMessageSent)
	subUpdated := s.bus.Subscribe(eventbus.EventMessageUpdated)
	subDeleted := s.bus.Subscribe(eventbus.EventMessageDeleted)
	subRead := s.bus.Subscribe(eventbus.EventConversationRead)
	defer s.bus.Unsubscribe(eventbus.EventMessageReceived, subReceived)
	defer s.bus.Unsubscribe(eventbus.EventMessageSent, subSent)
	defer s.bus.Unsubscribe(eventbus.EventMessageUpdated, subUpdated)
	defer s.bus.Unsubscribe(eventbus.EventMessageDeleted, subDeleted)
	defer s.bus.Unsubscribe(eventbus.EventConversationRead, subRead)

	for {
		select {
		case <-stream.Context().Done():
			return nil
		case ev := <-subReceived:
			if msg, ok := ev.Payload.(*domain.Message); ok {
				err := stream.Send(&conduitv1.EventEnvelope{
					Type:          string(ev.Type),
					TimestampUnix: ev.Timestamp.Unix(),
					Payload: &conduitv1.EventEnvelope_MessageReceived{
						MessageReceived: domainMsgToProto(msg),
					},
				})
				if err != nil {
					return err
				}
			}
		case ev := <-subSent:
			if msg, ok := ev.Payload.(*domain.Message); ok {
				err := stream.Send(&conduitv1.EventEnvelope{
					Type:          string(ev.Type),
					TimestampUnix: ev.Timestamp.Unix(),
					Payload: &conduitv1.EventEnvelope_MessageSent{
						MessageSent: domainMsgToProto(msg),
					},
				})
				if err != nil {
					return err
				}
			}
		case ev := <-subUpdated:
			if msg, ok := ev.Payload.(*domain.Message); ok {
				err := stream.Send(&conduitv1.EventEnvelope{
					Type:          string(ev.Type),
					TimestampUnix: ev.Timestamp.Unix(),
					Payload: &conduitv1.EventEnvelope_MessageUpdated{
						MessageUpdated: domainMsgToProto(msg),
					},
				})
				if err != nil {
					return err
				}
			}
		case ev := <-subDeleted:
			if msgID, ok := ev.Payload.(string); ok {
				err := stream.Send(&conduitv1.EventEnvelope{
					Type:          string(ev.Type),
					TimestampUnix: ev.Timestamp.Unix(),
					Payload: &conduitv1.EventEnvelope_MessageDeleted{
						MessageDeleted: msgID,
					},
				})
				if err != nil {
					return err
				}
			}
		case ev := <-subRead:
			if readEv, ok := ev.Payload.(*conduitv1.ConversationReadEvent); ok {
				err := stream.Send(&conduitv1.EventEnvelope{
					Type:          string(ev.Type),
					TimestampUnix: ev.Timestamp.Unix(),
					Payload: &conduitv1.EventEnvelope_ConversationRead{
						ConversationRead: readEv,
					},
				})
				if err != nil {
					return err
				}
			}
		}
	}
}

// ListConversations retorna conversaciones reales guardadas en SQLite.
func (s *Server) ListConversations(ctx context.Context, req *conduitv1.ListConversationsRequest) (*conduitv1.ListConversationsResponse, error) {
	convs, err := s.db.ListConversations(ctx, int(req.Limit), int(req.Offset))
	if err != nil {
		return nil, status.Errorf(codes.Internal, "error listando conversaciones: %v", err)
	}
	return &conduitv1.ListConversationsResponse{Conversations: convs}, nil
}

// GetMessages retorna el historial real de mensajes de una conversación.
func (s *Server) GetMessages(ctx context.Context, req *conduitv1.GetMessagesRequest) (*conduitv1.GetMessagesResponse, error) {
	msgs, err := s.db.GetMessagesByConversation(ctx, req.ConversationId, int(req.Limit), req.BeforeMessageId)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "error obteniendo mensajes: %v", err)
	}

	// La entrega del historial es asíncrona: el plugin emite los mensajes como
	// eventos y la GUI reintenta la lectura.
	//
	// - Con cursor y página incompleta: se pide la página anterior al proveedor.
	// - Sin cursor y sin mensajes locales: es la primera carga de ese chat, así
	//   que se pide la última página directamente.
	if req.BeforeMessageId != "" && len(msgs) < int(req.Limit) {
		go s.requestHistory(req.ConversationId, req.BeforeMessageId)
	} else if req.BeforeMessageId == "" && len(msgs) == 0 {
		go s.requestHistory(req.ConversationId, "")
	}

	return &conduitv1.GetMessagesResponse{Messages: msgs}, nil
}

// GetPinnedMessages retorna los mensajes fijados de una conversación.
func (s *Server) GetPinnedMessages(ctx context.Context, req *conduitv1.GetPinnedMessagesRequest) (*conduitv1.GetPinnedMessagesResponse, error) {
	if strings.TrimSpace(req.ConversationId) == "" {
		return nil, status.Error(codes.InvalidArgument, "conversation_id es obligatorio")
	}
	msgs, err := s.db.GetPinnedMessagesByConversation(ctx, req.ConversationId)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "error obteniendo mensajes fijados: %v", err)
	}
	return &conduitv1.GetPinnedMessagesResponse{Messages: msgs}, nil
}

// SearchMessages realiza búsqueda de texto completo con FTS5.
func (s *Server) SearchMessages(ctx context.Context, req *conduitv1.SearchMessagesRequest) (*conduitv1.SearchMessagesResponse, error) {
	limit := int(req.Limit)
	if limit <= 0 {
		limit = 20
	}

	results, err := s.db.SearchMessages(ctx, req.Query, limit)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "error buscando mensajes: %v", err)
	}

	var protoMsgs []*conduitv1.ProtoMessage
	for _, m := range results {
		protoMsgs = append(protoMsgs, &conduitv1.ProtoMessage{
			Id:              m.ID,
			ConversationId:  m.ConversationID,
			ChannelId:       m.ChannelID,
			RemoteMessageId: m.RemoteMessageID,
			SenderType:      string(m.SenderType),
			Body:            m.Body,
			ContentType:     string(m.ContentType),
			Status:          string(m.Status),
			SentAtUnix:      m.SentAt.Unix(),
		})
	}

	return &conduitv1.SearchMessagesResponse{Messages: protoMsgs}, nil
}

// ListContacts devuelve todos los contactos unificados.
func (s *Server) ListContacts(ctx context.Context, req *conduitv1.ListContactsRequest) (*conduitv1.ListContactsResponse, error) {
	contacts, err := s.db.ListContacts(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "error listando contactos: %v", err)
	}

	protoContacts := make([]*conduitv1.ProtoContact, 0, len(contacts))
	for i := range contacts {
		protoContacts = append(protoContacts, protoContactFromDomain(&contacts[i]))
	}

	return &conduitv1.ListContactsResponse{Contacts: protoContacts}, nil
}

// protoChannelsFromDomain convierte los canales de un contacto al contrato gRPC.
func protoChannelsFromDomain(channels []domain.ContactChannel) []*conduitv1.ProtoChannel {
	protoChannels := make([]*conduitv1.ProtoChannel, 0, len(channels))
	for _, ch := range channels {
		protoChannels = append(protoChannels, &conduitv1.ProtoChannel{
			Id:              ch.ID,
			ContactId:       ch.ContactID,
			Provider:        conduitv1.Provider(conduitv1.Provider_value["PROVIDER_"+strings.ToUpper(string(ch.Provider))]),
			RemoteId:        ch.RemoteID,
			Handle:          ch.Handle,
			IsPreferred:     ch.IsPreferred,
			ServerId:        ch.ServerID,
			ParentChannelId: ch.ParentChannelID,
			ChannelType:     ch.ChannelType,
		})
	}
	return protoChannels
}

// protoContactFromDomain convierte un contacto unificado (con identidad canónica) al contrato gRPC.
func protoContactFromDomain(c *domain.Contact) *conduitv1.ProtoContact {
	return &conduitv1.ProtoContact{
		Id:          c.ID,
		DisplayName: c.DisplayName,
		AvatarUrl:   c.AvatarURL,
		Email:       c.Email,
		Phone:       c.Phone,
		Channels:    protoChannelsFromDomain(c.Channels),
	}
}

// MergeContacts une dos contactos en uno solo migrando sus canales.
func (s *Server) MergeContacts(ctx context.Context, req *conduitv1.MergeContactsRequest) (*conduitv1.MergeContactsResponse, error) {
	if req.PrimaryContactId == "" || req.SecondaryContactId == "" {
		return nil, status.Error(codes.InvalidArgument, "ambos IDs de contacto son obligatorios")
	}

	merged, err := s.graph.MergeContacts(ctx, req.PrimaryContactId, req.SecondaryContactId)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "error fusionando contactos: %v", err)
	}

	return &conduitv1.MergeContactsResponse{
		MergedContact: protoContactFromDomain(merged),
	}, nil
}

// SetContactIdentity asigna email y teléfono canónicos a un contacto.
func (s *Server) SetContactIdentity(ctx context.Context, req *conduitv1.SetContactIdentityRequest) (*conduitv1.SetContactIdentityResponse, error) {
	if req.ContactId == "" {
		return nil, status.Error(codes.InvalidArgument, "contact_id es obligatorio")
	}

	contact, err := s.graph.SetContactIdentity(ctx, req.ContactId, req.Email, req.Phone)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "error asignando identidad: %v", err)
	}

	return &conduitv1.SetContactIdentityResponse{
		Contact: protoContactFromDomain(contact),
	}, nil
}

// FindContactsByIdentity busca contactos por email, teléfono o identificador de canal.
func (s *Server) FindContactsByIdentity(ctx context.Context, req *conduitv1.FindContactsByIdentityRequest) (*conduitv1.FindContactsByIdentityResponse, error) {
	if strings.TrimSpace(req.Query) == "" {
		return nil, status.Error(codes.InvalidArgument, "query es obligatorio")
	}

	contacts, err := s.graph.FindContactsByIdentity(ctx, req.Query)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "error buscando contactos por identidad: %v", err)
	}

	protoContacts := make([]*conduitv1.ProtoContact, 0, len(contacts))
	for i := range contacts {
		protoContacts = append(protoContacts, protoContactFromDomain(&contacts[i]))
	}

	return &conduitv1.FindContactsByIdentityResponse{Contacts: protoContacts}, nil
}

// SplitChannel desacopla un canal hacia un nuevo contacto individual.
func (s *Server) SplitChannel(ctx context.Context, req *conduitv1.SplitChannelRequest) (*conduitv1.SplitChannelResponse, error) {
	if req.ChannelId == "" || req.NewContactName == "" {
		return nil, status.Error(codes.InvalidArgument, "channel_id y new_contact_name son obligatorios")
	}

	newContact, err := s.graph.SplitChannel(ctx, req.ChannelId, req.NewContactName)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "error desacoplando canal: %v", err)
	}

	return &conduitv1.SplitChannelResponse{
		NewContact: protoContactFromDomain(newContact),
	}, nil
}

// ListAccounts lista todas las cuentas configuradas.
func (s *Server) ListAccounts(ctx context.Context, req *conduitv1.ListAccountsRequest) (*conduitv1.ListAccountsResponse, error) {
	accounts, err := s.db.ListAccounts(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "error listando cuentas: %v", err)
	}
	return &conduitv1.ListAccountsResponse{Accounts: accounts}, nil
}

// AddAccount agrega una nueva cuenta de cualquier plataforma.
func (s *Server) AddAccount(ctx context.Context, req *conduitv1.AddAccountRequest) (*conduitv1.AddAccountResponse, error) {
	creds := req.GetCredentials()
	conduitv1.NormalizeCredentials(creds)
	acc := &conduitv1.ProtoAccount{
		Id:          uuid.New().String(),
		Provider:    req.Provider,
		Label:       req.Label,
		Identifier:  req.Identifier,
		Status:      "disconnected",
		Credentials: creds,
	}
	if err := s.db.SaveAccount(ctx, acc); err != nil {
		return nil, status.Errorf(codes.Internal, "error guardando cuenta: %v", err)
	}
	return &conduitv1.AddAccountResponse{Account: acc}, nil
}

// RemoveAccount elimina una cuenta registrada.
func (s *Server) RemoveAccount(ctx context.Context, req *conduitv1.RemoveAccountRequest) (*conduitv1.RemoveAccountResponse, error) {
	if err := s.db.DeleteAccount(ctx, req.AccountId); err != nil {
		return nil, status.Errorf(codes.Internal, "error eliminando cuenta: %v", err)
	}
	return &conduitv1.RemoveAccountResponse{Success: true}, nil
}

// normalizeGRPCError extrae el mensaje legible de un error gRPC para mostrarlo en la UI.
func normalizeGRPCError(err error) string {
	if st, ok := status.FromError(err); ok {
		return st.Message()
	}
	return err.Error()
}

// applyConnectOutcome traduce la respuesta del plugin al estado persistible de la cuenta.
// Un resultado simulado queda como 'demo' y nunca como conexión real.
func applyConnectOutcome(acc *conduitv1.ProtoAccount, resp *conduitv1.ConnectResponse, err error) {
	switch {
	case err != nil:
		// El plugin nombra en el error las credenciales que faltan.
		acc.Status = "error"
		acc.StatusMessage = normalizeGRPCError(err)
		acc.QrCode = ""
	case !resp.GetSuccess():
		acc.Status = "error"
		acc.StatusMessage = resp.GetMessage()
	case resp.GetSimulated():
		acc.Status = "demo"
		acc.StatusMessage = resp.GetMessage()
	case resp.GetPairing():
		acc.Status = "pairing"
		acc.StatusMessage = ""
	case strings.Contains(strings.ToLower(resp.GetMessage()), "qr"):
		acc.Status = "pairing"
		acc.StatusMessage = ""
	default:
		acc.Status = "connected"
		acc.StatusMessage = ""
	}
}

// ConnectAccount conecta una cuenta específica llamando al plugin correspondiente si está disponible.
func (s *Server) ConnectAccount(ctx context.Context, req *conduitv1.ConnectAccountRequest) (*conduitv1.ConnectAccountResponse, error) {
	acc, err := s.db.GetAccountByID(ctx, req.AccountId)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "cuenta no encontrada: %v", err)
	}

	if s.plugins != nil {
		provStr := acc.Provider.String()
		if len(provStr) > 9 && provStr[:9] == "PROVIDER_" {
			provStr = strings.ToLower(provStr[9:])
		}
		if inst, ok := s.plugins.GetInstance(provStr); ok {
			connCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			resp, err := inst.RPC.Connect(connCtx, &conduitv1.ConnectRequest{
				AccountIdentifier: acc.Identifier,
				Credentials:       acc.Credentials,
			})
			applyConnectOutcome(acc, resp, err)
			_ = s.db.SaveAccount(ctx, acc)
		}
	}

	// Refrescar cuenta desde la base de datos para obtener el qr_code si fue emitido
	if refreshed, err := s.db.GetAccountByID(ctx, req.AccountId); err == nil && refreshed != nil {
		acc = refreshed
	}

	return &conduitv1.ConnectAccountResponse{
		Account: acc,
	}, nil
}

// DisconnectAccount desconecta una cuenta.
func (s *Server) DisconnectAccount(ctx context.Context, req *conduitv1.DisconnectAccountRequest) (*conduitv1.DisconnectAccountResponse, error) {
	acc, err := s.db.GetAccountByID(ctx, req.AccountId)
	if err == nil && acc != nil {
		acc.Status = "disconnected"
		acc.QrCode = ""
		acc.StatusMessage = ""
		_ = s.db.SaveAccount(ctx, acc)

		if s.plugins != nil {
			provStr := acc.Provider.String()
			if len(provStr) > 9 && provStr[:9] == "PROVIDER_" {
				provStr = strings.ToLower(provStr[9:])
			}
			if inst, ok := s.plugins.GetInstance(provStr); ok {
				_, _ = inst.RPC.Disconnect(ctx, &conduitv1.DisconnectRequest{})
			}
		}
	}
	return &conduitv1.DisconnectAccountResponse{Success: true}, nil
}

// ListPlugins devuelve los plugins descubiertos (instalados) seguidos de los oficiales del catálogo que no lo están.
func (s *Server) ListPlugins(ctx context.Context, req *conduitv1.ListPluginsRequest) (*conduitv1.ListPluginsResponse, error) {
	plugins := []*conduitv1.ProtoPluginInfo{}
	seen := map[string]bool{}
	if s.plugins != nil {
		for _, d := range s.plugins.Discovered() {
			m := d.Manifest
			id := m.GetId()
			seen[id] = true
			repo := ""
			if entry, ok := pluginmgr.CatalogEntryFor(id); ok {
				repo = entry.Repository
			}
			plugins = append(plugins, &conduitv1.ProtoPluginInfo{
				Id:          id,
				Name:        m.GetDisplayName(),
				Description: m.GetDescription(),
				Version:     m.GetVersion(),
				Author:      m.GetAuthor(),
				IsInstalled: true,
				IsEnabled:   s.db.GetSetting(ctx, fmt.Sprintf("plugin_%s_enabled", id), "true") == "true",
				Manifest:    m,
				Path:        d.Path,
				Repository:  repo,
			})
		}
	}
	for _, entry := range pluginmgr.Catalog {
		if seen[entry.ID] {
			continue
		}
		plugins = append(plugins, &conduitv1.ProtoPluginInfo{
			Id:          entry.ID,
			Name:        entry.Name,
			Description: entry.Description,
			Repository:  entry.Repository,
		})
	}
	return &conduitv1.ListPluginsResponse{Plugins: plugins}, nil
}

// InstallPlugin instala el último release de un plugin oficial del catálogo, lo habilita y,
// si hay un cargador inyectado, lo lanza en caliente. Solo se aceptan ids del catálogo.
func (s *Server) InstallPlugin(ctx context.Context, req *conduitv1.InstallPluginRequest) (*conduitv1.InstallPluginResponse, error) {
	fail := func(msg string) (*conduitv1.InstallPluginResponse, error) {
		return &conduitv1.InstallPluginResponse{Success: false, Message: msg}, nil
	}
	if _, ok := pluginmgr.CatalogEntryFor(req.PluginId); !ok {
		return fail(fmt.Sprintf("plugin %q is not an official plugin; use `conduitd plugin install owner/repo` for third-party plugins", req.PluginId))
	}
	if s.installer == nil {
		return fail("plugin installation is not available in this daemon")
	}
	res, err := s.installer.Install(ctx, req.PluginId, "")
	if err != nil {
		return fail(err.Error())
	}
	_ = s.db.SetSetting(ctx, fmt.Sprintf("plugin_%s_enabled", res.ID), "true")

	msg := fmt.Sprintf("Plugin '%s' %s installed at %s", res.ID, res.Version, res.Path)
	switch {
	case s.loader == nil:
		msg += "; restart conduitd to load it"
	default:
		if err := s.loader(ctx, res.Path); err != nil {
			msg += fmt.Sprintf("; it could not be loaded (%v), restart conduitd to load it", err)
		} else {
			msg += " and loaded"
		}
	}
	return &conduitv1.InstallPluginResponse{Success: true, Message: msg}, nil
}

// TogglePlugin activa o desactiva un plugin de forma persistente.
func (s *Server) TogglePlugin(ctx context.Context, req *conduitv1.TogglePluginRequest) (*conduitv1.TogglePluginResponse, error) {
	key := fmt.Sprintf("plugin_%s_enabled", req.PluginId)
	val := "false"
	if req.Enable {
		val = "true"
	}
	_ = s.db.SetSetting(ctx, key, val)
	return &conduitv1.TogglePluginResponse{Success: true}, nil
}

// GetSettings obtiene la configuración actual.
func (s *Server) GetSettings(ctx context.Context, req *conduitv1.GetSettingsRequest) (*conduitv1.GetSettingsResponse, error) {
	settings := s.db.GetAppSettings(ctx)
	return &conduitv1.GetSettingsResponse{Settings: settings}, nil
}

// UpdateSettings guarda cambios en la configuración.
func (s *Server) UpdateSettings(ctx context.Context, req *conduitv1.UpdateSettingsRequest) (*conduitv1.UpdateSettingsResponse, error) {
	if req.Settings != nil {
		_ = s.db.SaveAppSettings(ctx, req.Settings)
	}
	return &conduitv1.UpdateSettingsResponse{Settings: req.Settings}, nil
}

// GetStatus retorna el estado y uptime del Core Daemon.
func (s *Server) GetStatus(ctx context.Context, req *conduitv1.GetStatusRequest) (*conduitv1.GetStatusResponse, error) {
	uptime := int64(time.Since(s.startTime).Seconds())
	return &conduitv1.GetStatusResponse{
		CoreVersion:   "0.1.0-alpha",
		UptimeSeconds: uptime,
		ChannelStatuses: map[string]string{
			"storage":  "connected (sqlite fts5)",
			"bus":      "active",
			"identity": "active",
		},
	}, nil
}

// SendReaction procesa una reacción con emoji a un mensaje, alternándola o agregándola.
func (s *Server) SendReaction(ctx context.Context, req *conduitv1.SendReactionRequest) (*conduitv1.SendReactionResponse, error) {
	if strings.TrimSpace(req.MessageId) == "" {
		return nil, status.Error(codes.InvalidArgument, "message_id es obligatorio")
	}

	msg, err := s.db.FindMessageByID(ctx, req.MessageId)
	if err != nil || msg == nil {
		if found, fErr := s.db.FindMessageByRemoteID(ctx, "", req.MessageId); fErr == nil && found != nil {
			msg = found
		} else {
			return nil, status.Errorf(codes.NotFound, "mensaje %s no encontrado", req.MessageId)
		}
	}

	senderID := "me"
	emoji := strings.TrimSpace(req.Emoji)
	var added bool
	if emoji == "" {
		_ = s.db.RemoveAllReactionsBySender(ctx, msg.ID, senderID)
	} else {
		added, err = s.db.ToggleReaction(ctx, msg.ID, senderID, emoji)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "error al actualizar reacción: %v", err)
		}
	}

	// Recargar reacciones actualizadas del mensaje
	reactionsMap, _ := s.db.GetReactionsByMessageIDs(ctx, []string{msg.ID})
	currentReactions := reactionsMap[msg.ID]
	msg.Reactions = currentReactions

	// Notificar a los clientes conectados
	s.bus.Publish(eventbus.EventMessageUpdated, msg)

	// Despachar al plugin correspondiente si está activo
	channel, _ := s.db.GetChannelByID(ctx, msg.ChannelID)
	if channel != nil && s.plugins != nil {
		pluginName := string(channel.Provider)
		if inst, ok := s.plugins.GetInstance(pluginName); ok {
			outEmoji := emoji
			if !added {
				outEmoji = ""
			}
			go func() {
				outCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				_, _ = inst.RPC.SendReaction(outCtx, &conduitv1.PluginReactionRequest{
					RemoteChatId:    channel.RemoteID,
					RemoteMessageId: msg.RemoteMessageID,
					Emoji:           outEmoji,
				})
			}()
		}
	}

	var protoReactions []*conduitv1.ProtoReaction
	for _, r := range currentReactions {
		protoReactions = append(protoReactions, &conduitv1.ProtoReaction{
			Emoji:      r.Emoji,
			Count:      int32(r.Count),
			Senders:    r.Senders,
			IncludesMe: r.IncludesMe,
		})
	}

	return &conduitv1.SendReactionResponse{
		Success:   true,
		Reactions: protoReactions,
	}, nil
}

// SendVote registra o actualiza un voto en una encuesta y lo despacha al proveedor.
func (s *Server) SendVote(ctx context.Context, req *conduitv1.SendVoteRequest) (*conduitv1.SendVoteResponse, error) {
	if strings.TrimSpace(req.MessageId) == "" {
		return nil, status.Error(codes.InvalidArgument, "message_id es obligatorio")
	}
	if len(req.SelectedOptions) == 0 {
		return nil, status.Error(codes.InvalidArgument, "debes seleccionar al menos una opción")
	}

	msg, err := s.db.FindMessageByID(ctx, req.MessageId)
	if err != nil || msg == nil {
		if found, fErr := s.db.FindMessageByRemoteID(ctx, "", req.MessageId); fErr == nil && found != nil {
			msg = found
		} else {
			return nil, status.Errorf(codes.NotFound, "mensaje %s no encontrado", req.MessageId)
		}
	}

	if msg.Poll == nil {
		return nil, status.Error(codes.FailedPrecondition, "el mensaje no es una encuesta")
	}

	if msg.Poll.IsClosed {
		return nil, status.Error(codes.FailedPrecondition, "la encuesta está cerrada")
	}

	selectedSet := make(map[string]bool)
	for _, opt := range req.SelectedOptions {
		selectedSet[strings.TrimSpace(opt)] = true
	}

	isSingleChoice := msg.Poll.SelectableCount == 1

	for i := range msg.Poll.Options {
		opt := &msg.Poll.Options[i]
		matches := selectedSet[opt.ID] || selectedSet[opt.Text]
		if isSingleChoice {
			if matches {
				if !opt.Chosen {
					opt.Chosen = true
					opt.VoteCount++
					opt.Voters = append(opt.Voters, "me")
				}
			} else {
				if opt.Chosen {
					opt.Chosen = false
					if opt.VoteCount > 0 {
						opt.VoteCount--
					}
					var newVoters []string
					for _, v := range opt.Voters {
						if v != "me" {
							newVoters = append(newVoters, v)
						}
					}
					opt.Voters = newVoters
				}
			}
		} else {
			if matches {
				if !opt.Chosen {
					opt.Chosen = true
					opt.VoteCount++
					opt.Voters = append(opt.Voters, "me")
				} else {
					opt.Chosen = false
					if opt.VoteCount > 0 {
						opt.VoteCount--
					}
					var newVoters []string
					for _, v := range opt.Voters {
						if v != "me" {
							newVoters = append(newVoters, v)
						}
					}
					opt.Voters = newVoters
				}
			}
		}
	}

	total := 0
	for _, opt := range msg.Poll.Options {
		total += opt.VoteCount
	}
	msg.Poll.TotalVotes = total

	pollJSON, _ := json.Marshal(msg.Poll)
	msg.Metadata = string(pollJSON)
	_ = s.db.UpdateMessageMetadata(ctx, msg.ID, string(pollJSON))

	s.bus.Publish(eventbus.EventMessageUpdated, msg)

	channel, _ := s.db.GetChannelByID(ctx, msg.ChannelID)
	if channel != nil && s.plugins != nil {
		pluginName := string(channel.Provider)
		if inst, ok := s.plugins.GetInstance(pluginName); ok {
			go func() {
				outCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				_, _ = inst.RPC.SendVote(outCtx, &conduitv1.PluginVoteRequest{
					RemoteChatId:    channel.RemoteID,
					RemoteMessageId: msg.RemoteMessageID,
					SelectedOptions: req.SelectedOptions,
				})
			}()
		}
	}

	return &conduitv1.SendVoteResponse{
		Success: true,
		Poll:    storage.PollDomainToProto(msg.Poll),
	}, nil
}

// ClickButton procesa la interacción con un botón interactivo y lo delega al plugin correspondiente.
func (s *Server) ClickButton(ctx context.Context, req *conduitv1.ClickButtonRequest) (*conduitv1.ClickButtonResponse, error) {
	if strings.TrimSpace(req.MessageId) == "" {
		return nil, status.Error(codes.InvalidArgument, "message_id es obligatorio")
	}
	if strings.TrimSpace(req.ButtonId) == "" {
		return nil, status.Error(codes.InvalidArgument, "button_id es obligatorio")
	}

	msg, err := s.db.FindMessageByID(ctx, req.MessageId)
	if err != nil || msg == nil {
		if found, fErr := s.db.FindMessageByRemoteID(ctx, "", req.MessageId); fErr == nil && found != nil {
			msg = found
		} else {
			return nil, status.Errorf(codes.NotFound, "mensaje %s no encontrado", req.MessageId)
		}
	}

	// Despachar al plugin correspondiente si está disponible
	channel, _ := s.db.GetChannelByID(ctx, msg.ChannelID)
	if channel == nil || s.plugins == nil {
		return &conduitv1.ClickButtonResponse{
			Success:         true,
			FeedbackMessage: "Botón presionado",
		}, nil
	}

	pluginName := string(channel.Provider)
	inst, ok := s.plugins.GetInstance(pluginName)
	if !ok {
		return &conduitv1.ClickButtonResponse{
			Success:         true,
			FeedbackMessage: "Botón presionado",
		}, nil
	}

	outCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	resp, err := inst.RPC.ClickButton(outCtx, &conduitv1.PluginClickButtonRequest{
		RemoteChatId:    channel.RemoteID,
		RemoteMessageId: msg.RemoteMessageID,
		ButtonId:        req.ButtonId,
		ButtonText:      req.ButtonText,
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "error al presionar botón en el proveedor: %v", err)
	}
	if !resp.Success {
		return &conduitv1.ClickButtonResponse{
			Success:         false,
			FeedbackMessage: resp.ErrorMessage,
		}, nil
	}

	return &conduitv1.ClickButtonResponse{
		Success:         true,
		FeedbackMessage: resp.FeedbackMessage,
		IsAlert:         resp.IsAlert,
		Url:             resp.Url,
	}, nil
}

// EditMessage edita el cuerpo de un mensaje existente y lo sincroniza con el proveedor.
func (s *Server) EditMessage(ctx context.Context, req *conduitv1.EditMessageRequest) (*conduitv1.EditMessageResponse, error) {
	if strings.TrimSpace(req.MessageId) == "" {
		return nil, status.Error(codes.InvalidArgument, "message_id es obligatorio")
	}
	newBody := strings.TrimSpace(req.NewBody)
	if newBody == "" {
		return nil, status.Error(codes.InvalidArgument, "new_body no puede estar vacío")
	}

	msg, err := s.db.FindMessageByID(ctx, req.MessageId)
	if err != nil || msg == nil {
		if found, fErr := s.db.FindMessageByRemoteID(ctx, "", req.MessageId); fErr == nil && found != nil {
			msg = found
		} else {
			return nil, status.Errorf(codes.NotFound, "mensaje %s no encontrado", req.MessageId)
		}
	}

	if err := s.db.UpdateMessageBody(ctx, msg.ID, newBody); err != nil {
		return nil, status.Errorf(codes.Internal, "error actualizando mensaje: %v", err)
	}

	msg.Body = newBody
	msg.IsEdited = true
	now := time.Now()
	msg.EditedAt = &now

	s.bus.Publish(eventbus.EventMessageUpdated, msg)

	channel, _ := s.db.GetChannelByID(ctx, msg.ChannelID)
	if channel != nil && s.plugins != nil {
		pluginName := string(channel.Provider)
		if inst, ok := s.plugins.GetInstance(pluginName); ok {
			go func() {
				outCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				_, _ = inst.RPC.EditMessage(outCtx, &conduitv1.PluginEditMessageRequest{
					RemoteChatId:    channel.RemoteID,
					RemoteMessageId: msg.RemoteMessageID,
					NewBody:         newBody,
				})
			}()
		}
	}

	return &conduitv1.EditMessageResponse{
		Success: true,
		Message: domainMsgToProto(msg),
	}, nil
}

// DeleteMessage elimina un mensaje en la base de datos y envía la revocación al proveedor.
func (s *Server) DeleteMessage(ctx context.Context, req *conduitv1.DeleteMessageRequest) (*conduitv1.DeleteMessageResponse, error) {
	if strings.TrimSpace(req.MessageId) == "" {
		return nil, status.Error(codes.InvalidArgument, "message_id es obligatorio")
	}

	msg, err := s.db.FindMessageByID(ctx, req.MessageId)
	if err != nil || msg == nil {
		if found, fErr := s.db.FindMessageByRemoteID(ctx, "", req.MessageId); fErr == nil && found != nil {
			msg = found
		} else {
			return nil, status.Errorf(codes.NotFound, "mensaje %s no encontrado", req.MessageId)
		}
	}

	if err := s.db.DeleteMessage(ctx, msg.ID); err != nil {
		return nil, status.Errorf(codes.Internal, "error eliminando mensaje: %v", err)
	}

	s.bus.Publish(eventbus.EventMessageDeleted, msg.ID)

	channel, _ := s.db.GetChannelByID(ctx, msg.ChannelID)
	if channel != nil && s.plugins != nil {
		pluginName := string(channel.Provider)
		if inst, ok := s.plugins.GetInstance(pluginName); ok {
			go func() {
				outCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				_, _ = inst.RPC.DeleteMessage(outCtx, &conduitv1.PluginDeleteMessageRequest{
					RemoteChatId:    channel.RemoteID,
					RemoteMessageId: msg.RemoteMessageID,
				})
			}()
		}
	}

	return &conduitv1.DeleteMessageResponse{Success: true}, nil
}

// PinMessage fija o desfija un mensaje en la conversación y en el proveedor.
func (s *Server) PinMessage(ctx context.Context, req *conduitv1.PinMessageRequest) (*conduitv1.PinMessageResponse, error) {
	if strings.TrimSpace(req.MessageId) == "" {
		return nil, status.Error(codes.InvalidArgument, "message_id es obligatorio")
	}

	msg, err := s.db.FindMessageByID(ctx, req.MessageId)
	if err != nil || msg == nil {
		if found, fErr := s.db.FindMessageByRemoteID(ctx, "", req.MessageId); fErr == nil && found != nil {
			msg = found
		} else {
			return nil, status.Errorf(codes.NotFound, "mensaje %s no encontrado", req.MessageId)
		}
	}

	if err := s.db.SetMessagePinned(ctx, msg.ID, req.Pinned); err != nil {
		return nil, status.Errorf(codes.Internal, "error actualizando estado fijado: %v", err)
	}

	msg.IsPinned = req.Pinned
	s.bus.Publish(eventbus.EventMessageUpdated, msg)

	channel, _ := s.db.GetChannelByID(ctx, msg.ChannelID)
	if channel != nil && s.plugins != nil {
		pluginName := string(channel.Provider)
		if inst, ok := s.plugins.GetInstance(pluginName); ok {
			go func() {
				outCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				fromMe := (msg.SenderType == domain.SenderMe)
				_, _ = inst.RPC.PinMessage(outCtx, &conduitv1.PluginPinMessageRequest{
					RemoteChatId:    channel.RemoteID,
					RemoteMessageId: msg.RemoteMessageID,
					Pinned:          req.Pinned,
					FromMe:          fromMe,
					ParticipantId:   msg.SenderRemoteID,
				})
			}()
		}
	}

	return &conduitv1.PinMessageResponse{
		Success:  true,
		IsPinned: req.Pinned,
	}, nil
}

// requestHistory pide al plugin del proveedor una página de historial: la última
// si no hay cursor, o la anterior a ese mensaje. Es genérica: sirve para cualquier
// proveedor que implemente SyncChatHistory.
func (s *Server) requestHistory(convID, beforeID string) {
	if s.plugins == nil {
		return
	}

	targetRemoteID, provider := s.historyTarget(convID)
	if targetRemoteID == "" || provider == "" {
		return
	}
	if !s.allowHistoryRequest(convID) {
		return
	}

	// Sin cursor se pide la última página; con cursor, la anterior a ese mensaje.
	var oldestRemoteID string
	var oldestTime time.Time
	var oldestSenderType string
	if beforeID != "" {
		err := s.db.QueryRowContext(
			context.Background(),
			"SELECT remote_message_id, sent_at, sender_type FROM messages WHERE id = ? OR remote_message_id = ? ORDER BY rowid DESC LIMIT 1",
			beforeID, beforeID,
		).Scan(&oldestRemoteID, &oldestTime, &oldestSenderType)
		if err != nil || oldestRemoteID == "" {
			return
		}
	}

	inst, ok := s.plugins.GetInstance(provider)
	if !ok {
		return
	}

	syncCtx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	_, _ = inst.RPC.SyncChatHistory(syncCtx, &conduitv1.SyncChatHistoryRequest{
		RemoteChatId:               targetRemoteID,
		OldestRemoteMessageId:      oldestRemoteID,
		OldestMessageTimestampUnix: oldestTime.Unix(),
		OldestMessageFromMe:        oldestSenderType == "me" || oldestSenderType == "USER",
		Limit:                      50,
	})
}

// historyTarget resuelve el chat remoto y el proveedor de una conversación.
func (s *Server) historyTarget(convID string) (remoteID, provider string) {
	if conv, err := s.db.GetConversationByID(context.Background(), convID); err == nil && conv != nil {
		if conv.ActiveChannelId != "" {
			if ch, err := s.db.GetChannelByID(context.Background(), conv.ActiveChannelId); err == nil && ch != nil {
				remoteID = ch.RemoteID
				provider = providerName(ch.Provider)
			}
		}
		if remoteID == "" && conv.ContactId != "" {
			if chs, err := s.db.GetChannelsByContactID(context.Background(), conv.ContactId); err == nil && len(chs) > 0 {
				remoteID = chs[0].RemoteID
				provider = providerName(chs[0].Provider)
			}
		}
	}
	if remoteID != "" {
		return remoteID, provider
	}

	// Respaldo: el identificador de la conversación empieza con el proveedor.
	if idx := strings.Index(convID, ":"); idx > 0 {
		return convID[idx+1:], convID[:idx]
	}
	return convID, ""
}

// allowHistoryRequest aplica un enfriamiento por conversación para que el scroll
// no dispare una petición al proveedor en cada evento.
func (s *Server) allowHistoryRequest(convID string) bool {
	const cooldown = 5 * time.Second

	s.historyMu.Lock()
	defer s.historyMu.Unlock()
	if s.historyCooldown == nil {
		s.historyCooldown = make(map[string]time.Time)
	}
	if last, ok := s.historyCooldown[convID]; ok && time.Since(last) < cooldown {
		return false
	}
	s.historyCooldown[convID] = time.Now()
	return true
}

// providerName normaliza el nombre del proveedor para el gestor de plugins.
func providerName(p domain.Provider) string {
	return strings.ToLower(strings.TrimPrefix(string(p), "PROVIDER_"))
}

// MarkConversationRead marca los mensajes recibidos como leídos en SQLite y propaga el leído al proveedor
// en segundo plano. Siempre intenta vaciar los pendientes, incluso sin mensajes nuevos (reintento).
func (s *Server) MarkConversationRead(ctx context.Context, req *conduitv1.MarkConversationReadRequest) (*conduitv1.MarkConversationReadResponse, error) {
	if req.ConversationId == "" {
		return nil, status.Error(codes.InvalidArgument, "conversation_id es obligatorio")
	}

	marked, err := s.db.MarkConversationReadForSync(ctx, req.ConversationId)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "error actualizando mensajes en base de datos: %v", err)
	}

	go FlushReadSync(context.Background(), s.db, s.plugins, req.ConversationId)

	return &conduitv1.MarkConversationReadResponse{
		Success:     true,
		MarkedCount: int32(marked),
	}, nil
}
