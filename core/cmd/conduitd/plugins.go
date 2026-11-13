package main

import (
	"github.com/cestila/ConduitOmniChat/core/pkg/domain"
	conduitv1 "github.com/cestila/ConduitOmniChat/sdk/conduitv1"
)

// pluginConfig es la configuración que el loop de eventos necesita de un plugin descubierto.
type pluginConfig struct {
	Name     string
	Provider domain.Provider
	Proto    conduitv1.Provider
}

// pluginConfigFromManifest deriva la configuración del plugin a partir de su manifiesto.
func pluginConfigFromManifest(m *conduitv1.PluginManifest) pluginConfig {
	return pluginConfig{
		Name:     m.GetId(),
		Provider: domain.Provider(m.GetId()),
		Proto:    m.GetProvider(),
	}
}
