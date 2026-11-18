package pluginmgr

import (
	"context"
	"testing"
	"time"

	conduitv1 "github.com/cestila/ConduitOmniChat/sdk/conduitv1"
)

func TestTelegramPluginLifecycle(t *testing.T) {
	// Ruta al binario compilado del plugin
	binaryPath := builtPlugin(t, "telegram")

	mgr := NewManager()
	defer mgr.StopAll()

	instance, err := mgr.Launch("telegram", binaryPath)
	if err != nil {
		t.Fatalf("error lanzando plugin de telegram: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// El plugin real exige credenciales de aplicación: sin ellas no hay sesión MTProto.
	_, err = instance.RPC.Connect(ctx, &conduitv1.ConnectRequest{
		AccountIdentifier: "+5491100001111",
	})
	if err == nil {
		t.Fatal("se esperaba error al conectar Telegram sin api_id/api_hash")
	}

	// Enviar sin sesión autorizada tampoco puede funcionar.
	if _, err := instance.RPC.SendOutgoingMessage(ctx, &conduitv1.OutgoingMessageRequest{
		RemoteRecipientId: "@destinatario",
		Body:              "Mensaje de prueba vía plugin gRPC",
	}); err == nil {
		t.Fatal("se esperaba error al enviar sin sesión de Telegram")
	}
}

func TestWhatsAppPluginLifecycle(t *testing.T) {
	binaryPath := builtPlugin(t, "whatsapp")

	mgr := NewManager()
	defer mgr.StopAll()

	instance, err := mgr.Launch("whatsapp", binaryPath)
	if err != nil {
		t.Fatalf("error lanzando plugin de whatsapp: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	connResp, err := instance.RPC.Connect(ctx, &conduitv1.ConnectRequest{
		AccountIdentifier: "+5491199998888",
	})
	if err != nil {
		t.Fatalf("error llamando Connect en plugin whatsapp: %v", err)
	}

	if !connResp.Success {
		t.Errorf("se esperaba conexión exitosa, respuesta: %s", connResp.Message)
	}

	msgResp, err := instance.RPC.SendOutgoingMessage(ctx, &conduitv1.OutgoingMessageRequest{
		RemoteRecipientId: "+5491133334444",
		Body:              "Mensaje de prueba vía WhatsApp plugin",
	})
	if err != nil {
		t.Fatalf("error enviando mensaje con whatsapp plugin: %v", err)
	}

	// Cuando no hay dispositivo emparejado, whatsmeow devuelve error de device JID o retorna el estado de desconexión / QR
	if !msgResp.Success {
		t.Logf("Comportamiento esperado sin pairing de WhatsApp: %s", msgResp.ErrorMessage)
	} else if msgResp.RemoteMessageId == "" {
		t.Errorf("se esperaba RemoteMessageId en respuesta exitosa")
	}
}

func TestManagerStopPluginRemovesFromActiveAndDisabled(t *testing.T) {
	mgr := NewManager()

	mgr.RegisterInstance("active", &Instance{
		Name:     "active",
		Manifest: &conduitv1.PluginManifest{Id: "active", DisplayName: "Active", Version: "1.0.0"},
		Path:     "/tmp/conduit-plugin-active",
	})
	mgr.mu.Lock()
	mgr.disabled["disabled"] = Discovered{
		Manifest: &conduitv1.PluginManifest{Id: "disabled", DisplayName: "Disabled", Version: "1.0.0"},
		Path:     "/tmp/conduit-plugin-disabled",
	}
	mgr.mu.Unlock()

	if err := mgr.StopPlugin("active"); err != nil {
		t.Fatalf("StopPlugin(active) unexpected error: %v", err)
	}
	if _, ok := mgr.GetInstance("active"); ok {
		t.Fatal("active plugin still present in m.plugins after StopPlugin")
	}

	if err := mgr.StopPlugin("disabled"); err != nil {
		t.Fatalf("StopPlugin(disabled) unexpected error: %v", err)
	}
	if found := mgr.Discovered(); len(found) != 0 {
		t.Fatalf("Discovered() = %+v, want empty after stopping active and disabled plugins", found)
	}

	if err := mgr.StopPlugin("missing"); err != nil {
		t.Fatalf("StopPlugin(missing) = %v, want nil", err)
	}
}

func TestManagerStopPluginKillsRunningClient(t *testing.T) {
	path := builtPlugin(t, "discord")
	mgr := NewManager()
	defer mgr.StopAll()

	inst, err := mgr.LaunchDiscovered(context.Background(), path, nil)
	if err != nil {
		t.Fatalf("LaunchDiscovered: %v", err)
	}
	if inst.Client == nil {
		t.Fatal("expected non-nil plugin Client")
	}

	if err := mgr.StopPlugin("discord"); err != nil {
		t.Fatalf("StopPlugin(discord) error: %v", err)
	}
	if !inst.Client.Exited() {
		t.Fatal("expected plugin process to exit after StopPlugin")
	}
	if _, ok := mgr.GetInstance("discord"); ok {
		t.Fatal("discord still registered after StopPlugin")
	}
}
