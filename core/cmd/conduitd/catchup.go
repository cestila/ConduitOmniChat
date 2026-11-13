package main

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/cestila/ConduitOmniChat/core/pkg/domain"
	"github.com/cestila/ConduitOmniChat/core/pkg/pluginmgr"
	"github.com/cestila/ConduitOmniChat/core/pkg/storage"
	conduitv1 "github.com/cestila/ConduitOmniChat/sdk/conduitv1"
)

// catchUpRecentMessages recupera los mensajes que llegaron mientras el daemon
// estaba apagado en las conversaciones más recientes del proveedor.
//
// Cada plugin decide qué significa "última página": Telegram consulta el
// historial del chat y WhatsApp responde que no aplica, porque su protocolo ya
// reenvía los mensajes pendientes al reconectar. Resincronizar es idempotente:
// la base tiene UNIQUE(channel_id, remote_message_id).
// lastCatchUp evita repetir la recuperación si el canal se reconecta en bucle.
var lastCatchUp sync.Map

func catchUpRecentMessages(db *storage.DB, plugins *pluginmgr.Manager, provider domain.Provider, maxConversations int) {
	if plugins == nil || maxConversations <= 0 {
		maxConversations = 15
	}

	if last, ok := lastCatchUp.Load(provider); ok {
		if t, isTime := last.(time.Time); isTime && time.Since(t) < 30*time.Second {
			return
		}
	}
	lastCatchUp.Store(provider, time.Now())

	inst, ok := plugins.GetInstance(string(provider))
	if !ok {
		return
	}

	// Deja que el cliente termine de asentarse antes de pedirle historial.
	time.Sleep(2 * time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	convs, err := db.ListConversations(ctx, 200, 0)
	if err != nil {
		log.Printf("⚠️ no se pudieron listar conversaciones para recuperar historial de %s: %v", provider, err)
		return
	}

	attempted, synced, failed := 0, 0, 0
	for _, conv := range convs {
		if attempted >= maxConversations {
			break
		}
		if conv.GetActiveChannelId() == "" {
			continue
		}

		channel, err := db.GetChannelByID(ctx, conv.GetActiveChannelId())
		if err != nil || channel == nil {
			continue
		}
		if channel.Provider != provider || channel.RemoteID == "" {
			continue
		}

		reqCtx, reqCancel := context.WithTimeout(ctx, 15*time.Second)
		resp, err := inst.RPC.SyncChatHistory(reqCtx, &conduitv1.SyncChatHistoryRequest{
			RemoteChatId: channel.RemoteID,
			Limit:        50,
		})
		reqCancel()

		attempted++
		if err != nil || !resp.GetSuccess() {
			// El proveedor puede no soportar pedir la última página: se resume al final.
			failed++
			continue
		}
		synced++
	}

	if attempted == 0 {
		return
	}
	log.Printf("✅ Recuperación de historial de %s: %d/%d conversaciones", provider, synced, attempted)
	if failed > 0 {
		log.Printf("ℹ️ %s no recuperó historial en %d conversaciones (el proveedor entrega los pendientes por su cuenta o no soporta la consulta)", provider, failed)
	}
}
