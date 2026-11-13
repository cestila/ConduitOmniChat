package main

import (
	"testing"

	conduitv1 "github.com/cestila/ConduitOmniChat/sdk/conduitv1"
)

// Verifica que un evento de disponibilidad simulado no convierta en 'connected'
// una cuenta que está en modo demostración o que falló por credenciales.
func TestApplyChannelReadyKeepsSimulatedAccountsDeclared(t *testing.T) {
	t.Run("la demostración se conserva", func(t *testing.T) {
		acc := &conduitv1.ProtoAccount{Status: "demo", StatusMessage: "Modo demostración: todavía es simulada"}
		applyChannelReady(acc, &conduitv1.PluginEvent{EventType: "channel.ready", Simulated: true})

		if acc.Status != "demo" {
			t.Fatalf("una cuenta simulada pasó a %q", acc.Status)
		}
		if acc.StatusMessage == "" {
			t.Fatalf("se perdió el mensaje de demostración")
		}
	})

	t.Run("el error de credenciales se conserva", func(t *testing.T) {
		acc := &conduitv1.ProtoAccount{Status: "error", StatusMessage: "faltan credenciales de Telegram: api_id"}
		applyChannelReady(acc, &conduitv1.PluginEvent{EventType: "channel.ready", Simulated: true})

		if acc.Status != "error" {
			t.Fatalf("una cuenta sin credenciales pasó a %q", acc.Status)
		}
	})

	t.Run("una conexión real sí marca connected", func(t *testing.T) {
		acc := &conduitv1.ProtoAccount{Status: "pairing", QrCode: "qr-viejo"}
		applyChannelReady(acc, &conduitv1.PluginEvent{EventType: "channel.ready"})

		if acc.Status != "connected" {
			t.Fatalf("una conexión real debería quedar connected, obtuve %q", acc.Status)
		}
		if acc.QrCode != "" {
			t.Fatalf("el QR debe limpiarse al conectar")
		}
	})
}

// Un reinicio debe reanudar Telegram con las credenciales guardadas: sin api_id
// el plugin rechaza el Connect y la cuenta queda desconectada.
func TestResumeConnectRequestCarriesStoredCredentials(t *testing.T) {
	acc := &conduitv1.ProtoAccount{
		Identifier: "telegram",
		Credentials: &conduitv1.ProviderCredentials{
			ApiId:   "1234567",
			ApiHash: "hash-de-prueba",
		},
	}

	req := resumeConnectRequest(acc)
	if req.GetAccountIdentifier() != "telegram" {
		t.Fatalf("account_identifier = %q, se esperaba telegram", req.GetAccountIdentifier())
	}
	if req.GetCredentials().GetApiId() != "1234567" {
		t.Fatalf("api_id no viaja en el resume: %q", req.GetCredentials().GetApiId())
	}
	if req.GetCredentials().GetApiHash() != "hash-de-prueba" {
		t.Fatalf("api_hash no viaja en el resume")
	}
}

// Una cuenta sin credenciales guardadas no debe inventarlas.
func TestResumeConnectRequestWithoutCredentials(t *testing.T) {
	req := resumeConnectRequest(&conduitv1.ProtoAccount{Identifier: "cestila"})
	if req.GetCredentials() != nil {
		t.Fatalf("no se deben inventar credenciales: %+v", req.GetCredentials())
	}
}
