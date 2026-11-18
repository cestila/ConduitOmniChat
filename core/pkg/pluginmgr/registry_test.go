package pluginmgr

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cestila/ConduitOmniChat/sdk/pluginsdk"
)

func TestManifestStoreSaveLoadAndMergeRemote(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "plugins-manifest.json")
	store := NewManifestStore(path)

	// Missing file returns empty initialized manifest.
	initial, err := store.Load()
	if err != nil {
		t.Fatalf("initial Load() error: %v", err)
	}
	if len(initial.Plugins) != 0 {
		t.Fatalf("expected 0 initial plugins, got %d", len(initial.Plugins))
	}

	// Persist a third-party plugin record.
	if err := store.UpsertThirdParty("custom", "alice/conduit-plugin-custom", "0.1.0"); err != nil {
		t.Fatalf("UpsertThirdParty() error: %v", err)
	}
	// Persist another third-party record colliding with an official ID to verify merge preserves third-party.
	if err := store.UpsertThirdParty("whatsapp", "bob/conduit-plugin-whatsapp", "0.9.0"); err != nil {
		t.Fatalf("UpsertThirdParty() colliding id error: %v", err)
	}

	reloaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load() after upsert error: %v", err)
	}
	customRec, ok := reloaded.Plugins["custom"]
	if !ok {
		t.Fatal("expected custom third-party plugin in reloaded manifest")
	}
	if customRec.Source != SourceThirdParty || customRec.Repository != "alice/conduit-plugin-custom" || customRec.InstalledVersion != "0.1.0" {
		t.Fatalf("unexpected custom record: %+v", customRec)
	}

	// Merge remote registry: official entries added, third-party entries NOT overwritten.
	remote := &Registry{
		SchemaVersion: 1,
		UpdatedAt:     "2026-11-18T12:00:00Z",
		Core:          ComponentRelease{Version: "0.1.2", Tag: "v0.1.2", ReleaseURL: "https://example.com/core"},
		GUI:           ComponentRelease{Version: "0.1.2", Tag: "v0.1.2", ReleaseURL: "https://example.com/gui"},
		Plugins: []RegistryPlugin{
			{
				ID:                 "whatsapp",
				Name:               "WhatsApp Multi-Device",
				Repository:         "cestila/conduit-plugin-whatsapp",
				LatestVersion:      "0.1.2",
				MinProtocolVersion: 1,
				MaxProtocolVersion: 1,
				MinCoreVersion:     "0.1.0",
				ReleaseURL:         "https://example.com/wa",
			},
			{
				ID:                 "telegram",
				Name:               "Telegram MTProto",
				Repository:         "cestila/conduit-plugin-telegram",
				LatestVersion:      "0.1.2",
				MinProtocolVersion: 1,
				MaxProtocolVersion: 1,
				MinCoreVersion:     "0.1.0",
				Dependencies:       []PluginDependency{{PluginID: "whatsapp", MinVersion: "0.1.0"}},
				ReleaseURL:         "https://example.com/tg",
			},
		},
	}
	if err := store.MergeRemote(remote); err != nil {
		t.Fatalf("MergeRemote() error: %v", err)
	}

	merged, err := store.Load()
	if err != nil {
		t.Fatalf("Load() after merge error: %v", err)
	}
	waRec := merged.Plugins["whatsapp"]
	if waRec.Source != SourceThirdParty || waRec.Repository != "bob/conduit-plugin-whatsapp" || waRec.InstalledVersion != "0.9.0" {
		t.Fatalf("third-party record was overwritten by MergeRemote: %+v", waRec)
	}
	tgRec, ok := merged.Plugins["telegram"]
	if !ok || tgRec.Source != SourceOfficial || tgRec.LatestVersion != "0.1.2" || len(tgRec.Dependencies) != 1 {
		t.Fatalf("unexpected merged official telegram record: %+v (ok=%v)", tgRec, ok)
	}
	if merged.Core.Version != "0.1.2" || merged.GUI.Version != "0.1.2" {
		t.Fatalf("expected core/gui versions updated, got core=%+v gui=%+v", merged.Core, merged.GUI)
	}

	// Remove deletes record and persists.
	if err := store.Remove("custom"); err != nil {
		t.Fatalf("Remove() error: %v", err)
	}
	afterRemove, err := store.Load()
	if err != nil {
		t.Fatalf("Load() after remove error: %v", err)
	}
	if _, exists := afterRemove.Plugins["custom"]; exists {
		t.Fatal("expected custom plugin to be removed")
	}
}

