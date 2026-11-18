# Design: Plugin Lifecycle, Remote & Local Registry, Dependency Checks, Update Notifications, System Versioning & About View

Date: 2026-11-18
Status: Ready for User Approval

## 1. Intent & Why

ConduitOmniChat currently has an incomplete plugin management experience and disconnected version reporting:
- **Plugins:** `conduitd` can install official plugins from GitHub Releases and remove them via CLI, but the gRPC API and Electron GUI only expose `ListPlugins`, `InstallPlugin` (latest only), and `TogglePlugin`. Users cannot uninstall or update plugins from the GUI, third-party `owner/conduit-plugin-<id>` plugins are rejected over gRPC and not tracked for updates, there is no dependency/compatibility validation before installing or updating, and hot-updating an already running plugin would collide with `ErrDuplicatePlugin` in `pluginmgr.Manager`.
- **System Versions:** `CoreVersion` is hardcoded to `"0.1.0-alpha"` in `core/pkg/rpc/server.go`, `gui/src/app.js` reads `status.version` instead of `status.core_version` (falling back to `v1.0`), no build-time version injection (`-ldflags`) exists in `Makefile`, `.goreleaser.yaml`, `flake.nix`, or CI, and there is no **About** view listing the versions of all system components.
- **App Updates:** Desktop users on Windows, macOS, and Linux AppImage have no built-in way to be notified of new Conduit releases and apply updates with one click.

This design establishes a cohesive, automated versioning, update-checking, and plugin-lifecycle architecture where:
1. Build and CI pipelines automatically inject accurate versions into every artifact.
2. `conduitd` maintains a **local plugin manifest** (`<dataDir>/plugins-manifest.json`) merged with a CI-generated **remote `registry.json`** (with offline fallback to the embedded catalog), so both **official** and **third-party** plugins are tracked, checked for dependencies/compatibility, and updatable.
3. Updates are **never silent**: the app checks and notifies, and the user explicitly triggers installation.

## 2. Goals, Non-Goals & Anti-Goals

### Goals
- Complete plugin lifecycle in `conduitd`, gRPC, CLI, and GUI: **Install** (official + custom `owner/conduit-plugin-<id>`), **Uninstall**, **Update** (single or all), and **Enable/Disable**.
- Unified **local + remote manifest** tracking both official plugins (synced from `registry.json`) and installed third-party plugins (`owner/repo`).
- **Dependency & compatibility management:** validate `protocol_version`, `min_core_version`, and optional plugin `dependencies` before installing or updating, surfacing clear warnings or blocking incompatible installs.
- Detect and notify users about **plugin updates** and **new official catalog plugins** without hitting unauthenticated GitHub REST API rate limits (60 req/hr) for official plugins.
- Automatic build-time version injection for `conduitd` (`Makefile`, `.goreleaser.yaml`, `flake.nix`, `.github/workflows/release.yml`) and `conduitd version` CLI command.
- Fix the GUI status version bug (`status.core_version`) and add an **About** view showing versions of GUI, `conduitd`, SDK `protocol_version`, `conduit-tui` (when present), and all installed plugins.
- Non-silent app update notifications + 1-click download/install via `electron-updater` on supported packages (Windows NSIS, macOS, writable Linux AppImage), and graceful notification + release link fallback on Windows portable, `tar.gz`, Nix, and dev builds.

### Non-Goals
- Silent/automatic background installation of app or plugin updates without user confirmation.
- Hosting a custom binary CDN or package server (GitHub Releases + raw `registry.json` in the repo are sufficient).
- Modifying external repositories (`conduit-plugin-whatsapp`, `conduit-plugin-telegram`, `tui`) directly in this repo's PRs beyond defining the contract (`registry.json` + optional `repository_dispatch` webhook).

### Anti-Goals
- Duplicating plugin catalog/version-comparison logic in both Go (`conduitd`) and JavaScript (`gui`).
- Overwriting a running plugin binary on disk without stopping its `go-plugin` process first or without verifying the new download's SHA-256 checksum beforehand.
- Attempting to delete or overwrite read-only / externally managed plugin binaries (e.g., Nix store paths).

## 3. Constraints

- **External plugin/TUI repos:** `plugins/` and `tui/` in this workspace only contain compiled binaries; their Go sources live in separate repositories.
- **Rate limits:** Unauthenticated GitHub REST API calls are limited to 60 requests/hour per IP. Official plugins avoid this via `registry.json`; third-party plugins query GitHub Releases per tracked repo (cached with TTL and `ETag`/15-min cache to stay well under limits).
- **Linux & Windows packaging diversity:** Self-updating via `electron-updater` does not work for Windows `portable` `.exe` builds (`PORTABLE_EXECUTABLE_DIR`), Nix-managed read-only AppImages (`/nix/store/...`), or extracted `tar.gz` archives.
- **Single source of truth for gRPC:** Any proto changes in `sdk/proto/conduit.proto` must be regenerated into `sdk/conduitv1` via `make proto`.

