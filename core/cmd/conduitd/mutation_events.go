package main

import (
	"context"
	"time"

	"github.com/cestila/ConduitOmniChat/core/pkg/domain"
	"github.com/cestila/ConduitOmniChat/core/pkg/eventbus"
	"github.com/cestila/ConduitOmniChat/core/pkg/storage"
	conduitv1 "github.com/cestila/ConduitOmniChat/sdk/conduitv1"
)

// applyDeleteEvent aplica la eliminación de un mensaje recibido desde un plugin.
// Busca por canal si está disponible, o por remote_message_id como fallback.
// No toca las conversaciones ni los contactos y nunca corrompe last_message_at.
func applyDeleteEvent(ctx context.Context, db *storage.DB, bus *eventbus.Bus, provider domain.Provider, ev *conduitv1.PluginEvent) {
	chatTarget := ev.ChatJid
	if chatTarget == "" {
		chatTarget = ev.RemoteSenderId
	}
	var channelID string
	if chatTarget != "" {
		if existingChan, _ := db.GetChannelByProviderAndRemoteID(ctx, string(provider), chatTarget); existingChan != nil {
			channelID = existingChan.ID
		}
	}
	localID, err := db.DeleteMessageByRemoteID(ctx, channelID, ev.RemoteMessageId)
	if (err != nil || localID == "") && channelID != "" {
		localID, err = db.DeleteMessageByRemoteID(ctx, "", ev.RemoteMessageId)
	}
	if err == nil && localID != "" {
		bus.Publish(eventbus.EventMessageDeleted, localID)
	}
}

// applyEditEvent aplica la edición de un mensaje recibido desde un plugin.
// Actualiza el texto en SQLite, asigna is_edited y timestamp, y emite EventMessageUpdated.
func applyEditEvent(ctx context.Context, db *storage.DB, bus *eventbus.Bus, provider domain.Provider, ev *conduitv1.PluginEvent) {
	chatTarget := ev.ChatJid
	if chatTarget == "" {
		chatTarget = ev.RemoteSenderId
	}
	var channelID string
	if chatTarget != "" {
		if existingChan, _ := db.GetChannelByProviderAndRemoteID(ctx, string(provider), chatTarget); existingChan != nil {
			channelID = existingChan.ID
		}
	}
	targetMsg, err := db.FindMessageByRemoteID(ctx, channelID, ev.RemoteMessageId)
	if (err != nil || targetMsg == nil) && channelID != "" {
		targetMsg, err = db.FindMessageByRemoteID(ctx, "", ev.RemoteMessageId)
	}
	if err == nil && targetMsg != nil {
		_ = db.UpdateMessageBody(ctx, targetMsg.ID, ev.Body)
		targetMsg.Body = ev.Body
		targetMsg.IsEdited = true
		now := time.Now()
		if ev.EditedAtUnix > 0 {
			now = time.Unix(ev.EditedAtUnix, 0)
		}
		targetMsg.EditedAt = &now
		bus.Publish(eventbus.EventMessageUpdated, targetMsg)
	}
}
