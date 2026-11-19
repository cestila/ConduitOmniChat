package rpc

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/cestila/ConduitOmniChat/core/pkg/buildinfo"
	"github.com/cestila/ConduitOmniChat/core/pkg/plugininstall"
	"github.com/cestila/ConduitOmniChat/core/pkg/pluginmgr"
	conduitv1 "github.com/cestila/ConduitOmniChat/sdk/conduitv1"
	"github.com/cestila/ConduitOmniChat/sdk/pluginsdk"
	"google.golang.org/grpc"
)

type fakeConnectPluginClient struct {
	conduitv1.PluginServiceClient
	connectCalls []*conduitv1.ConnectRequest
}

func (f *fakeConnectPluginClient) Connect(_ context.Context, req *conduitv1.ConnectRequest, _ ...grpc.CallOption) (*conduitv1.ConnectResponse, error) {
	f.connectCalls = append(f.connectCalls, req)
	return &conduitv1.ConnectResponse{Success: true, Message: "reconnected"}, nil
}

func makeTarGz(t *testing.T, binName string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	if err := tw.WriteHeader(&tar.Header{
		Name: binName,
		Mode: 0o755,
		Size: int64(len(content)),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func newTestReleaseServer(t *testing.T, repo, tag, binName string, content []byte, corruptChecksum bool) *httptest.Server {
	t.Helper()
	archiveName := fmt.Sprintf("conduit-plugin-telegram_%s_%s_%s.tar.gz", strings.TrimPrefix(tag, "v"), runtime.GOOS, runtime.GOARCH)
	archiveBytes := makeTarGz(t, binName, content)
	sum := sha256.Sum256(archiveBytes)
	hexSum := hex.EncodeToString(sum[:])
	if corruptChecksum {
		hexSum = strings.Repeat("0", 64)
	}
	checksumsBody := fmt.Sprintf("%s  %s\n", hexSum, archiveName)

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/" + repo + "/releases/latest":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tag_name": tag,
				"assets": []map[string]string{
					{"name": archiveName, "browser_download_url": srv.URL + "/download/" + archiveName},
					{"name": "checksums.txt", "browser_download_url": srv.URL + "/download/checksums.txt"},
				},
			})
		case "/download/" + archiveName:
			_, _ = w.Write(archiveBytes)
		case "/download/checksums.txt":
			_, _ = w.Write([]byte(checksumsBody))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestUninstallPluginRejectsManagedExternallyAndSucceedsForManaged(t *testing.T) {
	t.Run("rejects externally managed plugin", func(t *testing.T) {
		srv, _ := newMediaTestServer(t)
		managedDir := t.TempDir()
		inst := plugininstall.New(managedDir)
		srv.SetPluginInstaller(inst)

		mgr := pluginmgr.NewManager()
		mgr.RegisterInstance("telegram", &pluginmgr.Instance{
			Name:     "telegram",
			Path:     "/usr/local/bin/conduit-plugin-telegram",
			Manifest: &conduitv1.PluginManifest{Id: "telegram", Version: "1.0.0"},
		})
		srv.plugins = mgr

		stopped := false
		srv.SetPluginStopper(func(id string) error {
			stopped = true
			return nil
		})

		resp, err := srv.UninstallPlugin(context.Background(), &conduitv1.UninstallPluginRequest{PluginId: "telegram"})
		if err != nil {
			t.Fatalf("UninstallPlugin error: %v", err)
		}
		if resp.Success {
			t.Fatalf("expected managed_externally plugin uninstall to be rejected, got %+v", resp)
		}
		if stopped {
			t.Fatal("StopPlugin must not be called for externally managed plugin")
		}
	})

	t.Run("succeeds for managed plugin and clears manifest store", func(t *testing.T) {
		srv, _ := newMediaTestServer(t)
		managedDir := t.TempDir()
		binPath := filepath.Join(managedDir, "conduit-plugin-telegram")
		if err := os.WriteFile(binPath, []byte("bin"), 0o755); err != nil {
			t.Fatal(err)
		}

		inst := plugininstall.New(managedDir)
		srv.SetPluginInstaller(inst)

		store := pluginmgr.NewManifestStore(filepath.Join(t.TempDir(), "plugins-manifest.json"))
		if err := store.UpsertThirdParty("telegram", "cestila/conduit-plugin-telegram", "1.0.0"); err != nil {
			t.Fatal(err)
		}
		srv.SetManifestStore(store)

		mgr := pluginmgr.NewManager()
		mgr.RegisterInstance("telegram", &pluginmgr.Instance{
			Name:     "telegram",
			Path:     binPath,
			Manifest: &conduitv1.PluginManifest{Id: "telegram", Version: "1.0.0"},
		})
		srv.plugins = mgr

		var stoppedID string
		srv.SetPluginStopper(func(id string) error {
			stoppedID = id
			return mgr.StopPlugin(id)
		})

		resp, err := srv.UninstallPlugin(context.Background(), &conduitv1.UninstallPluginRequest{PluginId: "telegram"})
		if err != nil {
			t.Fatalf("UninstallPlugin error: %v", err)
		}
		if !resp.Success {
			t.Fatalf("expected uninstall success, got %+v", resp)
		}
		if stoppedID != "telegram" {
			t.Fatalf("stoppedID = %q, want telegram", stoppedID)
		}
		if _, err := os.Stat(binPath); !os.IsNotExist(err) {
			t.Fatalf("expected binary %s to be removed", binPath)
		}
		m, err := store.Load()
		if err != nil {
			t.Fatal(err)
		}
		if _, exists := m.Plugins["telegram"]; exists {
			t.Fatal("expected telegram to be removed from ManifestStore")
		}
	})
}

func TestUpdatePluginAbortsOnChecksumMismatchWithoutStopping(t *testing.T) {
	srv, _ := newMediaTestServer(t)
	managedDir := t.TempDir()
	binPath := filepath.Join(managedDir, "conduit-plugin-telegram")
	if err := os.WriteFile(binPath, []byte("old-bin"), 0o755); err != nil {
		t.Fatal(err)
	}

	gh := newTestReleaseServer(t, "cestila/conduit-plugin-telegram", "v2.0.0", "conduit-plugin-telegram", []byte("new-bin"), true)
	inst := plugininstall.New(managedDir)
	inst.APIBase = gh.URL
	srv.SetPluginInstaller(inst)

	mgr := pluginmgr.NewManager()
	mgr.RegisterInstance("telegram", &pluginmgr.Instance{
		Name:     "telegram",
		Path:     binPath,
		Manifest: &conduitv1.PluginManifest{Id: "telegram", Version: "1.0.0", Provider: conduitv1.Provider_PROVIDER_TELEGRAM},
	})
	srv.plugins = mgr

	stopped := false
	srv.SetPluginStopper(func(id string) error {
		stopped = true
		return nil
	})

	resp, err := srv.UpdatePlugin(context.Background(), &conduitv1.UpdatePluginRequest{PluginId: "telegram"})
	if err != nil {
		t.Fatalf("UpdatePlugin error: %v", err)
	}
	if resp.Success {
		t.Fatalf("expected failure on checksum mismatch, got %+v", resp)
	}
	if stopped {
		t.Fatal("StopPlugin must NOT be called when PrepareUpdate fails")
	}
	gotBin, _ := os.ReadFile(binPath)
	if string(gotBin) != "old-bin" {
		t.Fatalf("binary modified on failed update: %q", string(gotBin))
	}
}

func TestUpdatePluginSuccessStopsCommitsRelaunchesAndReconnects(t *testing.T) {
	srv, db := newMediaTestServer(t)
	ctx := context.Background()
	managedDir := t.TempDir()
	binPath := filepath.Join(managedDir, "conduit-plugin-telegram")
	if err := os.WriteFile(binPath, []byte("old-bin"), 0o755); err != nil {
		t.Fatal(err)
	}

	gh := newTestReleaseServer(t, "cestila/conduit-plugin-telegram", "v2.0.0", "conduit-plugin-telegram", []byte("new-bin"), false)
	inst := plugininstall.New(managedDir)
	inst.APIBase = gh.URL
	srv.SetPluginInstaller(inst)

	store := pluginmgr.NewManifestStore(filepath.Join(t.TempDir(), "plugins-manifest.json"))
	srv.SetManifestStore(store)

	mgr := pluginmgr.NewManager()
	oldClient := &fakeConnectPluginClient{}
	mgr.RegisterInstance("telegram", &pluginmgr.Instance{
		Name:     "telegram",
		Path:     binPath,
		RPC:      oldClient,
		Manifest: &conduitv1.PluginManifest{Id: "telegram", Version: "1.0.0", Provider: conduitv1.Provider_PROVIDER_TELEGRAM},
	})
	srv.plugins = mgr

	// Seed a connected account for telegram.
	addResp, err := srv.AddAccount(ctx, &conduitv1.AddAccountRequest{
		Provider:   conduitv1.Provider_PROVIDER_TELEGRAM,
		Label:      "tg-main",
		Identifier: "+5491112345678",
	})
	if err != nil {
		t.Fatal(err)
	}
	acc := addResp.Account
	acc.Status = "connected"
	if err := db.SaveAccount(ctx, acc); err != nil {
		t.Fatal(err)
	}

	var events []string
	srv.SetPluginStopper(func(id string) error {
		events = append(events, "stop:"+id)
		return mgr.StopPlugin(id)
	})

	newClient := &fakeConnectPluginClient{}
	srv.SetPluginLoader(func(_ context.Context, path string) error {
		events = append(events, "load:"+path)
		mgr.RegisterInstance("telegram", &pluginmgr.Instance{
			Name:     "telegram",
			Path:     path,
			RPC:      newClient,
			Manifest: &conduitv1.PluginManifest{Id: "telegram", Version: "2.0.0", Provider: conduitv1.Provider_PROVIDER_TELEGRAM},
		})
		return nil
	})

	resp, err := srv.UpdatePlugin(ctx, &conduitv1.UpdatePluginRequest{PluginId: "telegram"})
	if err != nil {
		t.Fatalf("UpdatePlugin error: %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected update success, got %+v", resp)
	}
	if len(resp.Results) != 1 || !resp.Results[0].Success || resp.Results[0].ToVersion != "v2.0.0" {
		t.Fatalf("unexpected results: %+v", resp.Results)
	}
	gotBin, _ := os.ReadFile(binPath)
	if string(gotBin) != "new-bin" {
		t.Fatalf("binary = %q, want new-bin", string(gotBin))
	}
	if len(events) != 2 || events[0] != "stop:telegram" || events[1] != "load:"+binPath {
		t.Fatalf("unexpected lifecycle order: %v", events)
	}
	if len(newClient.connectCalls) != 1 || newClient.connectCalls[0].AccountIdentifier != "+5491112345678" {
		t.Fatalf("expected best-effort reconnect on relaunched plugin, got %+v", newClient.connectCalls)
	}
}

func TestUpdatePluginRejectsBothPluginIDAndAll(t *testing.T) {
	srv, _ := newMediaTestServer(t)
	srv.SetPluginInstaller(plugininstall.New(t.TempDir()))
	resp, err := srv.UpdatePlugin(context.Background(), &conduitv1.UpdatePluginRequest{
		PluginId: "telegram",
		All:      true,
	})
	if err != nil {
		t.Fatalf("unexpected gRPC error: %v", err)
	}
	if resp.Success {
		t.Fatalf("expected validation failure when both plugin_id and all are set, got %+v", resp)
	}
}

func TestInstallPluginAcceptsThirdPartyAndRejectsIncompatible(t *testing.T) {
	t.Run("accepts third-party owner/conduit-plugin-foo and persists manifest", func(t *testing.T) {
		srv, _ := newMediaTestServer(t)
		fi := &fakePluginInstaller{}
		srv.SetPluginInstaller(fi)
		store := pluginmgr.NewManifestStore(filepath.Join(t.TempDir(), "plugins-manifest.json"))
		srv.SetManifestStore(store)

		resp, err := srv.InstallPlugin(context.Background(), &conduitv1.InstallPluginRequest{PluginId: "acme/conduit-plugin-foo"})
		if err != nil || !resp.Success {
			t.Fatalf("resp=%+v err=%v", resp, err)
		}
		m, err := store.Load()
		if err != nil {
			t.Fatal(err)
		}
		rec, ok := m.Plugins["foo"]
		if !ok || rec.Source != pluginmgr.SourceThirdParty || rec.Repository != "acme/conduit-plugin-foo" {
			t.Fatalf("third-party manifest record not persisted: %+v (ok=%v)", rec, ok)
		}
	})

	t.Run("rejects incompatible min_core_version", func(t *testing.T) {
		origVer := buildinfo.Version
		buildinfo.Version = "1.0.0"
		t.Cleanup(func() { buildinfo.Version = origVer })

		regSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/" {
				http.NotFound(w, r)
				return
			}
			_ = json.NewEncoder(w).Encode(pluginmgr.Registry{
				SchemaVersion: 1,
				Plugins: []pluginmgr.RegistryPlugin{
					{
						ID:             "telegram",
						Name:           "Telegram",
						Repository:     "cestila/conduit-plugin-telegram",
						LatestVersion:  "2.0.0",
						MinCoreVersion: "99.0.0",
					},
				},
			})
		}))
		defer regSrv.Close()

		srv, _ := newMediaTestServer(t)
		fi := &fakePluginInstaller{}
		srv.SetPluginInstaller(fi)
		srv.SetRegistryClient(pluginmgr.NewRegistryClient(regSrv.URL, regSrv.URL, time.Minute))

		resp, err := srv.InstallPlugin(context.Background(), &conduitv1.InstallPluginRequest{PluginId: "telegram"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Success || fi.spec != "" {
			t.Fatalf("expected incompatible plugin to be rejected before install, got resp=%+v spec=%q", resp, fi.spec)
		}
	})

	t.Run("rejects missing dependency", func(t *testing.T) {
		regSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/" {
				http.NotFound(w, r)
				return
			}
			_ = json.NewEncoder(w).Encode(pluginmgr.Registry{
				SchemaVersion: 1,
				Plugins: []pluginmgr.RegistryPlugin{
					{
						ID:           "telegram",
						Name:         "Telegram",
						Repository:   "cestila/conduit-plugin-telegram",
						Dependencies: []pluginmgr.PluginDependency{{PluginID: "whatsapp", MinVersion: "1.0.0"}},
					},
				},
			})
		}))
		defer regSrv.Close()

		srv, _ := newMediaTestServer(t)
		fi := &fakePluginInstaller{}
		srv.SetPluginInstaller(fi)
		srv.SetRegistryClient(pluginmgr.NewRegistryClient(regSrv.URL, regSrv.URL, time.Minute))

		resp, err := srv.InstallPlugin(context.Background(), &conduitv1.InstallPluginRequest{PluginId: "telegram"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Success || fi.spec != "" {
			t.Fatalf("expected missing dependency to reject install, got resp=%+v spec=%q", resp, fi.spec)
		}
	})
}

