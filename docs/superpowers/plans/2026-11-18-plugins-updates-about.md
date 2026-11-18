# Plugin Lifecycle, Remote & Local Registry, Dependency Checks, System Versioning & About View Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver complete plugin install/uninstall/update (for both official and third-party plugins) with dependency validation, non-silent update notifications, build-time version injection across all components, an About view, and `electron-updater` integration.

**Architecture:** `conduitd` acts as the single source of truth for plugin state by merging a CI-maintained remote `registry.json` with a persistent local `<dataDir>/plugins-manifest.json` (which also tracks third-party `owner/conduit-plugin-<id>` repos and dependency/protocol constraints). Updates are never silent: `conduitd` and `electron-updater` (`autoDownload: false`) only check and notify, executing downloads and atomic replacements only when triggered by the user.

**Tech Stack:** Go (`core`, `sdk`), gRPC / Protobuf (`sdk/proto/conduit.proto`), Electron + Node.js (`gui`, `electron-updater`), GitHub Actions (`.github/workflows/release.yml`, `update-registry.yml`), Nix (`flake.nix`), GoReleaser (`.goreleaser.yaml`).

**Spec:** [2026-11-18-plugins-updates-about-design.md](file:///home/cestila/workspace/Personales/ConduitOmniChat/docs/superpowers/specs/2026-11-18-plugins-updates-about-design.md)

## Global Constraints

- **No silent auto-installs:** `autoUpdater.autoDownload = false`, `autoUpdater.autoInstallOnAppQuit = false`; plugin and app updates only execute on explicit user action.
- **Atomic verified updates:** Download archive + `checksums.txt` to temp dir and verify SHA-256 **before** stopping the running plugin or modifying the existing binary on disk.
- **Externally managed binaries:** Plugins outside user-writable `PluginDir` (e.g., `/nix/store/...`) are marked `managed_externally = true`; `Update` and `Uninstall` must reject them cleanly.
- **Packaging guardrails for `electron-updater`:** Enable `electron-updater` only when `app.isPackaged` and (`win32` without `PORTABLE_EXECUTABLE_DIR`, `darwin`, or `linux` with writable `APPIMAGE` outside `/nix/store/`).
- **Single source of truth:** Do not duplicate catalog/semver/dependency logic in `gui/`; keep it in `conduitd`.
- **Proto codegen:** Every change to `sdk/proto/conduit.proto` must be regenerated via `make proto`.

## Review Focus

- **Offline / unreachable `registry.json`:** `CheckUpdates` and `ListPlugins` must fall back to `<dataDir>/plugins-manifest.json` and embedded `pluginmgr.Catalog` without returning an RPC error, setting `registry_reachable = false`.
- **Corrupted or mismatched SHA-256 during `UpdatePlugin`:** Must abort before calling `Manager.StopPlugin`, leaving the currently running plugin binary intact and running.
- **Incompatible `protocol_version` or `min_core_version`:** `ListPlugins` / `CheckUpdates` must set `compatible = false` with an explicit `incompatibility_reason`, and `InstallPlugin`/`UpdatePlugin` must reject incompatible targets.
- **Externally managed plugin binary (read-only / Nix store):** `managed_externally = true` must be set when `Path` is outside `PluginDir`, and `UninstallPlugin`/`UpdatePlugin` must return a clear error rather than `EACCES`.
- **Non-self-updatable Electron runtime (`PORTABLE_EXECUTABLE_DIR`, `/nix/store` `APPIMAGE`, `tar.gz`, dev):** `gui/main.js` must not invoke `electron-updater` and must instead fall back to the release-link notification from `CheckUpdates`.

---

### Task 1: Build-Time Versioning (`buildinfo`), `conduitd version` CLI & Build Pipeline Injection

**Files:**
- Create: `core/pkg/buildinfo/buildinfo.go`
- Create: `core/pkg/buildinfo/buildinfo_test.go`
- Modify: `core/cmd/conduitd/cli.go:26-75`
- Modify: `core/cmd/conduitd/cli_test.go`
- Modify: `Makefile:1-20`
- Modify: `.goreleaser.yaml:15-17`
- Modify: `flake.nix:93-122`
- Modify: `.github/workflows/release.yml:49-60`

**Interfaces:**
- Produces:
  - `buildinfo.Version` (`string`, default `"dev"`), `buildinfo.Commit` (`string`, default `"none"`), `buildinfo.Date` (`string`, default `"unknown"`)
  - `buildinfo.Summary() string` (returns formatted version string e.g. `"v0.1.2 (commit abc1234, built 2026-11-18)"` or `"dev"`)

- [ ] **Step 1: Write failing test for `buildinfo.Summary()` and `conduitd version` CLI**

In `core/pkg/buildinfo/buildinfo_test.go`:
```go
func TestSummaryDefaultsAndInjected(t *testing.T) {
	if got := Summary(); got == "" {
		t.Fatal("expected non-empty summary")
	}
}
```
In `core/cmd/conduitd/cli_test.go`, add `TestRunCLIVersion` verifying `conduitd version` and `conduitd --version` exit `0` and print `buildinfo.Version`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd core && go test ./pkg/buildinfo/... ./cmd/conduitd -run "TestSummaryDefaultsAndInjected|TestRunCLIVersion"`
Expected: FAIL (package `buildinfo` does not exist / unknown command `"version"`).

- [ ] **Step 3: Implement `buildinfo` and `conduitd version` CLI**

Create `core/pkg/buildinfo/buildinfo.go` with `Version`, `Commit`, `Date` vars and `Summary() string`. Update `core/cmd/conduitd/cli.go` to handle `"version"`, `"--version"`, `"-v"` and list `version` in `cliUsage` / `cliHelp`.

- [ ] **Step 4: Wire `-ldflags` in `Makefile`, `.goreleaser.yaml`, `flake.nix`, and `.github/workflows/release.yml`**

Inject `-X github.com/cestila/ConduitOmniChat/core/pkg/buildinfo.Version=...`, `.Commit=...`, `.Date=...` in all four build definitions.

- [ ] **Step 5: Run tests and verify build**

Run: `cd core && go test ./pkg/buildinfo/... ./cmd/conduitd/... && make daemon && ./bin/conduitd version`
Expected: PASS and `./bin/conduitd version` prints the git tag/version.

- [ ] **Step 6: Commit**

```bash
git add core/pkg/buildinfo core/cmd/conduitd/cli.go core/cmd/conduitd/cli_test.go Makefile .goreleaser.yaml flake.nix .github/workflows/release.yml
git commit -m "feat(core): inject build-time version via ldflags and add conduitd version command"
```

---

### Task 2: Remote Registry, Local Manifest (`plugins-manifest.json`) & Dependency/Semver Checker

**Files:**
- Create: `registry.json`
- Create: `core/pkg/pluginmgr/compat.go`
- Create: `core/pkg/pluginmgr/compat_test.go`
- Create: `core/pkg/pluginmgr/registry.go`
- Create: `core/pkg/pluginmgr/registry_test.go`

**Interfaces:**
- Consumes: `pluginmgr.Catalog` (`core/pkg/pluginmgr/catalog.go`), `pluginsdk.ProtocolVersion` (`sdk/pluginsdk/pluginsdk.go`), `buildinfo.Version`
- Produces:
  - `type PluginDependency struct { PluginID string; MinVersion string }`
  - `type RegistryPlugin struct { ID, Name, Description, Repository, LatestVersion, ReleaseURL, MinCoreVersion string; MinProtocolVersion, MaxProtocolVersion uint32; Dependencies []PluginDependency }`
  - `type Registry struct { SchemaVersion int; UpdatedAt string; Core, GUI ComponentRelease; Plugins []RegistryPlugin }`
  - `type LocalPluginRecord struct { ID, Repository, Source, InstalledVersion, LatestVersion, MinCoreVersion, ReleaseURL, LastCheckedAt string; MinProtocolVersion, MaxProtocolVersion uint32; Dependencies []PluginDependency }`
  - `type ManifestStore struct { ... }` with `NewManifestStore(path string)`, `Load() (*LocalManifest, error)`, `Save(*LocalManifest) error`, `UpsertThirdParty(id, repo, version string) error`, `Remove(id string) error`, `MergeRemote(*Registry)`
  - `type RegistryClient struct { ... }` with `NewRegistryClient(url, githubAPIBase string, ttl time.Duration)`, `Fetch(ctx context.Context, force bool) (*Registry, bool, error)` (returns registry, `reachable bool`, error), `FetchLatestRepoVersion(ctx context.Context, repo string) (version, releaseURL string, err error)`
  - `CheckCompatibility(entry RegistryPlugin, coreVersion string, protoVersion uint32, installedVersions map[string]string) (compatible bool, reason string)`
  - `CompareSemver(a, b string) int`

- [ ] **Step 1: Write failing tests for semver comparison, dependency/protocol compatibility, `ManifestStore`, and `RegistryClient` (with `httptest.Server` + offline fallback)**

Cover:
- `CompareSemver("v0.1.2", "0.1.1") > 0`
- `CheckCompatibility` failing when `protoVersion` < `MinProtocolVersion` or > `MaxProtocolVersion`, when `coreVersion` < `MinCoreVersion` (and skipping when `coreVersion == "dev"`), and when a `PluginDependency` is missing or below `MinVersion`.
- `ManifestStore` persisting third-party `owner/conduit-plugin-<id>` records across `Save`/`Load` and merging remote `Registry` entries without overwriting third-party records.
- `RegistryClient.Fetch` caching within TTL, bypassing on `force=true`, and returning `reachable=false` on HTTP 500 / timeout without panicking.

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd core && go test ./pkg/pluginmgr/... -run "TestCompareSemver|TestCheckCompatibility|TestManifestStore|TestRegistryClient"`
Expected: FAIL (undefined symbols).

- [ ] **Step 3: Implement `compat.go`, `registry.go`, and initial `registry.json`**

Implement `CompareSemver`, `CheckCompatibility`, `ManifestStore`, and `RegistryClient` (respecting `CONDUIT_REGISTRY_URL` and `CONDUIT_GITHUB_API`, 5s HTTP timeout, 15m TTL cache). Seed `registry.json` at repo root with current `0.1.2` core/gui and `whatsapp`/`telegram` entries.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd core && go test ./pkg/pluginmgr/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add registry.json core/pkg/pluginmgr/compat.go core/pkg/pluginmgr/compat_test.go core/pkg/pluginmgr/registry.go core/pkg/pluginmgr/registry_test.go
git commit -m "feat(pluginmgr): add remote registry client, local manifest store and dependency compatibility checks"
```

---

### Task 3: Process Lifecycle (`Manager.StopPlugin`) & Atomic `Installer.Update` / Writable Check

**Files:**
- Modify: `core/pkg/pluginmgr/manager.go:160-229`
- Modify: `core/pkg/pluginmgr/plugin_test.go`
- Modify: `core/pkg/plugininstall/installer.go:154-275`
- Modify: `core/pkg/plugininstall/installer_test.go`

**Interfaces:**
- Consumes: `Manager` (`core/pkg/pluginmgr/manager.go`), `Installer` (`core/pkg/plugininstall/installer.go`)
- Produces:
  - `(m *Manager) StopPlugin(id string) error` (kills `Client`, deletes from `m.plugins` and `m.disabled`, returns `nil` if not present)
  - `(in *Installer) IsManagedPath(binaryPath string) bool` (true iff `filepath.Clean(filepath.Dir(binaryPath)) == filepath.Clean(in.PluginDir)`)
  - `(in *Installer) PrepareUpdate(ctx context.Context, spec, version string) (*PreparedInstall, error)` (downloads & verifies SHA-256 into temp file **without** touching `PluginDir`)
  - `(p *PreparedInstall) Commit() (Result, error)` / `(p *PreparedInstall) Cleanup()`

- [ ] **Step 1: Write failing tests for `Manager.StopPlugin`, `Installer.IsManagedPath`, and `PrepareUpdate` + checksum failure leaving existing binary intact**

Add tests in `plugin_test.go` and `installer_test.go`:
- `TestManagerStopPluginRemovesFromActiveAndDisabled`
- `TestInstallerIsManagedPath` (true for `<PluginDir>/conduit-plugin-foo`, false for `/nix/store/.../conduit-plugin-foo`)
- `TestInstallerPrepareUpdateChecksumMismatchPreservesExistingBinary`

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd core && go test ./pkg/pluginmgr/... ./pkg/plugininstall/... -run "TestManagerStopPlugin|TestInstallerIsManagedPath|TestInstallerPrepareUpdate"`
Expected: FAIL.

- [ ] **Step 3: Implement `StopPlugin`, `IsManagedPath`, and two-phase `PrepareUpdate` / `Commit`**

In `installer.go`, extract the download + SHA-256 verification + binary extraction into `PrepareUpdate` (producing a temp file inside `in.PluginDir` or OS temp), and `Commit()` to `chmod 0755` + `os.Rename` over the target path. Refactor `Install` to reuse `PrepareUpdate` + `Commit` so download/checksum logic is not duplicated.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd core && go test ./pkg/pluginmgr/... ./pkg/plugininstall/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add core/pkg/pluginmgr/manager.go core/pkg/pluginmgr/plugin_test.go core/pkg/plugininstall/installer.go core/pkg/plugininstall/installer_test.go
git commit -m "feat(plugininstall): add Manager.StopPlugin, managed-path check and two-phase verified updates"
```

---

### Task 4: Protobuf Contract & Codegen (`sdk/proto/conduit.proto`)

**Files:**
- Modify: `sdk/proto/conduit.proto:45-57`, `sdk/proto/conduit.proto:445-452`, and plugin RPC/message definitions
- Regenerate: `sdk/conduitv1/conduit.pb.go`, `sdk/conduitv1/conduit_grpc.pb.go`

**Interfaces:**
- Produces in `ConduitService`:
  - `UninstallPlugin(UninstallPluginRequest) returns (UninstallPluginResponse)`
  - `UpdatePlugin(UpdatePluginRequest) returns (UpdatePluginResponse)`
  - `CheckUpdates(CheckUpdatesRequest) returns (CheckUpdatesResponse)`
  - `MarkPluginsSeen(MarkPluginsSeenRequest) returns (MarkPluginsSeenResponse)`
- Produces fields on `GetStatusResponse`: `uint32 protocol_version = 4`, `string tui_version = 5`
- Produces fields on `ProtoPluginInfo`: `latest_version`, `update_available`, `is_new`, `compatible`, `incompatibility_reason`, `managed_externally`, `is_third_party`, `release_url`, `dependencies`
- Produces fields on `InstallPluginRequest`: `string version` (optional)

- [ ] **Step 1: Edit `sdk/proto/conduit.proto` adding the new fields and RPCs**

Append new field numbers without modifying or reusing existing field numbers.

- [ ] **Step 2: Regenerate protobuf bindings**

Run: `make proto`
Expected: `sdk/conduitv1/conduit.pb.go` and `conduit_grpc.pb.go` update cleanly with exit code 0.

- [ ] **Step 3: Verify `sdk` and `core` compile**

Run: `cd sdk && go test ./... && cd ../core && go build ./...`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add sdk/proto/conduit.proto sdk/conduitv1/
git commit -m "feat(proto): add plugin update/uninstall/check-updates RPCs and status version fields"
```

---

### Task 5: Core gRPC Handlers, Hot-Update Reconnect, TUI Version Detection & CLI Subcommands

**Files:**
- Modify: `core/pkg/rpc/server.go` (constructor/options, `ListPlugins`, `InstallPlugin`, `UninstallPlugin`, `UpdatePlugin`, `CheckUpdates`, `MarkPluginsSeen`, `GetStatus`)
- Modify: `core/pkg/rpc/server_test.go` (or create `core/pkg/rpc/plugins_updates_test.go`)
- Modify: `core/cmd/conduitd/main.go` (wire `ManifestStore`, `RegistryClient`, `tuiVersion` probe, `pluginStopper` / `pluginLauncher`)
- Modify: `core/cmd/conduitd/cli.go` & `core/cmd/conduitd/cli_test.go` (`conduitd plugin check`, `conduitd plugin update [<id>|--all]`)

**Interfaces:**
- Consumes: `pluginmgr.RegistryClient`, `pluginmgr.ManifestStore`, `pluginmgr.CheckCompatibility`, `pluginmgr.Manager.StopPlugin`, `plugininstall.Installer.PrepareUpdate` / `IsManagedPath` / `Remove`, `buildinfo.Version`
- Produces:
  - Full gRPC `UninstallPlugin`, `UpdatePlugin`, `CheckUpdates`, `MarkPluginsSeen`
  - Updated `ListPlugins` (merges `ManifestStore` + `RegistryClient`, computes `update_available`, `is_new`, `compatible`, `managed_externally`, `is_third_party`)
  - Updated `InstallPlugin` (accepts official ID or `owner/conduit-plugin-<id>`, checks compatibility/dependencies, persists third-party entry in `ManifestStore`)
  - Updated `GetStatus` (returns `buildinfo.Version`, `pluginsdk.ProtocolVersion`, `tuiVersion`)
  - CLI `conduitd plugin check` and `conduitd plugin update [<id>|--all]`

- [ ] **Step 1: Write failing tests for gRPC plugin lifecycle & CLI `plugin check` / `plugin update`**

Test:
- `UninstallPlugin` rejects `managed_externally` plugins and succeeds for writable plugins (calling `StopPlugin` + `Remove` + clearing `ManifestStore`).
- `UpdatePlugin` aborts on checksum mismatch without calling `StopPlugin`; on success stops plugin, commits binary, relaunches, and attempts best-effort `ConnectAccount` for previously connected accounts.
- `InstallPlugin` accepts `owner/conduit-plugin-foo` and rejects incompatible `min_core_version` / missing dependency.
- `CheckUpdates` returns `registry_reachable=false` gracefully when offline and still checks local/cached state.
- `GetStatus` returns `buildinfo.Version` and `pluginsdk.ProtocolVersion`.
- `cli_test.go`: `conduitd plugin check` and `conduitd plugin update --all`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd core && go test ./pkg/rpc/... ./cmd/conduitd/...`
Expected: FAIL.

- [ ] **Step 3: Implement gRPC handlers, `main.go` wiring (including `conduit-tui --version` probe), and CLI `plugin check` / `plugin update`**

Note: For `UpdatePlugin` hot-update reconnect, snapshot connected accounts belonging to the plugin's provider before `StopPlugin`, then after `loader(ctx, path)` call `ConnectAccount` best-effort for each.

- [ ] **Step 4: Run full Go test suite**

Run: `cd core && go test ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add core/pkg/rpc/ core/cmd/conduitd/
git commit -m "feat(core): implement plugin update/uninstall/check-updates RPCs, hot-reconnect and CLI commands"
```

---

### Task 6: GUI Client, IPC & `electron-updater` Integration

**Files:**
- Modify: `gui/package.json` (add `electron-updater` dependency and `build.publish` GitHub config)
- Modify: `gui/conduit-client.js:233-260` (add `uninstallPlugin`, `updatePlugin`, `checkUpdates`, `markPluginsSeen`, update `installPlugin` to pass `version`)
- Modify: `gui/main.js:344-365` (add IPC handlers + `electron-updater` with `autoDownload = false`, `autoInstallOnAppQuit = false`, and packaging mode detection)
- Modify: `gui/preload.js:30-45` (expose `uninstallPlugin`, `updatePlugin`, `checkUpdates`, `markPluginsSeen`, `getAppInfo`, `checkAppUpdate`, `downloadAppUpdate`, `installAppUpdate`, `onAppUpdateEvent`)
- Modify: `.github/workflows/release.yml:66-74` (upload `latest*.yml` alongside `.AppImage`, `.tar.gz`, `.exe`)
- Create: `gui/test/updater-guard.test.js`

**Interfaces:**
- Consumes: `ConduitService` RPCs from Task 4/5
- Produces:
  - `window.conduit.uninstallPlugin(pluginId)`, `updatePlugin(pluginId, all)`, `checkUpdates(forceRefresh)`, `markPluginsSeen(ids)`
  - `window.conduit.getAppInfo()` -> `{ version, platform, packagingMode, selfUpdateSupported }`
  - `window.conduit.checkAppUpdate()`, `downloadAppUpdate()`, `installAppUpdate()`, `onAppUpdateEvent(cb)`

- [ ] **Step 1: Write failing unit test for packaging mode & `electron-updater` activation guard**

In `gui/test/updater-guard.test.js` (extract pure helper `detectPackagingMode({ isPackaged, platform, env })` into `gui/updater-guard.js` so it is unit-testable without Electron runtime):
- `win32` + `PORTABLE_EXECUTABLE_DIR` -> `selfUpdateSupported: false` (`win32-portable`)
- `linux` + `APPIMAGE="/nix/store/abc-conduit.AppImage"` -> `selfUpdateSupported: false` (`nix-appimage`)
- `linux` + `APPIMAGE="/home/user/Conduit.AppImage"` -> `selfUpdateSupported: true` (`appimage`)
- `isPackaged: false` -> `selfUpdateSupported: false` (`dev`)

- [ ] **Step 2: Run test to verify it fails**

Run: `cd gui && node test/updater-guard.test.js`
Expected: FAIL (`Cannot find module '../updater-guard'`).

- [ ] **Step 3: Implement `gui/updater-guard.js`, install `electron-updater`, update `conduit-client.js`, `main.js`, `preload.js`, `package.json`, and `.github/workflows/release.yml`**

Run `cd gui && npm install electron-updater` (check `package-lock.json` updates cleanly). Ensure `autoUpdater.autoDownload = false` and `autoUpdater.autoInstallOnAppQuit = false`.

- [ ] **Step 4: Run GUI tests**

Run: `cd gui && node test/updater-guard.test.js`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add gui/package.json gui/package-lock.json gui/updater-guard.js gui/conduit-client.js gui/main.js gui/preload.js gui/test/updater-guard.test.js .github/workflows/release.yml
git commit -m "feat(gui): add electron-updater guard, plugin lifecycle IPC and release metadata"
```

---

### Task 7: GUI Plugins View, Update Badges, Status Bugfix & About View

**Files:**
- Modify: `gui/src/index.html:269-280`, `gui/src/index.html:528-545` (Plugins view header with custom `owner/repo` install + `Update All`, About section in Settings or dedicated About modal)
- Modify: `gui/src/app.js:150-170` (fix `status.version` -> `status.core_version`), `gui/src/app.js:2737-2830` (`loadPlugins`, `renderPlugins`, update/uninstall/custom-install actions, `markPluginsSeen`, sidebar badge, About view population & app update banner/actions)
- Modify: `gui/src/styles.css` (badges, update banner, About version table)
- Create: `gui/test/plugins-about.test.js`

**Interfaces:**
- Consumes: `window.conduit.*` APIs from Task 6

- [ ] **Step 1: Write failing DOM harness tests in `gui/test/plugins-about.test.js`**

Test:
- Status display uses `status.core_version` (not `status.version` / `v1.0`).
- `renderPlugins` renders **Update** button when `update_available`, disables **Update**/**Uninstall** when `managed_externally`, shows incompatibility reason when `compatible === false`, and renders **Uninstall** confirmation flow.
- About view renders GUI version, Core version, `SDK Protocol v...`, TUI version (or `"Not installed"`), and installed plugins table.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd gui && node test/plugins-about.test.js`
Expected: FAIL.

- [ ] **Step 3: Implement `index.html`, `app.js`, and `styles.css` changes**

- Fix `status.core_version` in `app.js`.
- Add sidebar badge on Plugins nav item (count of `update_available` + `is_new`), clearing `is_new` via `markPluginsSeen` when opening `#view-plugins`.
- Add custom `owner/conduit-plugin-<id>` install input, per-plugin **Update** / **Uninstall** buttons, **Update All** button, and compatibility warnings.
- Add **About** section in `#view-settings` (showing GUI, Core, SDK Protocol, TUI, and installed plugins + "Check for Updates" button + app update download/restart or release-link CTA).

- [ ] **Step 4: Run full GUI test suite**

Run: `cd gui && npm test`
Expected: PASS (all existing tests + `updater-guard.test.js` + `plugins-about.test.js`).

- [ ] **Step 5: Commit**

```bash
git add gui/src/index.html gui/src/app.js gui/src/styles.css gui/test/plugins-about.test.js gui/package.json
git commit -m "feat(gui): add plugin updates/uninstall UI, dependency warnings, About view and fix core_version"
```

---

### Task 8: CI Registry Automation Workflow & `CHANGELOG.md`

**Files:**
- Create: `.github/workflows/update-registry.yml`
- Modify: `CHANGELOG.md`

- [ ] **Step 1: Create `.github/workflows/update-registry.yml`**

Configure triggers (`release: [published]`, `repository_dispatch`, `schedule`, `workflow_dispatch`), authenticated `GITHUB_TOKEN` calls to fetch latest releases for `ConduitOmniChat` and all repos in `registry.json`, preserving `min_protocol_version` / `max_protocol_version` / `min_core_version` / `dependencies` unless supplied in `client_payload` or release metadata asset, and committing `registry.json` when changed.

- [ ] **Step 2: Update `CHANGELOG.md` under `## [Unreleased]`**

Add concise Keep a Changelog entries for Added/Fixed items.

- [ ] **Step 3: Validate workflow YAML & run full project verification**

Run: `make test`
Expected: All Go and GUI tests pass.

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/update-registry.yml CHANGELOG.md
git commit -m "ci: automate registry.json updates on releases and update CHANGELOG"
```