## 4. Approaches Considered

1. **Approach 1 — `conduitd` centralizes remote `registry.json` + local `plugins-manifest.json`, dependency checks & lifecycle + `electron-updater` in GUI *(Selected)***
   - `conduitd` syncs official entries from `registry.json` into a persistent local manifest that also tracks installed third-party `owner/repo` plugins and dependency metadata, exposing `UninstallPlugin`, `UpdatePlugin`, and `CheckUpdates` over gRPC.
   - `gui` renders badges, plugin actions, compatibility/dependency warnings, the About view, and delegates desktop app updates to `electron-updater` (`autoDownload: false`).
   - *Tradeoff:* Adds proto RPCs, local manifest persistence, `electron-updater` dependency, and CI metadata (`latest*.yml` + `registry.json` workflow), but keeps all plugin logic unified across GUI, CLI, and TUI.

2. **Approach 2 — Update checking in Electron (JS), `conduitd` only executes install/remove**
   - *Rejected:* Duplicates catalog, third-party repo tracking, and dependency/compatibility logic in JS and leaves CLI/TUI without update-checking capabilities.

3. **Approach 3 — Same as Approach 1 for plugins/About, but no `electron-updater` (link-only for app updates)**
   - *Rejected:* Forces Windows and macOS users to manually download and run installers on every release instead of 1-click updates.

## 5. Architecture & Detailed Design

### 5.1. Build-Time Versioning & System Info

1. **Core Build Info (`core/pkg/buildinfo`):**
   - Variables: `Version = "dev"`, `Commit = "none"`, `Date = "unknown"`, injected via `-ldflags "-X ...Version=... -X ...Commit=... -X ...Date=..."`.
   - Update `Makefile` (`daemon` target using `git describe --tags --always --dirty` or `VERSION`), `.goreleaser.yaml` (`ldflags`), `flake.nix` (`ldflags`), and `.github/workflows/release.yml` (Windows & Linux sidecar build steps).
   - Add `conduitd version` (and `--version` / `-v`) to `core/cmd/conduitd/cli.go`.
2. **`GetStatusResponse` (`sdk/proto/conduit.proto`):**
   - Populate `core_version` with `buildinfo.Version` instead of hardcoded `"0.1.0-alpha"`.
   - Add fields: `uint32 protocol_version = 4` (from `pluginsdk.ProtocolVersion`) and `string tui_version = 5` (detected if `conduit-tui` binary exists in sibling/PATH/search dirs and responds to `--version` with a short timeout, cached at startup; empty if absent).
3. **GUI Version Bugfix:**
   - Fix `gui/src/app.js` to read `status.core_version` instead of `status.version`.
   - Expose `app.getVersion()` from `gui/main.js` via IPC along with packaging mode (`appimage`, `win32-nsis`, `win32-portable`, `darwin`, `archive`, `dev`).

### 5.2. Remote Registry (`registry.json`), Local Manifest (`plugins-manifest.json`) & Dependencies

1. **Remote Registry Schema (`registry.json` at repo root on `main`):**
   ```json
   {
     "schema_version": 1,
     "updated_at": "2026-11-18T12:00:00Z",
     "core": {
       "version": "0.1.2",
       "tag": "v0.1.2",
       "release_url": "https://github.com/cestila/ConduitOmniChat/releases/tag/v0.1.2"
     },
     "gui": {
       "version": "0.1.2",
       "tag": "v0.1.2",
       "release_url": "https://github.com/cestila/ConduitOmniChat/releases/tag/v0.1.2"
     },
     "plugins": [
       {
         "id": "whatsapp",
         "name": "WhatsApp",
         "description": "...",
         "repository": "cestila/conduit-plugin-whatsapp",
         "latest_version": "0.1.2",
         "min_protocol_version": 1,
         "max_protocol_version": 1,
         "min_core_version": "0.1.0",
         "dependencies": [],
         "release_url": "https://github.com/cestila/conduit-plugin-whatsapp/releases/tag/v0.1.2"
       }
     ]
   }
   ```
