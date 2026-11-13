package main

import (
	"testing"

	"github.com/cestila/ConduitOmniChat/core/pkg/domain"
	conduitv1 "github.com/cestila/ConduitOmniChat/sdk/conduitv1"
)

func TestPluginConfigFromManifest(t *testing.T) {
	m := &conduitv1.PluginManifest{Id: "telegram", Provider: conduitv1.Provider_PROVIDER_TELEGRAM}
	got := pluginConfigFromManifest(m)
	want := pluginConfig{Name: "telegram", Provider: domain.Provider("telegram"), Proto: conduitv1.Provider_PROVIDER_TELEGRAM}
	if got != want {
		t.Fatalf("se esperaba %+v, obtuve %+v", want, got)
	}
}
