package main

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cestila/ConduitOmniChat/core/pkg/domain"
	"github.com/cestila/ConduitOmniChat/core/pkg/eventbus"
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

type syncProgressPayload struct {
	Provider  string `json:"provider"`
	Completed int    `json:"completed"`
	Total     int    `json:"total"`
	Status    string `json:"status"`
}

func publishSyncProgress(bus *eventbus.Bus, provider domain.Provider, completed, total int, status string) {
	if bus == nil {
		return
	}
	raw, err := json.Marshal(syncProgressPayload{
		Provider:  string(provider),
		Completed: completed,
		Total:     total,
		Status:    status,
	})
	if err != nil {
		return
	}
	bus.Publish(eventbus.EventSyncProgress, string(raw))
}

func catchUpRecentMessages(db *storage.DB, plugins *pluginmgr.Manager, bus *eventbus.Bus, provider domain.Provider, maxConversations int) {
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

	// Breve espera para que el cliente termine de asentarse antes de pedir historial.
	time.Sleep(500 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	convs, err := db.ListConversations(ctx, 200, 0)
	if err != nil {
		log.Printf("⚠️ no se pudieron listar conversaciones para recuperar historial de %s: %v", provider, err)
		return
	}

	var targets []string
	for _, conv := range convs {
		if len(targets) >= maxConversations {
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
		targets = append(targets, channel.RemoteID)
	}

	total := len(targets)
	if total == 0 {
		return
	}

	publishSyncProgress(bus, provider, 0, total, "syncing")

	const workerCount = 4
	jobs := make(chan string, total)
	for _, remoteID := range targets {
		jobs <- remoteID
	}
	close(jobs)

	var completed atomic.Int32
	var synced atomic.Int32
	var failed atomic.Int32
	var wg sync.WaitGroup

	workers := workerCount
	if total < workers {
		workers = total
	}
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for remoteID := range jobs {
				reqCtx, reqCancel := context.WithTimeout(ctx, 15*time.Second)
				resp, err := inst.RPC.SyncChatHistory(reqCtx, &conduitv1.SyncChatHistoryRequest{
					RemoteChatId: remoteID,
					Limit:        50,
				})
				reqCancel()

				if err != nil || !resp.GetSuccess() {
					failed.Add(1)
				} else {
					synced.Add(1)
				}
				done := int(completed.Add(1))
				publishSyncProgress(bus, provider, done, total, "syncing")
			}
		}()
	}
	wg.Wait()

	publishSyncProgress(bus, provider, total, total, "done")
	log.Printf("✅ Recuperación de historial de %s: %d/%d conversaciones", provider, synced.Load(), total)
	if failed.Load() > 0 {
		log.Printf("ℹ️ %s no recuperó historial en %d conversaciones (el proveedor entrega los pendientes por su cuenta o no soporta la consulta)", provider, failed.Load())
	}
}
