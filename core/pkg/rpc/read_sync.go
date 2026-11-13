package rpc

import (
	"context"
	"log"
	"time"

	"github.com/cestila/ConduitOmniChat/core/pkg/domain"
	"github.com/cestila/ConduitOmniChat/core/pkg/pluginmgr"
	"github.com/cestila/ConduitOmniChat/core/pkg/storage"
	conduitv1 "github.com/cestila/ConduitOmniChat/sdk/conduitv1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// readSyncTimeout acota cada llamada MarkRead al plugin.
const readSyncTimeout = 7 * time.Second

// FlushReadSync propaga al proveedor los leídos pendientes de la conversación, una request por canal.
// Solo limpia el pendiente cuando el plugin confirma (o no soporta la operación); ante cualquier otro
// fallo lo deja pendiente para reintentar.
func FlushReadSync(ctx context.Context, db *storage.DB, plugins *pluginmgr.Manager, convID string) {
	if db == nil || plugins == nil {
		return
	}
	refs, err := db.PendingReadSync(ctx, convID)
	if err != nil {
		log.Printf("read-sync: error consultando pendientes de %s: %v", convID, err)
		return
	}

	// Agrupar por canal preservando el orden cronológico.
	var order []string
	groups := make(map[string][]storage.ReadSyncRef)
	for _, ref := range refs {
		if _, seen := groups[ref.ChannelID]; !seen {
			order = append(order, ref.ChannelID)
		}
		groups[ref.ChannelID] = append(groups[ref.ChannelID], ref)
	}

	for _, channelID := range order {
		group := groups[channelID]
		ch, err := db.GetChannelByID(ctx, channelID)
		if err != nil || ch == nil {
			log.Printf("read-sync: canal %s no disponible: %v", channelID, err)
			continue
		}
		inst, ok := plugins.GetInstance(providerName(ch.Provider))
		if !ok || inst == nil || inst.RPC == nil {
			continue // el plugin no está activo: queda pendiente
		}

		ids := make([]string, len(group))
		senders := make([]string, len(group))
		for i, ref := range group {
			ids[i] = ref.RemoteMessageID
			senders[i] = ref.SenderRemoteID
		}

		// Remitente común (compatibilidad con plugins que no leen remote_sender_ids).
		common := senders[0]
		for _, s := range senders {
			if s != common {
				common = ""
				break
			}
		}

		callCtx, cancel := context.WithTimeout(ctx, readSyncTimeout)
		resp, err := inst.RPC.MarkRead(callCtx, &conduitv1.PluginMarkReadRequest{
			RemoteChatId:     ch.RemoteID,
			RemoteMessageIds: ids,
			RemoteSenderIds:  senders,
			MaxRemoteId:      ids[len(ids)-1],
			SenderJid:        common,
		})
		cancel()

		switch {
		case err == nil && resp != nil && resp.Success:
		case err != nil && status.Code(err) == codes.Unimplemented:
			// El proveedor no puede sincronizar: no reintentar indefinidamente.
		default:
			reason := ""
			if resp != nil {
				reason = resp.ErrorMessage
			}
			log.Printf("read-sync: fallo propagando leídos de %s (%s): err=%v plugin=%q", convID, channelID, err, reason)
			continue
		}
		if err := db.ClearReadSyncPending(ctx, channelID, ids); err != nil {
			log.Printf("read-sync: error limpiando pendientes de %s: %v", channelID, err)
		}
	}
}

// FlushPendingReadSyncForProvider reintenta los leídos pendientes de todas las conversaciones con canales del proveedor.
func FlushPendingReadSyncForProvider(ctx context.Context, db *storage.DB, plugins *pluginmgr.Manager, provider domain.Provider) {
	if db == nil {
		return
	}
	convs, err := db.ConversationsWithPendingReadSync(ctx, provider)
	if err != nil {
		log.Printf("read-sync: error listando pendientes de %s: %v", provider, err)
		return
	}
	for _, convID := range convs {
		FlushReadSync(ctx, db, plugins, convID)
	}
}