func TestRegistryClientFetchCacheForceAndOfflineFallback(t *testing.T) {
	var hits atomic.Int32
	var statusCode atomic.Int32
	statusCode.Store(http.StatusOK)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		code := int(statusCode.Load())
		if code != http.StatusOK {
			w.WriteHeader(code)
			return
		}
		_ = json.NewEncoder(w).Encode(Registry{
			SchemaVersion: 1,
			UpdatedAt:     "2026-11-18T12:00:00Z",
			Core:          ComponentRelease{Version: "0.1.2", Tag: "v0.1.2"},
			Plugins: []RegistryPlugin{
				{ID: "whatsapp", LatestVersion: "0.1.2", MinProtocolVersion: 1, MaxProtocolVersion: 1},
			},
		})
	}))
	defer srv.Close()

	client := NewRegistryClient(srv.URL, srv.URL, 10*time.Minute)
	ctx := context.Background()

	// 1. First fetch hits server.
	reg, reachable, err := client.Fetch(ctx, false)
	if err != nil || !reachable || reg == nil {
		t.Fatalf("first Fetch() = (%v, %v, %v), want reachable=true err=nil", reg, reachable, err)
	}
	if hits.Load() != 1 {
		t.Fatalf("expected 1 HTTP hit, got %d", hits.Load())
	}

	// 2. Second fetch within TTL uses cache.
	reg2, reachable, err := client.Fetch(ctx, false)
	if err != nil || !reachable || reg2 == nil {
		t.Fatalf("cached Fetch() = (%v, %v, %v)", reg2, reachable, err)
	}
	if hits.Load() != 1 {
		t.Fatalf("expected cache hit (1 total HTTP hit), got %d", hits.Load())
	}

	// 3. force=true bypasses TTL cache.
	_, reachable, err = client.Fetch(ctx, true)
	if err != nil || !reachable {
		t.Fatalf("force Fetch() = (%v, %v)", reachable, err)
	}
	if hits.Load() != 2 {
		t.Fatalf("expected 2 HTTP hits after force=true, got %d", hits.Load())
	}

	// 4. HTTP 500 with force=true returns reachable=false and falls back to cached registry without panicking.
	statusCode.Store(http.StatusInternalServerError)
	fallback, reachable, _ := client.Fetch(ctx, true)
	if reachable {
		t.Fatal("expected reachable=false on HTTP 500")
	}
	if fallback == nil || len(fallback.Plugins) == 0 {
		t.Fatalf("expected non-empty fallback registry on HTTP 500, got %+v", fallback)
	}

	// 5. Fresh client on HTTP 500 falls back to embedded Catalog without panicking.
	freshClient := NewRegistryClient(srv.URL, srv.URL, 10*time.Minute)
	catalogFallback, reachable, _ := freshClient.Fetch(ctx, false)
	if reachable {
		t.Fatal("expected reachable=false for fresh client on HTTP 500")
	}
	if catalogFallback == nil || len(catalogFallback.Plugins) != len(Catalog) {
		t.Fatalf("expected fallback from embedded Catalog (%d entries), got %+v", len(Catalog), catalogFallback)
	}

	// 6. Timeout / canceled context returns reachable=false without panicking.
	slowSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(500 * time.Millisecond):
		}
	}))
	defer slowSrv.Close()

	timeoutClient := NewRegistryClient(slowSrv.URL, slowSrv.URL, time.Minute)
	shortCtx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	timeoutFallback, reachable, _ := timeoutClient.Fetch(shortCtx, false)
	if reachable {
		t.Fatal("expected reachable=false on timeout")
	}
	if timeoutFallback == nil || len(timeoutFallback.Plugins) != len(Catalog) {
		t.Fatalf("expected catalog fallback on timeout, got %+v", timeoutFallback)
	}
}

