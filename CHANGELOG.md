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

[Unreleased]: https://github.com/cestila/ConduitOmniChat/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/cestila/ConduitOmniChat/releases/tag/v0.1.0
