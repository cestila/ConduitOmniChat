// Package pluginsdk holds the go-plugin glue shared by the Conduit daemon and
// every channel plugin. A plugin only needs this package and conduitv1.
package pluginsdk

import (
	"context"

	"github.com/cestila/ConduitOmniChat/sdk/conduitv1"
	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"
)

// PluginKey is the name under which the channel service is dispensed.
const PluginKey = "channel"

// ProtocolVersion of conduit.v1.PluginService. Bump it on any breaking change;
// plugins report it in PluginManifest.protocol_version.
const ProtocolVersion uint32 = 1

// HandshakeConfig is the shared signature between conduitd and plugins.
var HandshakeConfig = plugin.HandshakeConfig{
	ProtocolVersion:  uint(ProtocolVersion),
	MagicCookieKey:   "CONDUIT_PLUGIN_MAGIC_COOKIE",
	MagicCookieValue: "conduit-omnichat-channel-plugin-v1",
}

// ChannelGRPCPlugin implements plugin.GRPCPlugin for conduit.v1.PluginService.
type ChannelGRPCPlugin struct {
	plugin.Plugin
	Impl conduitv1.PluginServiceServer
}

// GRPCServer registers the plugin implementation on the gRPC server.
func (p *ChannelGRPCPlugin) GRPCServer(_ *plugin.GRPCBroker, s *grpc.Server) error {
	conduitv1.RegisterPluginServiceServer(s, p.Impl)
	return nil
}

// GRPCClient returns the client used by conduitd to talk to the plugin.
func (p *ChannelGRPCPlugin) GRPCClient(_ context.Context, _ *plugin.GRPCBroker, c *grpc.ClientConn) (interface{}, error) {
	return conduitv1.NewPluginServiceClient(c), nil
}

// PluginMap is the plugin set conduitd dispenses from every plugin process.
func PluginMap(impl conduitv1.PluginServiceServer) map[string]plugin.Plugin {
	return map[string]plugin.Plugin{PluginKey: &ChannelGRPCPlugin{Impl: impl}}
}

// Serve runs impl as a Conduit channel plugin. It blocks until conduitd
// stops the plugin process.
func Serve(impl conduitv1.PluginServiceServer) {
	logger := hclog.New(&hclog.LoggerOptions{
		Level:      hclog.Debug,
		Output:     hclog.DefaultOutput,
		JSONFormat: true,
	})
	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: HandshakeConfig,
		Plugins:         PluginMap(impl),
		GRPCServer:      plugin.DefaultGRPCServer,
		Logger:          logger,
	})
}