2. **Local Manifest (`<dataDir>/plugins-manifest.json`):**
   - Persists the merged local state of known plugins:
     - Official catalog entries synced from `registry.json` (and embedded `pluginmgr.Catalog` as offline seed/fallback).
     - Third-party plugins installed by the user (`source: "third-party"`, `repository: "owner/conduit-plugin-<id>"`, `installed_version`, `latest_version`, `min_protocol_version`, `min_core_version`, `dependencies`, `last_checked_at`).
   - During `CheckUpdates` / `ListPlugins` (with 15-minute in-memory/manifest cache TTL, bypassed on `force_refresh`):
     - Fetches `registry.json` (default `https://raw.githubusercontent.com/cestila/ConduitOmniChat/main/registry.json`, overridable via `CONDUIT_REGISTRY_URL`, 5s HTTP timeout) to update official plugins and discover new official plugins.
     - For each tracked third-party plugin, queries `/repos/<owner>/<repo>/releases/latest` on GitHub (respecting cache TTL and gracefully degrading on rate limit/offline).
3. **Dependency & Compatibility Management:**
   - Each plugin entry (remote or local) can declare:
     - `min_protocol_version` / `max_protocol_version` (checked against `pluginsdk.ProtocolVersion`)
     - `min_core_version` (checked against `buildinfo.Version`, skipped when `buildinfo.Version == "dev"`)
     - `dependencies`: optional list of `{ plugin_id, min_version }`
   - Before `InstallPlugin` or `UpdatePlugin` proceeds, `conduitd` validates compatibility and dependencies:
     - If `protocol_version` or `min_core_version` is incompatible, the update/install is flagged `compatible = false` with an explicit reason (e.g., `"Requires Conduit Core >= v0.2.0"`), and the GUI disables the Update/Install button with that explanation.
     - If a required plugin dependency is missing or below `min_version`, `conduitd` reports the missing dependency in the response/UI (and blocks install unless dependencies are satisfied).
4. **CI Automation (`.github/workflows/update-registry.yml`):**
   - Triggered on `release: [published]`, `repository_dispatch` (for external plugin repos notifying of a new release), `schedule` (daily safety sync), and `workflow_dispatch`.
   - Uses `GITHUB_TOKEN` to query latest releases of `ConduitOmniChat` and all official plugin repos in `registry.json`, updates `registry.json`, and commits/pushes to `main`.

### 5.3. Plugin Lifecycle & gRPC / CLI / GUI

1. **Process Lifecycle in `pluginmgr.Manager`:**
   - Add `StopPlugin(id string) error`: terminates the `go-plugin` client (`client.Kill()`) and removes it from `m.plugins` / `m.disabled` so a new binary can be launched cleanly without `ErrDuplicatePlugin`.
   - Add `LaunchPlugin(id string)` to start a newly installed/updated plugin.
2. **Safe Install, Update & Uninstall in `plugininstall.Installer`:**
   - **Writable check:** Determine if a discovered plugin's `Path` resides in the user-writable `PluginDir` (`<dataDir>/plugins`). If not (e.g., Nix store `/nix/store/...` or bundled read-only path), mark `managed_externally = true`; `Update` and `Uninstall` reject with a clear error, and the GUI disables those buttons with an explanatory tooltip.
   - **Atomic Update (`Update`):** Download archive + `checksums.txt` to a temp directory and verify SHA-256 **before** stopping the running plugin or touching the existing binary on disk. Only after verification succeeds: snapshot connected accounts for that plugin, call `Manager.StopPlugin`, atomically replace the binary (`os.Rename`), update `<dataDir>/plugins-manifest.json`, re-launch if enabled, and best-effort reconnect previously connected accounts.
   - **Uninstall (`Remove`):** Stop running plugin via `Manager.StopPlugin`, delete binary from `PluginDir`, remove/clear its installed entry in `plugins-manifest.json`, and clear `plugin_<id>_enabled` setting.
3. **gRPC Contract (`sdk/proto/conduit.proto`):**
   - Extend `PluginInfo`:
     - `string latest_version`
     - `bool update_available`
     - `bool is_new` (true for newly discovered official catalog plugins not yet seen in `#view-plugins`)
     - `bool compatible`
     - `string incompatibility_reason`
     - `bool managed_externally`
     - `bool is_third_party`
     - `string release_url`
     - `repeated string dependencies`
   - Update `InstallPluginRequest` to accept either an official `plugin_id` OR a third-party `owner/conduit-plugin-<id>` spec, plus optional `version`.
   - New RPCs on `ConduitService`:
     - `UninstallPlugin(UninstallPluginRequest) returns (UninstallPluginResponse)`
     - `UpdatePlugin(UpdatePluginRequest) returns (UpdatePluginResponse)` (accepts `plugin_id` or `all = true`)
     - `CheckUpdates(CheckUpdatesRequest) returns (CheckUpdatesResponse)` (accepts `bool force_refresh`; returns core/gui latest versions, plugin updates, new plugins count, and `registry_reachable`)
     - `MarkPluginsSeen(MarkPluginsSeenRequest) returns (MarkPluginsSeenResponse)` (marks current catalog plugins as seen so the `is_new` badge clears)
