package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

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

// pathWithin informa si path es root o está bajo root (comparación por componentes, no por
// prefijo textual: `/x/media-evil` no está dentro de `/x/media`).
func pathWithin(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil || filepath.IsAbs(rel) {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// removeMediaFiles borra del disco los archivos de media de adjuntos ya eliminados de la base.
// Solo toca archivos regulares dentro de mediaDir: resuelve los enlaces simbólicos de mediaDir y
// del directorio padre de cada archivo y exige que sigan dentro (un subdirectorio simbólico no
// puede sacar el borrado fuera). Si la resolución falla o escapa, omite el archivo sin ruido;
// ignora los ya ausentes y registra cualquier otro error.
func removeMediaFiles(mediaDir string, paths []string) {
	if mediaDir == "" {
		return
	}
	root, err := filepath.EvalSymlinks(filepath.Clean(mediaDir))
	if err != nil {
		return
	}
	for _, p := range paths {
		clean := filepath.Clean(p)
		parent, err := filepath.EvalSymlinks(filepath.Dir(clean))
		if err != nil || !pathWithin(root, parent) {
			continue
		}
		target := filepath.Join(parent, filepath.Base(clean))
		info, err := os.Lstat(target)
		if err != nil {
			if !os.IsNotExist(err) {
				log.Printf("⚠️ channel.reconcile: no se pudo inspeccionar %s: %v", target, err)
			}
			continue
		}
		if !info.Mode().IsRegular() {
			continue
		}
		if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
			log.Printf("⚠️ channel.reconcile: no se pudo borrar %s: %v", target, err)
		}
	}
}

// handleChannelReconcile aplica un evento channel.reconcile sobre un foro: primero reubica en el tema
// General los pseudo temas con nombre de grupo que dejó el fallback antiguo del plugin, después borra
// las conversaciones de tema que ya no existen en el proveedor (salvo las posteriores al snapshot,
// ev.SentAtUnix) y sus archivos de media dentro de mediaDir, y avisa a los clientes. Devuelve los ids
// de conversación eliminados (incluidos los reubicados, cuya fila desaparece). Ante una lista inválida
// o sin SentAtUnix no hace nada.
func handleChannelReconcile(ctx context.Context, db *storage.DB, bus *eventbus.Bus, plugCfg pluginConfig, mediaDir string, ev *conduitv1.PluginEvent) []string {
	serverID := ev.ServerId
	if ev.SentAtUnix <= 0 {
		// Sin la hora a la que se obtuvo la lista viva no se puede saber si un tema es posterior: no se borra nada.
		log.Printf("⚠️ channel.reconcile de %s (%s) ignorado: falta sent_at_unix (hora del snapshot)", plugCfg.Name, serverID)
		return nil
	}
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
	// Reubicar los pseudo temas con nombre de grupo y borrar los temas ausentes van en la misma
	// transacción: o se aplican los dos pasos, o no se toca nada.
	res, err := db.ReconcileForumTopics(ctx, plugCfg.Name+":", string(plugCfg.Provider), serverID, time.Unix(ev.SentAtUnix, 0), isCandidate, live)
	deleted := append(append([]string{}, res.DeletedIDs...), res.RehomedIDs...)
	if err != nil {
		log.Printf("⚠️ channel.reconcile de %s (%s): error borrando temas: %v", plugCfg.Name, serverID, err)
		return nil
	}
	removeMediaFiles(mediaDir, res.MediaPaths)
	for _, convID := range deleted {
		bus.Publish(eventbus.EventConversationDeleted, eventbus.ConversationDeleted{ConversationID: convID, ServerID: serverID})
	}
	if len(res.RehomedIDs) > 0 {
		log.Printf("🔧 channel.reconcile %s/%s: %d conversación(es) con nombre de grupo movidas a General", plugCfg.Name, serverID, len(res.RehomedIDs))
	}
	if len(res.DeletedIDs) > 0 {
		log.Printf("🧹 channel.reconcile %s/%s: %d tema(s) eliminados", plugCfg.Name, serverID, len(res.DeletedIDs))
	}
	return deleted
}

// routePluginEvent despacha los eventos de plugin que gestiona este archivo. Devuelve true si lo consumió.
func routePluginEvent(ctx context.Context, db *storage.DB, bus *eventbus.Bus, plugCfg pluginConfig, mediaDir string, ev *conduitv1.PluginEvent) bool {
	switch ev.EventType {
	case "channel.reconcile":
		// Lista viva de temas de un foro: se borran los temas locales que ya no existen.
		handleChannelReconcile(ctx, db, bus, plugCfg, mediaDir, ev)
		return true
	}
	return false
}
