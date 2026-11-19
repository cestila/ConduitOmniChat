# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).
Every pull request with a user-visible change adds an entry under
**[Unreleased]**; a release moves those entries under its version.

This repository ships two versioned artifacts: the application (`conduitd`
and the desktop app, tags `vX.Y.Z`) and the plugin SDK Go module
(`github.com/cestila/ConduitOmniChat/sdk`, tags `sdk/vX.Y.Z`). SDK changes are
listed under an **SDK** heading inside each version.

## [Unreleased]

## [0.2.1] - 2026-11-19

### Changed

- Resolve Core, GUI, and official/third-party plugin latest releases directly from each repository's GitHub Releases (with `ETag` revalidation, web redirect fallback on rate limits, and `plugin-metadata.json` release asset support).
- Updated `Makefile`, `README.md`, and `docs/RELEASING.md` for standalone `conduit-plugin-*` repositories.
- Replaced skipped external-binary `pluginmgr` lifecycle tests with a hermetic stub plugin fixture (`core/pkg/pluginmgr/testdata/stubplugin`).

### Removed

- `.github/workflows/update-registry.yml` (plugin releases no longer require updating `ConduitOmniChat/registry.json`).
- Legacy in-repo `plugins/` and `tui/` build/test targets from `Makefile` and obsolete ignore entries (`plugins/`, `tui/`, `dist-public/`) from `.gitignore`.

## [0.2.0] - 2026-11-18

### Added

- Build-time version injection (`core/pkg/buildinfo`), `conduitd version`, and `GetStatusResponse` version/protocol reporting (`core_version`, `protocol_version`, `tui_version`).
- Remote plugin and application registry (`registry.json`), local manifest tracking (`plugins-manifest.json`), compatibility/dependency checks, and third-party `owner/conduit-plugin-<id>` installation.
- Plugin lifecycle updates and removal: `conduitd plugin check`, `conduitd plugin update [<id>|--all]`, `conduitd plugin remove <id>`, SHA-256 verified hot-updates with best-effort account reconnection, and externally managed plugin guards.
- Desktop app (`gui`) plugin updates and uninstall UI, dependency/compatibility warnings, custom plugin repository installer, guarded `electron-updater` auto-updates (falling back to release links on portable, `tar.gz`, and Nix packages), and an About view in Settings.
- CI workflow (`.github/workflows/update-registry.yml`) to keep `registry.json` synced with the latest Core, GUI, and official plugin releases.
- Real-time outgoing message status updates (`pending`, `sent`, `delivered`, `read`, `failed`) via `EventEnvelope.message_status_updated` and status check icons in the GUI.
- Plugin/account disconnection detection and live `channel_status_update` banner in the GUI (`channel.disconnected` and `StreamIncomingEvents` stream errors).
- `RenameContact` RPC and inline chat header rename with `name_locked` protection against resync overwrites, plus `IsLegibleName` preference and phone fallback display for symbol-only PushNames.
- Multi-account contact unification and `SyncContacts` / `StartChat` RPCs with live `ContactUpdatedEvent` updates and sync progress indicator.

### Fixed

- Desktop app daemon status indicator now reads `core_version` from `GetStatusResponse` instead of referencing an undefined `status.version` field.
- `checkAppUpdate` IPC handler now consistently returns an `updateAvailable` boolean alongside update details.
- `Server.SendMessage` no longer leaves outgoing messages stuck in `pending` when the target plugin is not running; it marks them `failed` and publishes `EventMessageStatus`.
- `gui/src/app.js` no longer hardcodes `fa-check-double` for all outgoing messages.

### SDK

- Added `UninstallPlugin`, `UpdatePlugin`, `CheckUpdates`, `MarkPluginsSeen`, `SyncContacts`, `StartChat`, and `RenameContact` RPCs and extended `PluginInfo`, `GetStatusResponse`, `ProtoContact`, `ProtoConversation`, and `EventEnvelope` in `sdk/proto/conduit.proto` (`sdk/conduitv1`, `sdk/v0.2.0`).

## [0.1.2] - 2026-11-17

### Added

- Nix Flake (`flake.nix`, `flake.lock`) and NixOS installation guide
  (`docs/NIXOS.md`), exposing precompiled release targets (`conduit-gui`,
  `conduitd`) by default alongside source-built targets (`conduit-gui-source`,
  `conduitd-source`), with desktop entry and icon integration.

### Fixed

- Desktop app no longer opens DevTools by default when packaged or running in
  production mode (`F12` and `DEBUG=1` remain available).

## [0.1.1] - 2026-11-13

### Changed

- The official plugin catalog no longer offers Discord: its plugin is an
  unpublished demo, so `conduitd plugin install discord` and the desktop app's
  Plugins panel no longer try to download it.

### Fixed

- The CI proto-drift check ignores the `protoc` version header of generated
  code, which differs between the CI runner and local toolchains.
- Desktop app artifacts carry the release version: the release workflow sets
  it from the tag (the 0.1.0 artifacts were labeled 1.0.0).

## [0.1.0] - 2026-11-13

First public release. SDK: `sdk/v0.1.0`.

### Added

- `conduitd` daemon with a local gRPC API on `127.0.0.1:50051` and SQLite
  storage in `~/.local/share/conduit`.
- Plugin system: plugins are separate executables (hashicorp/go-plugin over
  gRPC) that describe themselves with `GetManifest`; `conduitd` discovers
  them in its plugin directories.
- `conduitd plugin install|list|remove`: installs plugins from GitHub Releases
  with SHA-256 checksum verification.
- Desktop app (Electron) with a unified inbox, unified contacts and search,
  replies, reactions, polls, buttons, edit, delete, pin, read receipts, media,
  voice notes, forwarding and Telegram forum topics, where the plugin supports
  them. Credential forms, badges and QR instructions come from plugin manifests.
- Release artifacts: `conduitd` for Linux, macOS and Windows (amd64/arm64),
  AppImage and tar.gz for Linux, installer and portable build for Windows.

### SDK

- `conduit.v1` contract (`sdk/proto/conduit.proto`), generated Go code in
  `sdk/conduitv1` and `sdk/pluginsdk` (`Serve`, `HandshakeConfig`,
  `ValidateManifest`). Plugin protocol version 1.

[Unreleased]: https://github.com/cestila/ConduitOmniChat/compare/v0.2.1...HEAD
[0.2.1]: https://github.com/cestila/ConduitOmniChat/compare/v0.2.0...v0.2.1
[0.2.0]: https://github.com/cestila/ConduitOmniChat/compare/v0.1.2...v0.2.0
[0.1.2]: https://github.com/cestila/ConduitOmniChat/compare/v0.1.1...v0.1.2
[0.1.1]: https://github.com/cestila/ConduitOmniChat/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/cestila/ConduitOmniChat/releases/tag/v0.1.0
