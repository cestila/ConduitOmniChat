# ConduitOmniChat

Conduit is a local-first, multi-network chat client. A small background daemon
(`conduitd`) keeps your conversations from several messaging networks in one
local SQLite database, and a desktop app (Electron) lets you read and answer
them in a single unified inbox. Each network is a separate **plugin** process,
installed on demand from GitHub Releases.

> **Read the [Disclaimer / Terms of Service](#disclaimer--terms-of-service)
> before connecting any account.**

## Features

- One inbox for every connected account, with per-network badges and filters.
- Unified contacts: merge a person's identities across networks.
- Unified search over conversations, contacts and messages.
- Replies/quotes, reactions, polls, buttons, edit, delete and pin, read
  receipts, media (attachments, voice notes) and forwarding, where the network
  and plugin support them (each plugin lists its `capabilities` in its manifest).
- Telegram forum topics.
- Plugins describe their own UI as data (credential forms, QR instructions,
  colors, icons): the desktop app has no per-network code.
- Plugins are separate executables installed from GitHub Releases with
  SHA-256 verification.

Plugin status:

| Plugin | Status |
| --- | --- |
| WhatsApp | Working (unofficial client, see disclaimer) |
| Telegram | Working (your own `api_id`/`api_hash`) |
| Discord | **Demo only**: accepts credentials but the gateway is simulated; no real connection yet |

## Architecture

```
  +-----------------+   gRPC ConduitService    +-------------------+
  |  Electron GUI   | <----------------------> |      conduitd     |
  |      (gui/)     |   127.0.0.1:50051        |     (core/)       |
  +-----------------+                          |  SQLite + event   |
                                               |  bus + plugin mgr |
                                               +---------+---------+
                                                         | hashicorp/go-plugin
                                                         | gRPC PluginService
                                  +----------------------+-----------------+
                                  |                      |                 |
                         conduit-plugin-whatsapp  conduit-plugin-telegram  conduit-plugin-discord
```

More detail in [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

## Install (users)

1. Download the desktop app for your platform from the
   [GitHub Releases](https://github.com/cestila/ConduitOmniChat/releases) page:
   AppImage or `tar.gz` (Linux), portable or NSIS installer (Windows). The app
   bundles `conduitd` and starts it for you.
2. Install the plugins you need:
   - from the app: open the **Plugins** panel and install an official plugin, or
   - from a terminal:

   ```sh
   conduitd plugin install telegram            # official plugin id
   conduitd plugin install whatsapp --version v0.1.0
   conduitd plugin install owner/conduit-plugin-foo   # third-party repository
   conduitd plugin list
   conduitd plugin remove telegram
   ```

   Downloads come from the plugin's GitHub Release and the SHA-256 checksum in
   `checksums.txt` is verified before anything is installed. If it is missing
   or does not match, the install fails and the installed plugin is untouched.
3. Add an account from the app and follow the plugin's login flow.

### Plugin directories

`conduitd` scans these directories, highest precedence first (the first
plugin found with a given id wins):

1. `$CONDUIT_PLUGIN_DIR` (may hold several directories)
2. `<dataDir>/plugins` (default `~/.local/share/conduit/plugins`; where `plugin install` writes)
3. `<conduitd directory>/plugins`
4. `<conduitd directory>`
5. `<current directory>/bin/plugins`

`conduitd help` prints the resolved list. Every executable named
`conduit-plugin-*` is launched and asked for its manifest. A plugin disabled in
the Plugins panel is not launched.

## Build from source

Requirements: Go (version in `core/go.mod`), Node.js 22 for the GUI, and
`protoc` with `protoc-gen-go`/`protoc-gen-go-grpc` only if you change the proto.

```sh
make build        # conduitd -> bin/conduitd, plugins -> bin/plugins/
make test         # Go tests (sdk, core, plugins) and GUI tests
make proto        # regenerate sdk/conduitv1 from sdk/proto/conduit.proto

cd gui && npm ci && npm start    # run the desktop app against ./bin
```

The development workspace is a Go workspace (`go.work`) with `sdk/`, `core/`
and the three plugin modules. Published, the plugins live in their own
repositories.

## Project layout

| Path | Content |
| --- | --- |
| `sdk/` | Plugin contract (`proto/conduit.proto`), generated `conduitv1`, `pluginsdk` helpers |
| `core/` | `conduitd` daemon: storage, event bus, plugin manager, installer, gRPC server |
| `gui/` | Electron desktop app |
| `docs/` | Documentation |

Official plugins live in their own repositories:
[whatsapp](https://github.com/cestila/conduit-plugin-whatsapp),
[telegram](https://github.com/cestila/conduit-plugin-telegram),
[discord](https://github.com/cestila/conduit-plugin-discord).

## Writing a plugin

A plugin is a Go program that imports only the SDK and serves
`conduit.v1.PluginService`. See [docs/PLUGINS.md](docs/PLUGINS.md). Note that
the `Provider` enum is closed: a brand-new network needs a pull request to the
SDK first.

## Disclaimer / Terms of Service

Conduit is an independent project. It is **not affiliated with, endorsed by or
sponsored by** Meta/WhatsApp, Telegram or Discord. Using it is at your own risk
and you are responsible for complying with each network's terms.

- **WhatsApp:** the plugin uses an **unofficial** client library
  ([whatsmeow](https://github.com/tulir/whatsmeow)). Automating or using
  unofficial clients can violate WhatsApp's terms and **may get your account
  restricted or banned**. Do not use an account you cannot afford to lose.
- **Discord:** logging in with a **user token (self-bot) violates Discord's
  Terms of Service** and can get the account terminated. Use a **bot token**.
  (The Discord plugin is currently a demo that does not connect for real.)
- **Telegram:** you must create **your own `api_id` and `api_hash`** at
  <https://my.telegram.org/apps>. Do not share them and do not reuse someone
  else's.

Credentials you enter are stored locally; see [SECURITY.md](SECURITY.md).

## License

Apache License 2.0. See [LICENSE](LICENSE) and [NOTICE](NOTICE).
