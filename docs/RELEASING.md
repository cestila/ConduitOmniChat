# Releasing ConduitOmniChat

This document describes how `ConduitOmniChat` and the standalone plugin repositories are released and developed across repos.

## Repositories

| Repository | Content |
|---|---|
| `cestila/ConduitOmniChat` | `sdk/`, `core/` (`conduitd`), `gui/`, `docs/` |
| `cestila/conduit-plugin-whatsapp` | WhatsApp plugin |
| `cestila/conduit-plugin-telegram` | Telegram plugin |

## Releasing `ConduitOmniChat`

`main` is protected in every repository: changes land through pull requests (squash merge, CI green).

1. Open a PR that moves the **[Unreleased]** entries of `CHANGELOG.md` under a new `## [X.Y.Z] - YYYY-MM-DD` heading and updates the compare links at the bottom. Title: `chore(release): vX.Y.Z`.
2. Merge it, then pull and tag the merge commit on `main`:
   ```sh
   git switch main && git pull
   git tag vX.Y.Z
   # Tag the SDK subdirectory module too when sdk/ changed:
   git tag sdk/vX.Y.Z
   git push origin vX.Y.Z sdk/vX.Y.Z
   ```
   - The `vX.Y.Z` tag triggers the release workflow to build and publish `conduitd` and the desktop app.
   - Because `sdk/` lives in a subdirectory, Go requires the `sdk/vX.Y.Z` tag.
3. The release workflow also keeps `binaries.nix` current: its `nix-hashes` job downloads the
   published `Conduit-X.Y.Z.AppImage` and `conduitd_X.Y.Z_linux_amd64.tar.gz`, recalculates their
   hashes and opens `chore(nix): update precompiled release binaries to vX.Y.Z`. Merge that PR
   (CI only, no code) so `nix run github:cestila/ConduitOmniChat` serves the new binaries. Nothing
   in `flake.nix` needs editing by hand, and the flake reads version and hashes from `binaries.nix`.
4. If `sdk/` was tagged, wait until the Go module proxy serves the new version before updating plugins:
   ```sh
   GOPROXY=https://proxy.golang.org go list -m github.com/cestila/ConduitOmniChat/sdk@vX.Y.Z
   ```

## Releasing a plugin (`cestila/conduit-plugin-<id>`)

1. If the plugin needs a newer SDK release, update it first in a PR:
   ```sh
   GOWORK=off go get github.com/cestila/ConduitOmniChat/sdk@vX.Y.Z && GOWORK=off go mod tidy
   ```
2. Open a PR moving **[Unreleased]** entries in the plugin's `CHANGELOG.md` under `## [X.Y.Z] - YYYY-MM-DD` (`chore(release): vX.Y.Z`) and merge it.
3. Tag `main` and push the tag (`git switch main && git pull && git tag vX.Y.Z && git push origin vX.Y.Z`). GoReleaser publishes `conduit-plugin-<id>_<version>_<goos>_<goarch>.tar.gz`, `checksums.txt`, and `plugin-metadata.json`.
   `conduitd` resolves latest versions and compatibility metadata directly from each plugin repository's GitHub Releases (with `ETag` caching), so releasing a plugin requires no changes or PRs in `ConduitOmniChat`.
4. Verify the published release:
   ```sh
   conduitd plugin install <id>
   conduitd plugin list
   ```

## Versioning rules

- **ProtocolVersion:** a backwards-compatible change (new optional RPC or field) does not bump `ProtocolVersion`. A breaking change to `sdk/proto/conduit.proto` or the handshake bumps it, making old plugins and a new `conduitd` incompatible; in that case release the SDK, `conduitd`, and every official plugin together, and note it in the release notes.
- Never re-tag or move a published version tag; the Go module proxy caches modules permanently.

## Local cross-repo development

Check the repositories out side by side under a common parent directory and create a local `go.work` there (outside any git repo, or never committed):

```go
// ../go.work
go 1.26

use (
	./ConduitOmniChat/sdk
	./ConduitOmniChat/core
	./conduit-plugin-telegram
	./conduit-plugin-whatsapp
)
```
