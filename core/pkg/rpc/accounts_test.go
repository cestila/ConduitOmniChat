package rpc

import (
	"context"
	"testing"

	conduitv1 "github.com/cestila/ConduitOmniChat/sdk/conduitv1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Verifica la traducción de la respuesta del plugin al estado persistible de la cuenta:
// el error nombra lo que falta, la simulación se marca como demostración y nunca como conexión real.
func TestApplyConnectOutcome(t *testing.T) {
	cases := []struct {
		name        string
		resp        *conduitv1.ConnectResponse
		err         error
		wantStatus  string
		wantMessage string
	}{
		{
			name:        "error del plugin deja la cuenta en error con el mensaje legible",
			err:         status.Error(codes.InvalidArgument, "faltan credenciales de Telegram: api_id, api_hash, phone"),
			wantStatus:  "error",
			wantMessage: "faltan credenciales de Telegram: api_id, api_hash, phone",
		},
		{
			name:        "respuesta sin éxito deja la cuenta en error",
			resp:        &conduitv1.ConnectResponse{Success: false, Message: "token rechazado"},
			wantStatus:  "error",
			wantMessage: "token rechazado",
		},
		{
			name:        "sincronización simulada se marca como demostración",
			resp:        &conduitv1.ConnectResponse{Success: true, Simulated: true, Message: "Modo demostración: todavía es simulada"},
			wantStatus:  "demo",
			wantMessage: "Modo demostración: todavía es simulada",
		},
		{
			name:       "respuesta con QR queda en pairing",
			resp:       &conduitv1.ConnectResponse{Success: true, Message: "Escanea el código QR"},
			wantStatus: "pairing",
		},
		{
			name:       "conexión real queda en connected",
			resp:       &conduitv1.ConnectResponse{Success: true, Message: "Conectado"},
			wantStatus: "connected",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			acc := &conduitv1.ProtoAccount{Id: "acc-1", Status: "disconnected"}
			applyConnectOutcome(acc, tc.resp, tc.err)

			if acc.Status != tc.wantStatus {
				t.Fatalf("se esperaba status %q, obtuve %q", tc.wantStatus, acc.Status)
			}
			if acc.StatusMessage != tc.wantMessage {
				t.Fatalf("se esperaba mensaje %q, obtuve %q", tc.wantMessage, acc.StatusMessage)
			}
			if tc.wantStatus != "connected" && acc.Status == "connected" {
				t.Fatalf("una cuenta no real nunca debe quedar como conectada")
			}
		})
	}
}

func TestAddAccountNormalizesCredentialValues(t *testing.T) {
	srv, db := newMediaTestServer(t)
	resp, err := srv.AddAccount(context.Background(), &conduitv1.AddAccountRequest{
		Provider: conduitv1.Provider_PROVIDER_TELEGRAM,
		Label:    "tg",
		Credentials: &conduitv1.ProviderCredentials{Values: map[string]string{
			"api_id": "1", "api_hash": "h", "custom": "x",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	stored, err := db.GetAccountByID(context.Background(), resp.Account.Id)
	if err != nil || stored == nil {
		t.Fatalf("no se encontró la cuenta: %v", err)
	}
	c := stored.Credentials
	if c.GetApiHash() != "h" || c.GetApiId() != "1" {
		t.Fatalf("campos tipados no poblados: %+v", c)
	}
	if _, ok := c.Values["api_id"]; ok {
		t.Fatalf("api_id no debería quedar en Values: %v", c.Values)
	}
	if _, ok := c.Values["api_hash"]; ok {
		t.Fatalf("api_hash no debería quedar en Values: %v", c.Values)
	}
	if c.Values["custom"] != "x" {
		t.Fatalf("clave desconocida debe conservarse: %v", c.Values)
	}
}