func TestCheckUpdatesOfflineReturnsUnreachableGracefully(t *testing.T) {
	srv, _ := newMediaTestServer(t)
	// Closed server URL simulates offline environment.
	closedSrv := httptest.NewServer(http.NewServeMux())
	closedURL := closedSrv.URL
	closedSrv.Close()

	srv.SetRegistryClient(pluginmgr.NewRegistryClient(closedURL, closedURL, time.Minute))
	store := pluginmgr.NewManifestStore(filepath.Join(t.TempDir(), "plugins-manifest.json"))
	srv.SetManifestStore(store)

	resp, err := srv.CheckUpdates(context.Background(), &conduitv1.CheckUpdatesRequest{ForceRefresh: true})
	if err != nil {
		t.Fatalf("CheckUpdates must not fail when offline, got err=%v", err)
	}
	if resp.RegistryReachable {
		t.Fatal("expected RegistryReachable=false when offline")
	}
	if len(resp.Plugins) == 0 {
		t.Fatal("expected fallback catalog plugins when offline")
	}
}

func TestMarkPluginsSeenClearsIsNew(t *testing.T) {
	srv, _ := newMediaTestServer(t)
	ctx := context.Background()

	before := listPluginsByID(t, srv)
	if before["telegram"] == nil || !before["telegram"].IsNew {
		t.Fatalf("expected unseen catalog plugin telegram to have IsNew=true, got %+v", before["telegram"])
	}

	markResp, err := srv.MarkPluginsSeen(ctx, &conduitv1.MarkPluginsSeenRequest{})
	if err != nil || !markResp.Success {
		t.Fatalf("MarkPluginsSeen failed: resp=%+v err=%v", markResp, err)
	}

	after := listPluginsByID(t, srv)
	if after["telegram"] == nil || after["telegram"].IsNew {
		t.Fatalf("expected telegram IsNew=false after MarkPluginsSeen, got %+v", after["telegram"])
	}
}

