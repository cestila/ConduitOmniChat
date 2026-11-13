package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/cestila/ConduitOmniChat/core/pkg/domain"
	"github.com/cestila/ConduitOmniChat/core/pkg/storage"
)

// Verifica que el orden del servidor se conserve y que los canales de bases
// anteriores (sin posición) la reciban en la siguiente sincronización.
func TestChannelPositionFor(t *testing.T) {
	db, err := storage.NewDB(filepath.Join(t.TempDir(), "positions.db"))
	if err != nil {
		t.Fatalf("error creando base: %v", err)
	}
	defer db.Close()
	ctx := context.Background()

	// Canal legacy: existe desde antes de la columna position.
	legacy := &domain.Conversation{
		ID:              "legacy-chan",
		IsGroup:         true,
		Title:           "#viejo",
		LastMessageAt:   time.Unix(1000, 0),
		ServerID:        "guild-1",
		ServerName:      "Servidor",
		ParentChannelID: "cat-1",
		ChannelType:     "text",
	}
	if err := db.EnsureConversation(ctx, legacy); err != nil {
		t.Fatalf("error guardando el canal legacy: %v", err)
	}

	if got := channelPositionFor(ctx, db, "legacy-chan", "guild-1"); got != 1 {
		t.Fatalf("un canal sin posición debería recibir la 1, obtuve %d", got)
	}

	// Un canal nuevo continúa la secuencia.
	if got := channelPositionFor(ctx, db, "nuevo-chan", "guild-1"); got != 1 {
		t.Fatalf("con una sola fila en 0, la siguiente posición libre es 1, obtuve %d", got)
	}

	// Con posiciones ya asignadas se respeta la existente y no se reordena.
	assigned := &domain.Conversation{
		ID: "chan-ordenado", IsGroup: true, Title: "#ordenado", LastMessageAt: time.Unix(1000, 0),
		ServerID: "guild-1", ServerName: "Servidor", ParentChannelID: "cat-1", ChannelType: "text", Position: 7,
	}
	if err := db.EnsureConversation(ctx, assigned); err != nil {
		t.Fatalf("error guardando el canal ordenado: %v", err)
	}
	if got := channelPositionFor(ctx, db, "chan-ordenado", "guild-1"); got != 7 {
		t.Fatalf("una posición ya asignada no debe cambiar, obtuve %d", got)
	}

	// Un canal nuevo después de las posiciones 1 y 7 recibe la 8.
	if got := channelPositionFor(ctx, db, "otro-chan", "guild-1"); got != 8 {
		t.Fatalf("se esperaba la posición 8, obtuve %d", got)
	}
}
