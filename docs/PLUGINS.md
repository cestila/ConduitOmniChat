# Writing and installing plugins

## Model

A plugin is a standalone executable named `conduit-plugin-<id>`. `conduitd`
launches it as a child process using
[hashicorp/go-plugin](https://github.com/hashicorp/go-plugin) over gRPC and
talks to it through `conduit.v1.PluginService`
([`sdk/proto/conduit.proto`](../sdk/proto/conduit.proto)).

Plugins ship **no UI code**. They describe what the UI needs as data in a
manifest returned by `GetManifest`; the desktop app renders credential forms,
badges, icons and QR hints generically.

Plugins import only the SDK (`github.com/cestila/ConduitOmniChat/sdk`), never
`core`.

## Minimal plugin

```go
package main

import (
	"context"

	"github.com/cestila/ConduitOmniChat/sdk/conduitv1"
	"github.com/cestila/ConduitOmniChat/sdk/pluginsdk"
)

type MyPlugin struct {
	conduitv1.UnimplementedPluginServiceServer
}

func (p *MyPlugin) GetManifest(context.Context, *conduitv1.GetManifestRequest) (*conduitv1.PluginManifest, error) {
	return &conduitv1.PluginManifest{
		Id:              "mynet",
		DisplayName:     "My Network",
		Provider:        conduitv1.Provider_PROVIDER_SLACK, // see "Closed Provider enum"
		ProtocolVersion: pluginsdk.ProtocolVersion,
		// ...
	}, nil
}

// Implement Connect, Disconnect, SendOutgoingMessage, StreamIncomingEvents, ...

func main() { pluginsdk.Serve(&MyPlugin{}) }
```

Embedding `UnimplementedPluginServiceServer` means you only implement the RPCs
your network supports; the rest return `Unimplemented`. Advertise what you do
support in `capabilities`.

## Manifest fields

| Field | Meaning |
| --- | --- |
| `id` | Lowercase id (`^[a-z][a-z0-9-]{0,31}$`); also the conversation prefix |
| `display_name` | Name shown in the UI (required) |
| `description`, `author`, `homepage`, `version` | Metadata; set `version` at build time with `-ldflags "-X main.version=vX.Y.Z"` |
| `provider` | `Provider` enum value (required) |
| `icon` | Font Awesome class, e.g. `fa-brands fa-telegram` |
| `color` | CSS color for badges, e.g. `#229ed9` |
| `credential_fields` | Inputs of the "add account" form (below) |
| `login_flows` | `LOGIN_FLOW_QR`, `LOGIN_FLOW_PHONE_CODE` or `LOGIN_FLOW_TOKEN` |
| `qr_instructions` | Where to scan the QR in the official app |
| `credentials_hint` | Help text under the credential form |
| `capabilities` | Free-form feature list (`reactions`, `replies`, `polls`, `edit`, `media`, ...) |
| `protocol_version` | Must equal `pluginsdk.ProtocolVersion` |

`conduitd` validates the manifest (`pluginsdk.ValidateManifest`) and rejects
plugins with an invalid id, missing display name or provider, a mismatched
protocol version, or empty/duplicate credential field keys.

### Credential fields

Each `CredentialField` has `key`, `label`, `kind`, `required`, `placeholder`
and `help`. `kind` selects the input control:

| Kind | Control |
| --- | --- |
| `CREDENTIAL_FIELD_KIND_TEXT` | plain text |
| `CREDENTIAL_FIELD_KIND_SECRET` | masked input |
| `CREDENTIAL_FIELD_KIND_NUMBER` | numeric |
| `CREDENTIAL_FIELD_KIND_PHONE` | phone number |

The `key` is either the name of a typed `ProviderCredentials` field
(`api_id`, `bot_token`, ...) or any custom key. Read values in your plugin with
`creds.Value(key)` (`ProviderCredentials.Value`): it looks in the `values` map
first and then in the typed field of the same name.

### Login flows

- **QR**: the plugin emits `channel.auth_qr` events with the code and the GUI
  shows it (with `qr_instructions`).
- **Phone code**: a phone number plus a code sent by the network.
- **Token**: a token typed into the credential form.

## Protocol version and handshake

`pluginsdk.ProtocolVersion` (currently `1`) versions `PluginService`; it is
bumped on breaking changes. The go-plugin handshake uses
`pluginsdk.HandshakeConfig` (magic cookie key `CONDUIT_PLUGIN_MAGIC_COOKIE`).
`pluginsdk.Serve(impl)` wires both up, so plugin authors do not handle them.

## Discovery

`conduitd` scans these directories, highest precedence first, and launches every
executable named `conduit-plugin-*` (`.exe` on Windows):

1. `$CONDUIT_PLUGIN_DIR`
2. `<dataDir>/plugins` (`~/.local/share/conduit/plugins`)
3. `<conduitd directory>/plugins`
4. `<conduitd directory>`
5. `<current directory>/bin/plugins`

Plugins are registered by `manifest.id`; the first one found wins. `conduitd
help` prints the resolved directories.

## Installing

```sh
conduitd plugin install <id|owner/repo> [--version vX.Y.Z]
conduitd plugin list
conduitd plugin remove <id>
```

- An official `id` (`whatsapp`, `telegram`) maps to
  `github.com/cestila/conduit-plugin-<id>`. `owner/repo` installs a third-party
  repository, whose name must be `conduit-plugin-<id>`.
- The desktop app's Plugins panel installs **official** plugins through the
  `InstallPlugin` RPC, using the same installer.
- Plugins are **not** distributed as npm packages or `go get` modules for end
  users. Developers can run
  `go install github.com/cestila/conduit-plugin-<id>@latest` and copy the binary
  (named `conduit-plugin-<id>`) into a plugin directory.

### Release contract

The installer downloads, from the GitHub Release (latest, or `--version`):

- `conduit-plugin-<id>_<version>_<os>_<arch>.tar.gz` (`.zip` on Windows) with
  the executable at the archive root, and
- `checksums.txt` with SHA-256 sums.

If the checksum is missing or differs, the install fails and the installed
plugin is left untouched. Each plugin repository includes a
[GoReleaser](https://goreleaser.com) config (`.goreleaser.yaml`) producing these
assets for linux/darwin/windows on amd64/arm64, and a GitHub Actions workflow
that releases on `v*` tags.

## Enabling and disabling

The Plugins panel toggles a plugin (`TogglePlugin` RPC); the state is stored as
the setting `plugin_<id>_enabled`. A disabled plugin is discovered but not
launched.

## Closed Provider enum

The `Provider` enum in `conduit.proto` is closed (`WHATSAPP`, `TELEGRAM`,
`DISCORD`, `SLACK`, `GMAIL`). A plugin for a brand-new network therefore needs a
pull request to the SDK adding a `Provider` value (and regenerating the code
with `make proto`) before it can be registered. This is a known limitation.

## Building a plugin

```sh
go build -o conduit-plugin-<id> .
```

During development against a local SDK checkout, point the module at it with a
`replace github.com/cestila/ConduitOmniChat/sdk => ../path/to/sdk` directive in
`go.mod` (or use a `go.work`). Do not commit the `replace`.
