package main

import (
	"context"

	"github.com/cestila/ConduitOmniChat/core/pkg/storage"
	conduitv1 "github.com/cestila/ConduitOmniChat/sdk/conduitv1"
)

// applyChannelReady marca la cuenta como conectada cuando el plugin declara una conexión real.
// Un evento de una sincronización simulada nunca convierte la cuenta en 'connected'.
func applyChannelReady(acc *conduitv1.ProtoAccount, ev *conduitv1.PluginEvent) {
	if ev.GetSimulated() {
		return
	}
	acc.Status = "connected"
	acc.QrCode = ""
	acc.StatusMessage = ""
}

// applyChannelReadyForProvider actualiza las cuentas del proveedor que emitió el evento.
func applyChannelReadyForProvider(ctx context.Context, db *storage.DB, provider conduitv1.Provider, ev *conduitv1.PluginEvent) {
	accounts, err := db.ListAccounts(ctx)
	if err != nil {
		return
	}
	for _, acc := range accounts {
		if acc.Provider != provider {
			continue
		}
		applyChannelReady(acc, ev)
		_ = db.SaveAccount(ctx, acc)
	}
}

// resumeConnectRequest arma el Connect de reanudación con las credenciales
// persistidas. Sin ellas, los proveedores que no guardan sesión propia en disco
// (Telegram) no pueden reconectar tras un reinicio del daemon.
func resumeConnectRequest(acc *conduitv1.ProtoAccount) *conduitv1.ConnectRequest {
	return &conduitv1.ConnectRequest{
		AccountIdentifier: acc.GetIdentifier(),
		Credentials:       acc.GetCredentials(),
	}
}
