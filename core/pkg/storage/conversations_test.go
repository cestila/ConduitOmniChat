package storage

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/cestila/ConduitOmniChat/core/pkg/domain"
)

// Verifica que el orden de canal que publica el servidor se persista y sobreviva a la migración,
// para que la GUI pueda listar categorías y canales en el orden del servidor.
func TestConversationPositionPersists(t *testing.T) {
	db, err := NewDB(filepath.Join(t.TempDir(), "positions.db"))
	if err != nil {
		t.Fatalf("error creando base: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	// Primera sincronización: los canales llegan en el orden del servidor.
	for i, id := range []string{"chan-general", "chan-dev", "chan-voice"} {
		conv := newChannelConversation(id, "guild-1", "cat-a")
		conv.Position = i + 1
		if err := db.EnsureConversation(ctx, conv); err != nil {
			t.Fatalf("error guardando %s: %v", id, err)
		}
	}

	// Una resincronización no debe reordenar los canales existentes.
	convs, err := db.ListConversations(ctx, 50, 0)
	if err != nil {
		t.Fatalf("error listando conversaciones: %v", err)
	}
	byID := map[string]int32{}
	for _, c := range convs {
		byID[c.Id] = c.Position
	}
	if byID["chan-general"] != 1 || byID["chan-dev"] != 2 || byID["chan-voice"] != 3 {
		t.Fatalf("se esperaba el orden del servidor 1,2,3; obtuve %+v", byID)
	}

	// El siguiente canal nuevo del mismo servidor continúa la secuencia.
	next, err := db.NextChannelPosition(ctx, "guild-1")
	if err != nil {
		t.Fatalf("error calculando la posición siguiente: %v", err)
	}
	if next != 4 {
		t.Fatalf("se esperaba la posición 4, obtuve %d", next)
	}

	// Un servidor distinto tiene su propia secuencia.
	other, err := db.NextChannelPosition(ctx, "guild-2")
	if err != nil {
		t.Fatalf("error calculando la posición de otro servidor: %v", err)
	}
	if other != 1 {
		t.Fatalf("un servidor nuevo debería empezar en 1, obtuve %d", other)
	}

	// Re-guardar sin posición conserva la ya asignada.
	again := newChannelConversation("chan-dev", "guild-1", "cat-a")
	if err := db.EnsureConversation(ctx, again); err != nil {
		t.Fatalf("error re-guardando: %v", err)
	}
	stored, err := db.GetConversationByID(ctx, "chan-dev")
	if err != nil {
		t.Fatalf("error leyendo el canal: %v", err)
	}
	if stored.Position != 2 {
		t.Fatalf("la resincronización no debe reordenar: se esperaba 2, obtuve %d", stored.Position)
	}
}

// El canal sticky de un chat 1 a 1 solo cambia cuando un mensaje lo usa:
// una resincronización de contactos no debe pisar la última cuenta usada.
func TestEnsureConversationKeepsStickyChannel(t *testing.T) {
	db, err := NewDB(filepath.Join(t.TempDir(), "sticky.db"))
	if err != nil {
		t.Fatalf("error creando base: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	now := time.Now()
	contactID := "contact-sticky"
	waChannelID := "channel-wa"
	tgChannelID := "channel-tg"

	if err := db.SaveContact(ctx, &domain.Contact{
		ID:          contactID,
		DisplayName: "Persona",
		CreatedAt:   now,
		UpdatedAt:   now,
	}); err != nil {
		t.Fatalf("error guardando contacto: %v", err)
	}

	if err := db.EnsureConversation(ctx, &domain.Conversation{
		ID:              contactID,
		ContactID:       &contactID,
		Title:           "Persona",
		ActiveChannelID: &waChannelID,
	}); err != nil {
		t.Fatalf("error creando conversación: %v", err)
	}

	// Un contact.sync de otra cuenta no cambia la última usada.
	if err := db.EnsureConversation(ctx, &domain.Conversation{
		ID:              contactID,
		ContactID:       &contactID,
		Title:           "Persona",
		ActiveChannelID: &tgChannelID,
	}); err != nil {
		t.Fatalf("error resincronizando conversación: %v", err)
	}

	conversation, err := db.GetConversationByID(ctx, contactID)
	if err != nil {
		t.Fatalf("error leyendo conversación: %v", err)
	}
	if conversation.ActiveChannelId != waChannelID {
		t.Fatalf("el sticky debió seguir en %s, obtuvo %s", waChannelID, conversation.ActiveChannelId)
	}

	// Un mensaje sí actualiza la última cuenta usada.
	if err := db.SetConversationActiveChannel(ctx, contactID, tgChannelID); err != nil {
		t.Fatalf("error actualizando sticky: %v", err)
	}
	conversation, err = db.GetConversationByID(ctx, contactID)
	if err != nil {
		t.Fatalf("error releyendo conversación: %v", err)
	}
	if conversation.ActiveChannelId != tgChannelID {
		t.Fatalf("el sticky debió pasar a %s, obtuvo %s", tgChannelID, conversation.ActiveChannelId)
	}
}

// Una base anterior a la unificación puede tener dos hilos 1 a 1 para el mismo
// contacto. Al reabrirla, el arranque debe dejar un solo hilo con todos los
// mensajes y el sticky en el canal del último mensaje.
func TestStartupConsolidatesLegacyDMConversations(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy_dm_threads.db")

	db, err := NewDB(dbPath)
	if err != nil {
		t.Fatalf("error creando base: %v", err)
	}

	ctx := context.Background()
	now := time.Now()
	contactID := "contact-1"
	waChannelID := "channel-wa"
	tgChannelID := "channel-tg"

	if err := db.SaveContact(ctx, &domain.Contact{
		ID:          contactID,
		DisplayName: "Persona Unificada",
		Phone:       "+5491100006666",
		CreatedAt:   now,
		UpdatedAt:   now,
	}); err != nil {
		t.Fatalf("error guardando contacto: %v", err)
	}
	if err := db.SaveChannel(ctx, &domain.ContactChannel{
		ID:        waChannelID,
		ContactID: contactID,
		Provider:  domain.ProviderWhatsApp,
		RemoteID:  "5491100006666@s.whatsapp.net",
	}); err != nil {
		t.Fatalf("error guardando canal WhatsApp: %v", err)
	}
	if err := db.SaveChannel(ctx, &domain.ContactChannel{
		ID:        tgChannelID,
		ContactID: contactID,
		Provider:  domain.ProviderTelegram,
		RemoteID:  "+123456",
	}); err != nil {
		t.Fatalf("error guardando canal Telegram: %v", err)
	}

	// Dos hilos legacy con IDs distintos del contacto, como dejaba el merge viejo.
	for _, legacy := range []struct {
		id      string
		channel string
	}{
		{"legacy-wa", waChannelID},
		{"legacy-tg", tgChannelID},
	} {
		channelID := legacy.channel
		if err := db.EnsureConversation(ctx, &domain.Conversation{
			ID:              legacy.id,
			ContactID:       &contactID,
			IsGroup:         false,
			Title:           "Persona",
			ActiveChannelID: &channelID,
		}); err != nil {
			t.Fatalf("error guardando conversación %s: %v", legacy.id, err)
		}
	}

	legacyMessages := []struct {
		id             string
		conversationID string
		channelID      string
		sentAt         time.Time
	}{
		{"msg-wa", "legacy-wa", waChannelID, time.Unix(1000, 0)},
		{"msg-tg", "legacy-tg", tgChannelID, time.Unix(2000, 0)},
	}
	for _, m := range legacyMessages {
		if err := db.SaveMessage(ctx, &domain.Message{
			ID:              m.id,
			ConversationID:  m.conversationID,
			ChannelID:       m.channelID,
			RemoteMessageID: m.id,
			SenderType:      domain.SenderContact,
			Body:            m.id,
			ContentType:     domain.ContentTypeText,
			Status:          domain.StatusDelivered,
			SentAt:          m.sentAt,
		}); err != nil {
			t.Fatalf("error guardando mensaje %s: %v", m.id, err)
		}
	}

	if err := db.Close(); err != nil {
		t.Fatalf("error cerrando base: %v", err)
	}

	// Reabrir dispara la migración y la consolidación de arranque.
	reopened, err := NewDB(dbPath)
	if err != nil {
		t.Fatalf("error reabriendo base: %v", err)
	}
	defer reopened.Close()

	conversationIDs, err := reopened.ListDMConversationIDsByContact(ctx, contactID)
	if err != nil {
		t.Fatalf("error listando conversaciones: %v", err)
	}
	if len(conversationIDs) != 1 || conversationIDs[0] != contactID {
		t.Fatalf("se esperaba un solo hilo con id %s, obtenidos %v", contactID, conversationIDs)
	}

	messages, err := reopened.GetMessagesByConversation(ctx, contactID, 10, "")
	if err != nil {
		t.Fatalf("error leyendo mensajes: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("se esperaban 2 mensajes preservados, obtenidos %d", len(messages))
	}

	conversation, err := reopened.GetConversationByID(ctx, contactID)
	if err != nil {
		t.Fatalf("error leyendo conversación unificada: %v", err)
	}
	if conversation.ActiveChannelId != tgChannelID {
		t.Fatalf("el sticky debió quedar en %s, obtuvo %s", tgChannelID, conversation.ActiveChannelId)
	}
	if conversation.LastMessageAtUnix != 2000 {
		t.Fatalf("last_message_at debió ser 2000, obtuvo %d", conversation.LastMessageAtUnix)
	}

	// Idempotente: repetir la consolidación no cambia nada.
	if err := reopened.ConsolidateDuplicateDMConversations(ctx); err != nil {
		t.Fatalf("la segunda consolidación falló: %v", err)
	}
	again, err := reopened.GetMessagesByConversation(ctx, contactID, 10, "")
	if err != nil {
		t.Fatalf("error releyendo mensajes: %v", err)
	}
	if len(again) != 2 {
		t.Fatalf("la consolidación no es idempotente: se esperaban 2 mensajes, obtenidos %d", len(again))
	}
}

// Verifica que una base existente sin la columna position migre conservando las conversaciones.
func TestLegacyConversationsTableMigratesPositionColumn(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy_conversations.db")

	raw, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("error creando base legacy: %v", err)
	}
	legacySchema := `
	CREATE TABLE conversations (
		id TEXT PRIMARY KEY,
		contact_id TEXT,
		is_group BOOLEAN DEFAULT FALSE,
		title TEXT NOT NULL,
		last_message_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		active_channel_id TEXT,
		server_id TEXT,
		server_name TEXT,
		parent_channel_id TEXT,
		channel_type TEXT,
		avatar_url TEXT
	);
	INSERT INTO conversations (id, is_group, title, server_id, server_name, parent_channel_id, channel_type)
		VALUES ('legacy-chan', 1, '#viejo', 'guild-old', 'Servidor Viejo', 'cat-old', 'text');
	`
	if _, err := raw.Exec(legacySchema); err != nil {
		raw.Close()
		t.Fatalf("error sembrando base legacy: %v", err)
	}
	raw.Close()

	db, err := NewDB(dbPath)
	if err != nil {
		t.Fatalf("error migrando base legacy: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	legacy, err := db.GetConversationByID(ctx, "legacy-chan")
	if err != nil {
		t.Fatalf("la conversación legacy se perdió en la migración: %v", err)
	}
	if legacy.Title != "#viejo" || legacy.ServerId != "guild-old" {
		t.Fatalf("se esperaba conservar los datos legacy, obtuve %+v", legacy)
	}
	if legacy.Position != 0 {
		t.Fatalf("una conversación legacy no debería tener posición, obtuve %d", legacy.Position)
	}

	next, err := db.NextChannelPosition(ctx, "guild-old")
	if err != nil {
		t.Fatalf("error calculando posición en base migrada: %v", err)
	}
	if next != 1 {
		t.Fatalf("se esperaba empezar en 1 tras migrar, obtuve %d", next)
	}
}

func newChannelConversation(id, serverID, categoryID string) *domain.Conversation {
	return &domain.Conversation{
		ID:              id,
		IsGroup:         true,
		Title:           id,
		LastMessageAt:   time.Unix(1000, 0),
		ServerID:        serverID,
		ServerName:      "Servidor",
		ParentChannelID: categoryID,
		ChannelType:     "text",
	}
}

func TestGetPinnedMessagesByConversationWithEmptyMetadata(t *testing.T) {
	db, err := NewDB(filepath.Join(t.TempDir(), "pinned_test.db"))
	if err != nil {
		t.Fatalf("error creando base: %v", err)
	}
	defer db.Close()
	ctx := context.Background()

	contactID := "contact-pin"
	_, _ = db.conn.Exec("INSERT INTO contacts (id, display_name) VALUES (?, ?)", contactID, "Test")
	channelID := "chan-pin"
	_, _ = db.conn.Exec("INSERT INTO contact_channels (id, contact_id, provider, remote_id) VALUES (?, ?, ?, ?)",
		channelID, contactID, "whatsapp", "123456@s.whatsapp.net")
	convID := "conv-pin"
	_, _ = db.conn.Exec("INSERT INTO conversations (id, contact_id, title) VALUES (?, ?, ?)",
		convID, contactID, "Test Chat")

	// 1. Mensaje normal con metadata vacía '' (el caso problemático para json_extract)
	msgNormal := &domain.Message{
		ID:              "msg-1",
		ConversationID:  convID,
		ChannelID:       channelID,
		RemoteMessageID: "rem-1",
		SenderType:      domain.SenderContact,
		Body:            "Mensaje normal",
		ContentType:     domain.ContentTypeText,
		Status:          domain.StatusDelivered,
		SentAt:          time.Now().Add(-10 * time.Minute),
	}
	if err := db.SaveMessage(ctx, msgNormal); err != nil {
		t.Fatalf("error guardando msgNormal: %v", err)
	}

	// 2. Mensaje fijado
	msgPinned := &domain.Message{
		ID:              "msg-2",
		ConversationID:  convID,
		ChannelID:       channelID,
		RemoteMessageID: "rem-2",
		SenderType:      domain.SenderContact,
		Body:            "Mensaje fijado importante",
		ContentType:     domain.ContentTypeText,
		Status:          domain.StatusDelivered,
		SentAt:          time.Now(),
		IsPinned:        true,
	}
	if err := db.SaveMessage(ctx, msgPinned); err != nil {
		t.Fatalf("error guardando msgPinned: %v", err)
	}

	// 3. Consultar mensajes fijados
	pinnedList, err := db.GetPinnedMessagesByConversation(ctx, convID)
	if err != nil {
		t.Fatalf("GetPinnedMessagesByConversation falló (posible malformed JSON en metadata vacía): %v", err)
	}
	if len(pinnedList) != 1 {
		t.Fatalf("se esperaba 1 mensaje fijado, obtenidos %d", len(pinnedList))
	}
	if pinnedList[0].Id != "msg-2" || pinnedList[0].Body != "Mensaje fijado importante" {
		t.Errorf("mensaje fijado devuelto inesperado: %+v", pinnedList[0])
	}
}

// Verifica que ListConversations incluya todos los canales de un servidor aunque tengan 0 mensajes.
func TestListConversationsIncludesAllServerSiblingChannels(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "sibling_channels.db")
	db, err := NewDB(dbPath)
	if err != nil {
		t.Fatalf("error inicializando base: %v", err)
	}
	defer db.Close()
	ctx := context.Background()

	// 1. Canal activo del servidor (con mensaje)
	_ = db.EnsureConversation(ctx, &domain.Conversation{
		ID:              "tg:server_topic_2",
		Title:           "Debates",
		ServerID:        "server-1",
		ServerName:      "Mi Servidor",
		ParentChannelID: "server-1",
		ChannelType:     "forum",
		LastMessageAt:   time.Now(),
	})
	_ = db.SaveMessage(ctx, &domain.Message{
		ID:              "msg-active",
		ConversationID:  "tg:server_topic_2",
		ChannelID:       "chan-1",
		RemoteMessageID: "100",
		SenderType:      domain.SenderContact,
		Body:            "Hola en debates",
		ContentType:     domain.ContentTypeText,
		Status:          domain.StatusDelivered,
		SentAt:          time.Now(),
	})

	// 2. Canal General del servidor (sin mensajes, msg_count=0)
	_ = db.EnsureConversation(ctx, &domain.Conversation{
		ID:              "tg:server_topic_1",
		Title:           "General",
		ServerID:        "server-1",
		ServerName:      "Mi Servidor",
		ParentChannelID: "server-1",
		ChannelType:     "forum",
		LastMessageAt:   time.Now().Add(-24 * time.Hour),
	})

	// 3. Consultar con limit=1 (solo entraría el activo si no incluyera hermanos)
	list, err := db.ListConversations(ctx, 1, 0)
	if err != nil {
		t.Fatalf("ListConversations falló: %v", err)
	}

	foundGeneral := false
	foundDebates := false
	for _, c := range list {
		if c.Id == "tg:server_topic_1" && c.Title == "General" {
			foundGeneral = true
		}
		if c.Id == "tg:server_topic_2" && c.Title == "Debates" {
			foundDebates = true
		}
	}

	if !foundGeneral || !foundDebates {
		t.Fatalf("se esperaba encontrar tanto General como Debates, obtuve: %+v", list)
	}
}