func TestRegistryClientFetchLatestRepoVersionAndEnvOverrides(t *testing.T) {
	var repoHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/registry.json":
			_ = json.NewEncoder(w).Encode(Registry{SchemaVersion: 1, Core: ComponentRelease{Version: "0.1.3"}})
		case "/repos/alice/conduit-plugin-custom/releases/latest":
			repoHits.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"tag_name": "v0.3.1",
				"html_url": "https://github.com/alice/conduit-plugin-custom/releases/tag/v0.3.1",
			})
		case "/repos/alice/conduit-plugin-missing/releases/latest":
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	t.Setenv("CONDUIT_REGISTRY_URL", srv.URL+"/registry.json")
	t.Setenv("CONDUIT_GITHUB_API", srv.URL)

	client := NewRegistryClient("", "", time.Minute)
	reg, reachable, err := client.Fetch(context.Background(), false)
	if err != nil || !reachable || reg.Core.Version != "0.1.3" {
		t.Fatalf("env-overridden Fetch() = (%+v, %v, %v)", reg, reachable, err)
	}

	ver, relURL, err := client.FetchLatestRepoVersion(context.Background(), "alice/conduit-plugin-custom")
	if err != nil {
		t.Fatalf("FetchLatestRepoVersion() error: %v", err)
	}
	if ver != "0.3.1" {
		t.Fatalf("expected version 0.3.1, got %q", ver)
	}
	if relURL != "https://github.com/alice/conduit-plugin-custom/releases/tag/v0.3.1" {
		t.Fatalf("unexpected releaseURL %q", relURL)
	}

	// Cached within TTL.
	ver2, _, err := client.FetchLatestRepoVersion(context.Background(), "alice/conduit-plugin-custom")
	if err != nil || ver2 != "0.3.1" {
		t.Fatalf("cached FetchLatestRepoVersion() = (%q, %v)", ver2, err)
	}
	if repoHits.Load() != 1 {
		t.Fatalf("expected 1 repo API hit within TTL, got %d", repoHits.Load())
	}

	if _, _, err := client.FetchLatestRepoVersion(context.Background(), "alice/conduit-plugin-missing"); err == nil {
		t.Fatal("expected error for 404 repo release")
	}
	if _, _, err := client.FetchLatestRepoVersion(context.Background(), "invalid-repo"); err == nil {
		t.Fatal("expected error for invalid repo format")
	}
}

func TestSeededRegistryJSONValid(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "registry.json"))
	if err != nil {
		t.Fatalf("reading repo root registry.json: %v", err)
	}
	var reg Registry
	if err := json.Unmarshal(data, &reg); err != nil {
		t.Fatalf("unmarshaling registry.json: %v", err)
	}
	if reg.SchemaVersion != 1 {
		t.Fatalf("expected schema_version 1, got %d", reg.SchemaVersion)
	}
	if reg.Core.Version != "0.1.2" || reg.GUI.Version != "0.1.2" {
		t.Fatalf("unexpected core/gui versions: core=%+v gui=%+v", reg.Core, reg.GUI)
	}
	if len(reg.Plugins) != 2 {
		t.Fatalf("expected 2 seeded plugins, got %d", len(reg.Plugins))
	}
	for _, p := range reg.Plugins {
		if ok, reason := CheckCompatibility(p, reg.Core.Version, pluginsdk.ProtocolVersion, nil); !ok {
			t.Fatalf("seeded plugin %s failed compatibility check: %s", p.ID, reason)
		}
	}
}
