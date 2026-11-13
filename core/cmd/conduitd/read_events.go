package main

import (
	"context"
	"log"
	"strconv"
	"strings"

	"github.com/cestila/ConduitOmniChat/core/pkg/domain"
	"github.com/cestila/ConduitOmniChat/core/pkg/eventbus"
	"github.com/cestila/ConduitOmniChat/core/pkg/storage"
	conduitv1 "github.com/cestila/ConduitOmniChat/sdk/conduitv1"
)

// applyReadEvent aplica un "messages.read" (lectura hecha desde el celular) sobre el canal y la
// conversación EXISTENTES. Nunca crea contactos, canales ni conversaciones y no toca last_message_at.
func applyReadEvent(ctx context.Context, db *storage.DB, bus *eventbus.Bus, pluginName string, provider domain.Provider, ev *conduitv1.PluginEvent) {
	chatTarget := ev.ChatJid
	if chatTarget == "" {
		chatTarget = ev.RemoteSenderId
	}
	if chatTarget == "" || strings.Contains(chatTarget, "broadcast") {
		return
	}

	channel, err := db.GetChannelByProviderAndRemoteID(ctx, string(provider), chatTarget)
	if err != nil || channel == nil {
		log.Printf("ℹ️ Lectura descartada: no existe el canal %q (%s)", chatTarget, pluginName)
		return
	}

	convID := resolveReadConversation(ctx, db, pluginName, channel, chatTarget, ev)
	if convID == "" {
		log.Printf("ℹ️ Lectura descartada: no existe conversación para %q (%s)", chatTarget, pluginName)
		return
	}

	var remoteIDs []string
	for _, part := range strings.Split(ev.RemoteMessageId, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			remoteIDs = append(remoteIDs, trimmed)
		}
	}

	switch {
	case len(remoteIDs) == 0:
		_ = db.MarkMessagesAsRead(ctx, convID)
	case len(remoteIDs) == 1 && isNumericID(remoteIDs[0]):
		// Telegram informa un max_id: se marca todo hasta ese mensaje.
		_, _ = db.MarkMessagesAsReadUpToRemoteID(ctx, channel.ID, remoteIDs[0])
	default:
		_, _ = db.MarkMessagesAsReadByRemoteIDs(ctx, channel.ID, remoteIDs)
	}

	unread, err := db.UnreadCount(ctx, convID)
	if err != nil {
		log.Printf("⚠️ No se pudo contar los no leídos de %s: %v", convID, err)
	}
	bus.Publish(eventbus.EventConversationRead, &conduitv1.ConversationReadEvent{
		ConversationId:   convID,
		RemoteMessageIds: remoteIDs,
		MaxRemoteId:      ev.RemoteMessageId,
		UnreadCount:      unread,
	})
}

func isNumericID(id string) bool {
	_, err := strconv.Atoi(id)
	return err == nil
}

// resolveReadConversation busca la conversación existente del chat; devuelve "" si no hay ninguna.
func resolveReadConversation(ctx context.Context, db *storage.DB, pluginName string, channel *domain.ContactChannel, chatTarget string, ev *conduitv1.PluginEvent) string {
	candidate := channel.ContactID
	if strings.Contains(chatTarget, "@g.us") || ev.ServerId != "" {
		candidate = pluginName + ":" + chatTarget
	}
	if candidate != "" {
		if conv, err := db.GetConversationByID(ctx, candidate); err == nil && conv != nil {
			return candidate
		}
	}
	convID, err := db.ConversationIDForChannel(ctx, channel.ID)
	if err != nil {
		return ""
	}
	return convID
}
