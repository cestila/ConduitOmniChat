package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/cestila/ConduitOmniChat/core/pkg/domain"
	"github.com/cestila/ConduitOmniChat/core/pkg/eventbus"
	"github.com/cestila/ConduitOmniChat/core/pkg/identity"
	"github.com/cestila/ConduitOmniChat/core/pkg/plugininstall"
	"github.com/cestila/ConduitOmniChat/core/pkg/pluginmgr"
	"github.com/cestila/ConduitOmniChat/core/pkg/rpc"
	"github.com/cestila/ConduitOmniChat/core/pkg/storage"
	conduitv1 "github.com/cestila/ConduitOmniChat/sdk/conduitv1"
	"github.com/google/uuid"
	"google.golang.org/grpc"
)

const (
	defaultPort = 50051
)

func main() {
	// Configuración de rutas de datos
	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("Error obteniendo home directory: %v", err)
	}
	dataDir := filepath.Join(homeDir, ".local", "share", "conduit")
	dbPath := filepath.Join(dataDir, "conduit.db")

	execPath, _ := os.Executable()
	baseDir := filepath.Dir(execPath)
	cwd, _ := os.Getwd()
	pluginDirs := pluginmgr.SearchDirs(os.Getenv("CONDUIT_PLUGIN_DIR"), dataDir, baseDir, cwd)

	// Subcomandos (plugin install/list/remove, help): terminan antes de arrancar el daemon.
	cliInstaller := plugininstall.New(filepath.Join(dataDir, "plugins"))
	if handled, code := runCLI(os.Args[1:], os.Stdout, os.Stderr, cliDeps{Installer: cliInstaller, PluginDirs: pluginDirs}); handled {
		os.Exit(code)
	}

	fmt.Println("🚀 Iniciando ConduitOmniChat Daemon (conduitd)...")

	// Inicializar SQLite Storage
	db, err := storage.NewDB(dbPath)
	if err != nil {
		log.Fatalf("Error inicializando base de datos: %v", err)
	}
	defer db.Close()
	fmt.Printf("📦 Base de datos SQLite activa en: %s\n", dbPath)

	// Inicializar Event Bus
	bus := eventbus.NewBus()
	defer bus.Close()

	// Inicializar Identity Graph
	graph := identity.NewGraph(db)

	// Inicializar Plugin Manager
	pluginManager := pluginmgr.NewManager()
	// Motor de descargas compartido: la autodescarga (loop de plugins) y el RPC usan la misma
	// instancia para deduplicar descargas del mismo adjunto.
	downloader := rpc.NewDownloader(db, bus, pluginManager)
	defer pluginManager.StopAll()

	// Descubrimiento dinámico de plugins: cada ejecutable conduit-plugin-* se lanza,
	// se le pide el manifiesto y se registra por su id (gana el primero encontrado).
	pluginEnabled := func(id string) bool {
		return db.GetSetting(context.Background(), "plugin_"+id+"_enabled", "true") == "true"
	}

	// loadPlugin lanza un ejecutable de plugin, lo registra y arranca su loop de eventos.
	// Lo usan el arranque del daemon y la carga en caliente del RPC InstallPlugin.
	loadPlugin := func(ctx context.Context, binPath string) error {
		pInst, err := pluginManager.LaunchDiscovered(ctx, binPath, pluginEnabled)
		if err != nil {
			return err
		}
		cfg := pluginConfigFromManifest(pInst.Manifest)
		fmt.Printf("🔌 Plugin %s cargado desde: %s\n", cfg.Name, binPath)

		// Escuchar stream de eventos unificado
		go func(inst *pluginmgr.Instance, plugCfg pluginConfig) {
			stream, err := inst.RPC.StreamIncomingEvents(context.Background(), &conduitv1.PluginStreamRequest{})
			if err != nil {
				log.Printf("⚠️ Error iniciando stream de eventos %s: %v", plugCfg.Name, err)
				return
			}
			for {
				ev, err := stream.Recv()
				if err != nil {
					break
				}
				switch ev.EventType {
				case "channel.auth_qr":
					// Buscar cuenta del proveedor para actualizar estado a pairing y asignar QR
					accs, _ := db.ListAccounts(context.Background())
					for _, a := range accs {
						if a.Provider == plugCfg.Proto {
							a.Status = "pairing"
							a.QrCode = ev.Body
							_ = db.SaveAccount(context.Background(), a)
						}
					}
					bus.Publish(eventbus.EventChannelConnected, "qr:"+ev.Body)

				case "channel.auth_error":
					// Un fallo de autenticación debe verse en la cuenta, no quedar solo en el log del plugin.
					accs, _ := db.ListAccounts(context.Background())
					for _, a := range accs {
						if a.Provider == plugCfg.Proto {
							a.Status = "error"
							a.StatusMessage = ev.Body
							a.QrCode = ""
							_ = db.SaveAccount(context.Background(), a)
						}
					}
					bus.Publish(eventbus.EventChannelConnected, plugCfg.Name+":auth_error")

				case "channel.ready":
					applyChannelReadyForProvider(context.Background(), db, plugCfg.Proto, ev)
					bus.Publish(eventbus.EventChannelConnected, plugCfg.Name+":connected")
					// Recupera lo que llegó mientras el daemon no corría.
					go catchUpRecentMessages(db, pluginManager, plugCfg.Provider, 15)
					// Reintenta los leídos que no pudieron llegar al proveedor mientras estaba caído.
					go rpc.FlushPendingReadSyncForProvider(context.Background(), db, pluginManager, plugCfg.Provider)

				case "contact.sync":
					// Si es un grupo, registrar como conversación independiente directamente SIN tocar la tabla de contactos
					if ev.Body == "group" || strings.Contains(ev.RemoteSenderId, "@g.us") {
						convID := fmt.Sprintf("%s:%s", plugCfg.Name, ev.RemoteSenderId)
						// Crear o asegurar el canal para el grupo sin contact_id
						channelID := uuid.New().String()
						existingChan, _ := db.GetChannelByProviderAndRemoteID(context.Background(), string(plugCfg.Provider), ev.RemoteSenderId)
						if existingChan != nil {
							channelID = existingChan.ID
						} else {
							_ = db.SaveChannel(context.Background(), &domain.ContactChannel{
								ID:          channelID,
								ContactID:   "", // Grupos no pertenecen a personas
								Provider:    plugCfg.Provider,
								RemoteID:    ev.RemoteSenderId,
								Handle:      ev.SenderName,
								IsPreferred: true,
							})
						}

						_ = db.EnsureConversation(context.Background(), &domain.Conversation{
							ID:              convID,
							ContactID:       nil,
							IsGroup:         true,
							Title:           ev.SenderName,
							LastMessageAt:   time.Unix(ev.SentAtUnix, 0),
							ActiveChannelID: &channelID,
							ChannelType:     "group",
							AvatarURL:       ev.AvatarUrl,
						})
					} else {
						// Contacto humano 1 a 1: procesar con Identity Graph
						displayName := ev.SenderName
						if displayName == "" {
							displayName = ev.RemoteSenderId
						}
						contact, channel, err := graph.ResolveOrLinkChannel(context.Background(), plugCfg.Provider, ev.RemoteSenderId, ev.Body, displayName, ev.AvatarUrl, identity.Hints{Phone: ev.IdentityPhone, Email: ev.IdentityEmail})
						if err != nil {
							log.Printf("⚠️ No se pudo registrar el contacto %q (%s): %v", displayName, plugCfg.Name, err)
						} else if channel != nil {
							var lastMsg time.Time
							if ev.SentAtUnix > 0 {
								lastMsg = time.Unix(ev.SentAtUnix, 0)
							} else {
								lastMsg = time.Unix(0, 0)
							}
							_ = db.EnsureConversation(context.Background(), &domain.Conversation{
								ID:              contact.ID,
								ContactID:       &contact.ID,
								IsGroup:         false,
								Title:           contact.DisplayName,
								LastMessageAt:   lastMsg,
								ActiveChannelID: &channel.ID,
								ChannelType:     ev.ChannelType,
								AvatarURL:       contact.AvatarURL,
							})
						}
					}

				case "channel.sync":
					// Sincronización de canales de servidores, supergrupos y foros jerárquicos
					channelTitle := ev.ChannelName
					if channelTitle == "" {
						channelTitle = ev.SenderName
					}
					convID := fmt.Sprintf("%s:%s", plugCfg.Name, ev.RemoteSenderId)
					channelID := uuid.New().String()
					existingChan, _ := db.GetChannelByProviderAndRemoteID(context.Background(), string(plugCfg.Provider), ev.RemoteSenderId)
					if existingChan != nil {
						channelID = existingChan.ID
					} else {
						_ = db.SaveChannel(context.Background(), &domain.ContactChannel{
							ID:          channelID,
							ContactID:   "",
							Provider:    plugCfg.Provider,
							RemoteID:    ev.RemoteSenderId,
							Handle:      channelTitle,
							IsPreferred: true,
						})
					}

					// El orden lo dicta el servidor: se respeta la posición ya asignada y se
					// calcula una nueva solo si el canal no la tiene (nuevo o base anterior).
					position := channelPositionFor(context.Background(), db, convID, ev.ServerId)

					conv := &domain.Conversation{
						ID:              convID,
						ContactID:       nil,
						IsGroup:         true,
						Title:           channelTitle,
						LastMessageAt:   time.Unix(ev.SentAtUnix, 0),
						ActiveChannelID: &channelID,
						ServerID:        ev.ServerId,
						ServerName:      ev.ServerName,
						ParentChannelID: ev.ParentChannelId,
						ChannelType:     ev.ChannelType,
						Position:        position,
					}
					_ = db.EnsureConversation(context.Background(), conv)

				case "message.received", "message.sent", "messages.read", "message.reaction", "message.poll_update", "message.edited", "message.deleted", "message.pinned":
					// Una lectura nunca crea ni reordena: se resuelve aparte contra lo existente.
					if ev.EventType == "messages.read" {
						applyReadEvent(context.Background(), db, bus, plugCfg.Name, plugCfg.Provider, ev)
						continue
					}
					chatTarget := ev.ChatJid
					if chatTarget == "" {
						chatTarget = ev.RemoteSenderId
					}
					// Descartar mensajes de status@broadcast
					if strings.Contains(chatTarget, "broadcast") {
						break
					}

					// Eventos sobre mensajes ya existentes: no crean contactos, canales ni reinician LastMessageAt a 0
					if ev.EventType == "message.deleted" {
						applyDeleteEvent(context.Background(), db, bus, plugCfg.Provider, ev)
						continue
					}

					if ev.EventType == "message.edited" {
						applyEditEvent(context.Background(), db, bus, plugCfg.Provider, ev)
						continue
					}

					if ev.EventType == "message.pinned" {
						var channelID string
						if chatTarget != "" {
							if existingChan, _ := db.GetChannelByProviderAndRemoteID(context.Background(), string(plugCfg.Provider), chatTarget); existingChan != nil {
								channelID = existingChan.ID
							}
						}
						targetMsg, err := db.FindMessageByRemoteID(context.Background(), channelID, ev.RemoteMessageId)
						if (err != nil || targetMsg == nil) && channelID != "" {
							targetMsg, err = db.FindMessageByRemoteID(context.Background(), "", ev.RemoteMessageId)
						}
						if err == nil && targetMsg != nil {
							_ = db.SetMessagePinned(context.Background(), targetMsg.ID, ev.IsPinned)
							targetMsg.IsPinned = ev.IsPinned
							bus.Publish(eventbus.EventMessageUpdated, targetMsg)
						}
						continue
					}

					if ev.EventType == "message.reaction" {
						var channelID string
						if chatTarget != "" {
							if existingChan, _ := db.GetChannelByProviderAndRemoteID(context.Background(), string(plugCfg.Provider), chatTarget); existingChan != nil {
								channelID = existingChan.ID
							}
						}
						targetMsg, err := db.FindMessageByRemoteID(context.Background(), channelID, ev.RemoteMessageId)
						if (err != nil || targetMsg == nil) && channelID != "" {
							targetMsg, err = db.FindMessageByRemoteID(context.Background(), "", ev.RemoteMessageId)
						}
						if err == nil && targetMsg != nil {
							sender := ev.SenderName
							if sender == "" {
								sender = ev.ParticipantId
							}
							if sender == "" {
								sender = ev.RemoteSenderId
							}
							if sender == "" {
								sender = "contact"
							}
							if ev.ReactionRemoved || ev.ReactionEmoji == "" {
								_ = db.RemoveReaction(context.Background(), targetMsg.ID, sender, ev.ReactionEmoji)
							} else {
								_ = db.AddReaction(context.Background(), targetMsg.ID, sender, ev.ReactionEmoji)
							}
							reactsMap, _ := db.GetReactionsByMessageIDs(context.Background(), []string{targetMsg.ID})
							targetMsg.Reactions = reactsMap[targetMsg.ID]
							bus.Publish(eventbus.EventMessageUpdated, targetMsg)
						}
						continue
					}

					if ev.EventType == "message.poll_update" {
						var channelID string
						if chatTarget != "" {
							if existingChan, _ := db.GetChannelByProviderAndRemoteID(context.Background(), string(plugCfg.Provider), chatTarget); existingChan != nil {
								channelID = existingChan.ID
							}
						}
						targetMsg, err := db.FindMessageByRemoteID(context.Background(), channelID, ev.RemoteMessageId)
						if (err != nil || targetMsg == nil) && channelID != "" {
							targetMsg, err = db.FindMessageByRemoteID(context.Background(), "", ev.RemoteMessageId)
						}
						if err == nil && targetMsg != nil && ev.Poll != nil {
							targetMsg.Poll = storage.PollProtoToDomain(ev.Poll)
							pollJSON, _ := json.Marshal(targetMsg.Poll)
							targetMsg.Metadata = string(pollJSON)
							_ = db.UpdateMessageMetadata(context.Background(), targetMsg.ID, string(pollJSON))
							bus.Publish(eventbus.EventMessageUpdated, targetMsg)
						}
						continue
					}
					isGroup := strings.Contains(chatTarget, "@g.us") || ev.ServerId != ""

					var convID string
					var channelID string
					var convTitle string

					if isGroup {
						// Conversación de Grupo o Canal del servidor
						convID = fmt.Sprintf("%s:%s", plugCfg.Name, chatTarget)

						// Determinar el título real del grupo o canal (NUNCA el nombre del remitente individual)
						groupTitle := ev.ChannelName
						if groupTitle == "" {
							groupTitle = ev.ServerName
						}

						existingChan, _ := db.GetChannelByProviderAndRemoteID(context.Background(), string(plugCfg.Provider), chatTarget)
						if existingChan != nil {
							channelID = existingChan.ID
							if groupTitle == "" && existingChan.Handle != "" && existingChan.Handle != chatTarget && !strings.Contains(existingChan.Handle, "@g.us") {
								groupTitle = existingChan.Handle
							}
						} else {
							channelID = uuid.New().String()
							_ = db.SaveChannel(context.Background(), &domain.ContactChannel{
								ID:          channelID,
								ContactID:   "",
								Provider:    plugCfg.Provider,
								RemoteID:    chatTarget,
								Handle:      groupTitle,
								IsPreferred: true,
							})
						}

						if groupTitle == "" {
							if existingConv, err := db.GetConversationByID(context.Background(), convID); err == nil && existingConv != nil && existingConv.Title != "" && existingConv.Title != chatTarget && !strings.Contains(existingConv.Title, "@g.us") {
								groupTitle = existingConv.Title
							}
						}

						// Si es un tema pero groupTitle vino como el nombre del servidor, preservar el título previo del tema
						if strings.Contains(chatTarget, "_topic_") && (groupTitle == ev.ServerName || groupTitle == "") {
							if existingConv, err := db.GetConversationByID(context.Background(), convID); err == nil && existingConv != nil && existingConv.Title != "" && existingConv.Title != ev.ServerName {
								groupTitle = existingConv.Title
							}
						}

						convTitle := groupTitle

						chanType := ev.ChannelType
						if chanType == "" {
							chanType = "group"
						}

						_ = db.EnsureConversation(context.Background(), &domain.Conversation{
							ID:              convID,
							ContactID:       nil,
							IsGroup:         true,
							Title:           convTitle,
							LastMessageAt:   time.Unix(ev.SentAtUnix, 0),
							ActiveChannelID: &channelID,
							ServerID:        ev.ServerId,
							ServerName:      ev.ServerName,
							ParentChannelID: ev.ParentChannelId,
							ChannelType:     chanType,
							AvatarURL:       ev.AvatarUrl,
						})
					} else {
						// Chat individual 1 a 1 con un Contacto humano
						displayName := ev.ChannelName
						if displayName == "" && ev.SenderName != "me" {
							displayName = ev.SenderName
						}
						if displayName == "" {
							displayName = chatTarget
						}
						contact, channel, err := graph.ResolveOrLinkChannel(context.Background(), plugCfg.Provider, chatTarget, chatTarget, displayName, ev.AvatarUrl, identity.Hints{Phone: ev.IdentityPhone, Email: ev.IdentityEmail})
						if err != nil {
							log.Printf("⚠️ No se pudo resolver el contacto del mensaje %q (%s): %v", displayName, plugCfg.Name, err)
						} else if channel != nil {
							convID = contact.ID
							channelID = channel.ID
							convTitle = contact.DisplayName
							_ = db.EnsureConversation(context.Background(), &domain.Conversation{
								ID:              convID,
								ContactID:       &contact.ID,
								IsGroup:         false,
								Title:           convTitle,
								LastMessageAt:   time.Unix(ev.SentAtUnix, 0),
								ActiveChannelID: &channelID,
								ChannelType:     ev.ChannelType,
								AvatarURL:       contact.AvatarURL,
							})
						}
					}

					if convID != "" && channelID != "" {
						senderType := domain.SenderContact
						if ev.EventType == "message.sent" {
							senderType = domain.SenderMe
						} else if isGroup && ev.SenderName != "" {
							senderType = domain.SenderType(ev.SenderName)
						}

						cType := domain.ContentTypeText
						if ev.ContentType != "" {
							cType = domain.ContentType(ev.ContentType)
						}

						var domainPoll *domain.Poll
						if ev.Poll != nil {
							domainPoll = storage.PollProtoToDomain(ev.Poll)
							cType = domain.ContentTypePoll
							if ev.Body == "" && ev.Poll.Question != "" {
								ev.Body = ev.Poll.Question
							}
						}

						var domainButtonRows []domain.ButtonRow
						if len(ev.ButtonRows) > 0 {
							domainButtonRows = storage.ButtonRowsProtoToDomain(ev.ButtonRows)
						}

						var domainLoc *domain.Location
						if ev.Location != nil {
							domainLoc = &domain.Location{
								Latitude:  ev.Location.Latitude,
								Longitude: ev.Location.Longitude,
								Title:     ev.Location.Title,
								Address:   ev.Location.Address,
							}
						}

						var domainContact *domain.SharedContact
						if ev.SharedContact != nil {
							domainContact = &domain.SharedContact{
								DisplayName: ev.SharedContact.DisplayName,
								PhoneNumber: ev.SharedContact.PhoneNumber,
								Vcard:       ev.SharedContact.Vcard,
							}
						}

						var attachments []domain.Attachment
						for _, a := range ev.Attachments {
							locPath := a.LocalPath
							var pLocPath *string
							if locPath != "" {
								pLocPath = &locPath
							}
							attID := a.Id
							if attID == "" {
								attID = uuid.New().String()
							}
							dStatus := domain.DownloadStatus(a.DownloadStatus)
							if dStatus == "" {
								if pLocPath != nil {
									dStatus = domain.MediaCached
								} else {
									dStatus = domain.MediaRemoteOnly
								}
							}
							attachments = append(attachments, domain.Attachment{
								ID:             attID,
								MessageID:      "", // se asigna abajo
								FileName:       a.FileName,
								MimeType:       a.MimeType,
								FileSize:       a.FileSize,
								LocalPath:      pLocPath,
								DownloadStatus: dStatus,
								Checksum:       a.Checksum,
								RemoteRef:      a.RemoteRef,
							})
						}

						newMsgID := uuid.New().String()
						for i := range attachments {
							attachments[i].MessageID = newMsgID
						}

						var replyToID *string
						var replyToSender, replyToBody string
						if ev.ReplyToRemoteId != "" {
							if pID, err := db.FindMessageIDByRemoteID(context.Background(), channelID, ev.ReplyToRemoteId); err == nil && pID != "" {
								replyToID = &pID
								if pMsg, err := db.FindMessageByID(context.Background(), pID); err == nil && pMsg != nil {
									replyToSender = string(pMsg.SenderType)
									replyToBody = pMsg.Body
								}
							}
						}

						msg := &domain.Message{
							ID:               newMsgID,
							ConversationID:   convID,
							ChannelID:        channelID,
							RemoteMessageID:  ev.RemoteMessageId,
							SenderType:       senderType,
							SenderRemoteID:   ev.ParticipantId,
							Body:             ev.Body,
							ContentType:      cType,
							Status:           domain.StatusDelivered,
							SentAt:           time.Unix(ev.SentAtUnix, 0),
							ReceivedAt:       time.Now(),
							Attachments:      attachments,
							ReplyToMessageID: replyToID,
							ReplyToSender:    replyToSender,
							ReplyToBody:      replyToBody,
							Poll:             domainPoll,
							ButtonRows:       domainButtonRows,
							Location:         domainLoc,
							SharedContact:    domainContact,
							IsPinned:         ev.IsPinned,
							IsEdited:         ev.IsEdited,
						}
						_ = db.SaveMessage(context.Background(), msg)

						// El sticky de un chat 1 a 1 es la última cuenta usada.
						if !isGroup {
							_ = db.SetConversationActiveChannel(context.Background(), convID, channelID)
						}

						if ev.EventType == "message.sent" {
							bus.Publish(eventbus.EventMessageSent, msg)
						} else {
							bus.Publish(eventbus.EventMessageReceived, msg)
							// Autodescarga según ajustes, después de anunciar el mensaje para que
							// el message_updated llegue tras el message_received.
							downloader.AutoDownload(context.Background(), msg)
						}
					}
				}
			}
		}(pInst, cfg)
		return nil
	}

	for _, binPath := range pluginmgr.FindBinaries(pluginDirs) {
		if err := loadPlugin(context.Background(), binPath); err != nil {
			switch {
			case errors.Is(err, pluginmgr.ErrPluginDisabled):
				log.Printf("⏸️ Plugin deshabilitado en ajustes: %v", err)
			case errors.Is(err, pluginmgr.ErrDuplicatePlugin):
				log.Printf("⚠️ Plugin duplicado ignorado: %v", err)
			default:
				log.Printf("⚠️ Error iniciando plugin %s: %v", binPath, err)
			}
		}
	}

	// Restaurar y reanudar sesiones activas persistidas en la base de datos
	go func() {
		time.Sleep(500 * time.Millisecond)
		savedAccounts, err := db.ListAccounts(context.Background())
		if err != nil {
			return
		}

		for _, acc := range savedAccounts {
			// Solo re-conectar si la cuenta ya estaba conectada
			if acc.Status == "connected" {
				provStr := acc.Provider.String()
				if len(provStr) > 9 && provStr[:9] == "PROVIDER_" {
					provStr = strings.ToLower(provStr[9:])
				}

				if inst, ok := pluginManager.GetInstance(provStr); ok {
					fmt.Printf("🔄 Reanudando sesión persistida para cuenta '%s' (%s)...\n", acc.Label, provStr)
					connCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
					resp, err := inst.RPC.Connect(connCtx, resumeConnectRequest(acc))
					cancel()
					if err != nil {
						log.Printf("⚠️ No se pudo reanudar cuenta %s: %v", acc.Label, err)
					} else {
						fmt.Printf("✅ Sesión reanudada para %s: %s\n", acc.Label, resp.Message)
					}
				}
			}
		}
	}()

	// Servidor gRPC
	grpcServer := grpc.NewServer()
	rpcService := rpc.NewServer(db, bus, graph, pluginManager)
	rpcService.SetDownloader(downloader)
	rpcService.SetPluginInstaller(cliInstaller)
	// El contexto de la petición RPC termina con ella; el plugin debe vivir con el daemon.
	rpcService.SetPluginLoader(func(_ context.Context, path string) error {
		return loadPlugin(context.Background(), path)
	})
	conduitv1.RegisterConduitServiceServer(grpcServer, rpcService)

	addr := fmt.Sprintf("127.0.0.1:%d", defaultPort)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("Error abriendo puerto gRPC en %s: %v", addr, err)
	}

	go func() {
		fmt.Printf("🔌 Servidor gRPC escuchando en %s\n", addr)
		if err := grpcServer.Serve(listener); err != nil {
			log.Printf("Servidor gRPC cerrado: %v", err)
		}
	}()

	// Simulación de prueba de evento inicial
	testMsg := &domain.Message{
		ID:              uuid.New().String(),
		ConversationID:  uuid.New().String(),
		ChannelID:       "internal-test",
		RemoteMessageID: "boot-1",
		SenderType:      domain.SenderSystem,
		Body:            "Conduit Core Engine iniciado exitosamente con gRPC activo",
		ContentType:     domain.ContentTypeText,
		Status:          domain.StatusDelivered,
		SentAt:          time.Now(),
	}
	bus.Publish(eventbus.EventMessageReceived, testMsg)

	// Manejo de señales de terminación
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	fmt.Println("⚡ Conduit Core listo. Presiona Ctrl+C para detener.")
	<-stop

	fmt.Println("\n🛑 Deteniendo ConduitOmniChat Daemon limpiamente...")
	grpcServer.GracefulStop()
	_, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
}