4. **CLI (`core/cmd/conduitd/cli.go`):**
   - `conduitd version`
   - `conduitd plugin check`
   - `conduitd plugin update [<id>|--all]`
5. **GUI (`gui/`):**
   - Sidebar navigation badge on **Plugins** showing count of available plugin updates + unseen new official plugins (clears `is_new` via `MarkPluginsSeen` when `#view-plugins` is opened).
   - Plugin cards in `#view-plugins`:
     - Show installed version, source badge (`Official` / `Third-Party`), and when `update_available`, show target version + **Update** button (plus **Update All** when >1 compatible update exists).
     - Show **Uninstall** button with confirmation dialog (disabled with tooltip when `managed_externally`).
     - Support installing by official catalog card OR custom `owner/conduit-plugin-<id>` input.
     - Show clear compatibility/dependency warning badge if `compatible == false`.

### 5.4. Desktop App Update Notifications (`electron-updater`)

1. **Dependencies & Build Config (`gui/package.json` & `.github/workflows/release.yml`):**
   - Add `electron-updater` to `gui/package.json` dependencies and configure `build.publish` (`provider: "github"`, `owner: "cestila"`, `repo: "ConduitOmniChat"`).
   - Update `.github/workflows/release.yml` so `electron-builder` generates and uploads `latest.yml` / `latest-linux.yml` (and `latest-mac.yml` if macOS runner is added) to GitHub Releases.
2. **Main Process (`gui/main.js`):**
   - Set `autoUpdater.autoDownload = false` and `autoUpdater.autoInstallOnAppQuit = false`.
   - Enable `electron-updater` only when `app.isPackaged` AND:
     - `process.platform === "win32"` and `!process.env.PORTABLE_EXECUTABLE_DIR` (NSIS installer only), OR
     - `process.platform === "darwin"`, OR
     - `process.platform === "linux"` and `Boolean(process.env.APPIMAGE)` and the AppImage path is writable (excluding `/nix/store/...`).
   - On startup (non-blocking, e.g. 5s after window ready) and on manual "Check for Updates":
     - If `electron-updater` is active: emit IPC events (`update-available`, `download-progress`, `update-downloaded`, `update-error`) to renderer. Download & `quitAndInstall()` happen **only** on explicit user click.
     - Otherwise (Windows portable, Linux `tar.gz`, Nix, dev): use `CheckUpdates` from `conduitd` to show a non-intrusive notice with a button to open the GitHub Release URL.

### 5.5. "Acerca de" (About) View

- Accessible from Settings (`#view-settings`) or a dedicated About modal/section.
- Displays:
  - **Conduit GUI** version & packaging/platform info
  - **Conduit Core (`conduitd`)** version, git commit (short), and `SDK Protocol vX`
  - **Conduit TUI** version (if detected, or `"Not installed"`)
  - **Installed Plugins** list with ID, source (`Official` / `Third-Party`), installed version, latest version, and compatibility status
  - **"Check for Updates"** button, last checked timestamp, and direct action button when an app or plugin update is available.

## 6. Architect Calls Made (Reviewable Defaults)

- **CI protocol version sourcing:** `update-registry.yml` reads `min_protocol_version` / `max_protocol_version` / `min_core_version` / `dependencies` from `repository_dispatch` `client_payload` or a release metadata asset if present, and otherwise preserves the existing values in `registry.json` (defaulting to `1`).
- **Windows portable & Nix AppImage exclusion:** `electron-updater` is disabled when `PORTABLE_EXECUTABLE_DIR` is set or when `APPIMAGE` points to a non-writable path (such as `/nix/store/...`); both fall back to the release-link notification.
- **Hot-update account reconnection:** On `UpdatePlugin`, `conduitd` snapshots connected account IDs before stopping the plugin and performs best-effort `ConnectAccount` after relaunching the updated binary (any account needing interactive QR/2FA stays disconnected with its status surfaced).
- **New plugin badge persistence:** `seen_catalog_plugins` is persisted in SQLite settings and updated via `MarkPluginsSeen` when the user opens `#view-plugins`, so dismissed optional plugins do not leave a permanent badge.

## 7. Left to the Builder

- Exact protobuf field numbers in `sdk/proto/conduit.proto` (appending to existing messages without reusing numbers).
- Exact UI layout, CSS styling, and placement of the About panel (inside `#view-settings` vs modal triggered from Settings/sidebar).
- Semver comparison helper choice in Go (e.g. `golang.org/x/mod/semver`).
- Unit and integration tests for `buildinfo`, `registry` + `plugins-manifest.json` sync/fallback, dependency validation, `Manager.StopPlugin`, atomic `Installer.Update`/`Remove`, gRPC handlers, and GUI tests (`gui/test/`).
