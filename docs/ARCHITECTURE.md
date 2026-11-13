# Architecture

## Components

- **conduitd** (`core/cmd/conduitd`): local daemon. It serves `ConduitService`
  over gRPC on `127.0.0.1:50051` (loopback only), owns the SQLite database and
  supervises the plugin processes.
- **GUI** (`gui/`): Electron app. It talks to `conduitd` over gRPC (the
  `.proto` is shipped in the app resources) and starts the bundled daemon if
  none is running.
- **Plugins**: one executable per network, launched by `conduitd` with
  hashicorp/go-plugin and speaking `conduit.v1.PluginService` over gRPC. See
  [PLUGINS.md](PLUGINS.md).
- **SDK** (`sdk/`): the proto contract, generated Go code (`conduitv1`) and
  `pluginsdk` (`Serve`, handshake, manifest validation).

## Data directory

Everything lives under `~/.local/share/conduit/`:

| Path | Content |
| --- | --- |
| `conduit.db` | SQLite: conversations, messages, contacts, accounts, settings |
| `plugins/` | Plugin binaries installed with `conduitd plugin install` |

Plugin enable/disable state is stored in the settings table
(`plugin_<id>_enabled`).

## Event flow (high level)

1. On start, `conduitd` scans the plugin directories, launches each
   `conduit-plugin-*` executable and calls `GetManifest`; the plugin is
   registered under `manifest.id`.
2. When an account connects, `conduitd` calls the plugin's `Connect` with the
   stored credentials and subscribes to `StreamIncomingEvents`.
3. Incoming plugin events (messages, reactions, auth QR codes, ...) are stored
   in SQLite, linked to contacts by the identity graph, and published on the
   in-process event bus.
4. The GUI receives them through the `StreamEvents` server stream and renders
   them. Actions (send, react, edit, ...) go the other way: GUI to
   `ConduitService`, then to the right plugin RPC.
