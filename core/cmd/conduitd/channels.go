package main

import (
	"context"

	"github.com/cestila/ConduitOmniChat/core/pkg/storage"
)

// channelPositionFor decide qué posición guardar para un canal.
// Respeta la posición ya asignada y calcula una nueva cuando el canal es nuevo
// o cuando viene de una base anterior a la columna position (valor 0): así el
// orden del servidor también alcanza a los canales ya sincronizados.
func channelPositionFor(ctx context.Context, db *storage.DB, convID, serverID string) int {
	existing, err := db.GetConversationByID(ctx, convID)
	if err == nil && existing != nil && existing.Position > 0 {
		return int(existing.Position)
	}
	next, err := db.NextChannelPosition(ctx, serverID)
	if err != nil {
		return 0
	}
	return int(next)
}