func TestGetStatusReturnsBuildAndProtocolAndTUIVersion(t *testing.T) {
	origVer := buildinfo.Version
	buildinfo.Version = "v1.2.3"
	t.Cleanup(func() { buildinfo.Version = origVer })

	srv, _ := newMediaTestServer(t)
	srv.SetTUIVersion("v1.2.0")

	resp, err := srv.GetStatus(context.Background(), &conduitv1.GetStatusRequest{})
	if err != nil {
		t.Fatalf("GetStatus error: %v", err)
	}
	if resp.CoreVersion != "v1.2.3" {
		t.Fatalf("CoreVersion = %q, want v1.2.3", resp.CoreVersion)
	}
	if resp.ProtocolVersion != pluginsdk.ProtocolVersion {
		t.Fatalf("ProtocolVersion = %d, want %d", resp.ProtocolVersion, pluginsdk.ProtocolVersion)
	}
	if resp.TuiVersion != "v1.2.0" {
		t.Fatalf("TuiVersion = %q, want v1.2.0", resp.TuiVersion)
	}
}

func TestCheckUpdatesGuiVersionComparison(t *testing.T) {
	regSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"schema_version": 1,
			"gui": {"version": "0.1.2", "release_url": "https://github.com/cestila/ConduitOmniChat/releases/tag/v0.1.2"},
			"plugins": []
		}`))
	}))
	defer regSrv.Close()

	srv, _ := newMediaTestServer(t)
	srv.SetTUIVersion("0.0.1") // Must be ignored for gui_update_available.
	srv.SetRegistryClient(pluginmgr.NewRegistryClient(regSrv.URL, regSrv.URL, time.Minute))
	srv.SetManifestStore(pluginmgr.NewManifestStore(filepath.Join(t.TempDir(), "plugins-manifest.json")))

	cases := []struct {
		name       string
		guiVersion string
		wantUpdate bool
	}{
		{name: "older gui version", guiVersion: "0.1.1", wantUpdate: true},
		{name: "equal gui version", guiVersion: "0.1.2", wantUpdate: false},
		{name: "newer gui version", guiVersion: "0.2.0", wantUpdate: false},
		{name: "empty gui version", guiVersion: "", wantUpdate: false},
		{name: "dev gui version", guiVersion: "dev", wantUpdate: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := srv.CheckUpdates(context.Background(), &conduitv1.CheckUpdatesRequest{
				ForceRefresh: true,
				GuiVersion:   tc.guiVersion,
			})
			if err != nil {
				t.Fatalf("CheckUpdates error: %v", err)
			}
			if resp.GuiUpdateAvailable != tc.wantUpdate {
				t.Fatalf("GuiUpdateAvailable = %v, want %v (guiVersion=%q, latest=%q)",
					resp.GuiUpdateAvailable, tc.wantUpdate, tc.guiVersion, resp.GuiLatestVersion)
			}
		})
	}
}

func TestCheckUpdatesResolvesOfficialPluginsDirectlyFromRepoReleases(t *testing.T) {
	var srvHTTP *httptest.Server
	srvHTTP = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/registry.json":
			_ = json.NewEncoder(w).Encode(pluginmgr.Registry{
				SchemaVersion: 1,
				Core:          pluginmgr.ComponentRelease{Version: "0.2.0", Tag: "v0.2.0"},
				GUI:           pluginmgr.ComponentRelease{Version: "0.2.0", Tag: "v0.2.0"},
				Plugins: []pluginmgr.RegistryPlugin{
					{
						ID:                 "whatsapp",
						Name:               "WhatsApp",
						Repository:         "cestila/conduit-plugin-whatsapp",
						LatestVersion:      "0.1.2",
						MinProtocolVersion: 1,
						MaxProtocolVersion: 1,
					},
					{
						ID:                 "telegram",
						Name:               "Telegram",
						Repository:         "cestila/conduit-plugin-telegram",
						LatestVersion:      "0.1.2",
						MinProtocolVersion: 1,
						MaxProtocolVersion: 1,
					},
				},
			})
		case "/repos/cestila/ConduitOmniChat/releases/latest":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tag_name": "v0.2.1",
				"html_url": "https://github.com/cestila/ConduitOmniChat/releases/tag/v0.2.1",
			})
		case "/repos/cestila/conduit-plugin-whatsapp/releases/latest":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tag_name": "v0.1.4",
				"html_url": "https://github.com/cestila/conduit-plugin-whatsapp/releases/tag/v0.1.4",
				"assets": []map[string]string{
					{"name": "plugin-metadata.json", "browser_download_url": srvHTTP.URL + "/assets/wa.json"},
				},
			})
		case "/assets/wa.json":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"min_protocol_version": 1,
				"max_protocol_version": 1,
				"min_core_version":     "0.1.0",
				"dependencies":         []pluginmgr.PluginDependency{},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srvHTTP.Close()

	srv, _ := newMediaTestServer(t)
	mgr := pluginmgr.NewManager()
	mgr.RegisterInstance("whatsapp", &pluginmgr.Instance{
		Name:     "whatsapp",
		Path:     filepath.Join(t.TempDir(), "conduit-plugin-whatsapp"),
		Manifest: &conduitv1.PluginManifest{Id: "whatsapp", Version: "0.1.3", Provider: conduitv1.Provider_PROVIDER_WHATSAPP},
	})
	srv.plugins = mgr
	srv.SetRegistryClient(pluginmgr.NewRegistryClient(srvHTTP.URL+"/registry.json", srvHTTP.URL, time.Minute))
	srv.SetManifestStore(pluginmgr.NewManifestStore(filepath.Join(t.TempDir(), "plugins-manifest.json")))

	resp, err := srv.CheckUpdates(context.Background(), &conduitv1.CheckUpdatesRequest{
		ForceRefresh: true,
		GuiVersion:   "0.2.0",
	})
	if err != nil {
		t.Fatalf("CheckUpdates error: %v", err)
	}
	if !resp.RegistryReachable {
		t.Fatal("expected RegistryReachable=true")
	}
	if resp.CoreLatestVersion != "0.2.1" || !resp.GuiUpdateAvailable {
		t.Fatalf("expected live Core/GUI 0.2.1 update, got core=%q guiUpdate=%v", resp.CoreLatestVersion, resp.GuiUpdateAvailable)
	}
	var wa *conduitv1.ProtoPluginInfo
	for _, p := range resp.Plugins {
		if p.Id == "whatsapp" {
			wa = p
			break
		}
	}
	if wa == nil {
		t.Fatal("missing whatsapp plugin in CheckUpdates response")
	}
	if wa.LatestVersion != "0.1.4" || !wa.UpdateAvailable || !wa.Compatible {
		t.Fatalf("expected live whatsapp 0.1.4 updateable+compatible, got %+v", wa)
	}
}
