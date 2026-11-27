package main

import (
	"context"
	"log"
	"strconv"
	"strings"

	"github.com/cestila/ConduitOmniChat/core/pkg/eventbus"
	"github.com/cestila/ConduitOmniChat/core/pkg/storage"
	conduitv1 "github.com/cestila/ConduitOmniChat/sdk/conduitv1"
)

const topicMarker = "_topic_"

// topicNumber devuelve N si remoteID tiene la forma exacta `<serverID>_topic_<N>` con N numérico.
func topicNumber(serverID, remoteID string) (int64, bool) {
	rest, ok := strings.CutPrefix(remoteID, serverID+topicMarker)
	if !ok || rest == "" {
		return 0, false
	}
	for _, r := range rest {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	n, err := strconv.ParseInt(rest, 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

// parseReconcileBody valida el evento channel.reconcile y devuelve el conjunto de topics vivos.
// Rechaza (ok=false) un server vacío, una lista vacía o cualquier id que no sea `<ServerId>_topic_<N>`.
func parseReconcileBody(serverID, body string) (live map[string]bool, ok bool) {
	if serverID == "" {
		return nil, false
	}
	live = map[string]bool{}
	for _, line := range strings.Split(body, "\n") {
		id := strings.TrimSpace(line)
		if id == "" {
			continue
		}
		if _, valid := topicNumber(serverID, id); !valid {
			return nil, false
		}
		live[id] = true
	}
	if len(live) == 0 {
		return nil, false
	}
	return live, true
}

// handleChannelReconcile aplica un evento channel.reconcile: borra las conversaciones de tema
// de ese servidor que ya no existen en el proveedor y avisa a los clientes. Devuelve los ids
// de conversación borrados. Ante una lista inválida no hace nada.
func handleChannelReconcile(ctx context.Context, db *storage.DB, bus *eventbus.Bus, plugCfg pluginConfig, ev *conduitv1.PluginEvent) []string {
	serverID := ev.ServerId
	live, ok := parseReconcileBody(serverID, ev.Body)
	if !ok {
		log.Printf("⚠️ channel.reconcile de %s ignorado: servidor %q o lista de temas inválida/vacía", plugCfg.Name, serverID)
		return nil
	}
	// General (tema 1) no se puede borrar en Telegram: nunca es candidato.
	isCandidate := func(remoteID string) bool {
		n, valid := topicNumber(serverID, remoteID)
		return valid && n != 1
	}
	deleted, err := db.DeleteConversationsAbsentFrom(ctx, plugCfg.Name+":", string(plugCfg.Provider), serverID, isCandidate, live)
	if err != nil {
		log.Printf("⚠️ channel.reconcile de %s (%s): error borrando temas: %v", plugCfg.Name, serverID, err)
		return nil
	}
	for _, convID := range deleted {
		bus.Publish(eventbus.EventConversationDeleted, eventbus.ConversationDeleted{ConversationID: convID, ServerID: serverID})
	}
	if len(deleted) > 0 {
		log.Printf("🧹 channel.reconcile %s/%s: %d tema(s) eliminados", plugCfg.Name, serverID, len(deleted))
	}
	return deleted
}
