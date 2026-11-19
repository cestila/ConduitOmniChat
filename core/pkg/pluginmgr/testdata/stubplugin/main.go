package main

import (
	"context"

	conduitv1 "github.com/cestila/ConduitOmniChat/sdk/conduitv1"
	"github.com/cestila/ConduitOmniChat/sdk/pluginsdk"
)

type stubPlugin struct {
	conduitv1.UnimplementedPluginServiceServer
}

func (*stubPlugin) GetManifest(_ context.Context, _ *conduitv1.GetManifestRequest) (*conduitv1.PluginManifest, error) {
	return &conduitv1.PluginManifest{
		Id:              "stub",
		DisplayName:     "Stub Plugin",
		Version:         "0.0.1",
		Provider:        conduitv1.Provider_PROVIDER_DISCORD,
		ProtocolVersion: pluginsdk.ProtocolVersion,
	}, nil
}

func main() {
	pluginsdk.Serve(&stubPlugin{})
}
