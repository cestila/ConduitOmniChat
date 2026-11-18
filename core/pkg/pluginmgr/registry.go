package pluginmgr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cestila/ConduitOmniChat/core/pkg/buildinfo"
	"github.com/cestila/ConduitOmniChat/sdk/pluginsdk"
)

const (
	// DefaultRegistryURL is the raw GitHub URL for the remote plugin registry.
	DefaultRegistryURL = "https://raw.githubusercontent.com/cestila/ConduitOmniChat/main/registry.json"
	// RegistryURLEnv overrides the remote registry URL (used by tests and custom mirrors).
	RegistryURLEnv = "CONDUIT_REGISTRY_URL"

	// DefaultGitHubAPIBase is the GitHub REST API root.
	DefaultGitHubAPIBase = "https://api.github.com"
	// GitHubAPIBaseEnv overrides the GitHub API root (used by tests).
	GitHubAPIBaseEnv = "CONDUIT_GITHUB_API"

	// DefaultRegistryTTL is the default in-memory cache TTL for registry and repo lookups.
	DefaultRegistryTTL = 15 * time.Minute
	// DefaultRegistryHTTPTimeout bounds remote registry and GitHub API requests.
	DefaultRegistryHTTPTimeout = 5 * time.Second

	// SourceOfficial identifies plugins from the official registry/catalog.
	SourceOfficial = "official"
	// SourceThirdParty identifies plugins installed from a custom owner/repo.
	SourceThirdParty = "third-party"

	maxRegistryBytes = 4 << 20
)

// PluginDependency declares a required plugin and its minimum version.
type PluginDependency struct {
	PluginID   string `json:"plugin_id"`
	MinVersion string `json:"min_version,omitempty"`
}

// ComponentRelease describes the latest release of Conduit Core or GUI.
type ComponentRelease struct {
	Version    string `json:"version"`
	Tag        string `json:"tag,omitempty"`
	ReleaseURL string `json:"release_url,omitempty"`
}

// RegistryPlugin describes a plugin entry in the remote registry.
type RegistryPlugin struct {
	ID                 string             `json:"id"`
	Name               string             `json:"name"`
	Description        string             `json:"description,omitempty"`
	Repository         string             `json:"repository"`
	LatestVersion      string             `json:"latest_version,omitempty"`
	ReleaseURL         string             `json:"release_url,omitempty"`
	MinCoreVersion     string             `json:"min_core_version,omitempty"`
	MinProtocolVersion uint32             `json:"min_protocol_version,omitempty"`
	MaxProtocolVersion uint32             `json:"max_protocol_version,omitempty"`
	Dependencies       []PluginDependency `json:"dependencies,omitempty"`
}

// Registry represents the remote registry.json document.
type Registry struct {
	SchemaVersion int              `json:"schema_version"`
	UpdatedAt     string           `json:"updated_at,omitempty"`
	Core          ComponentRelease `json:"core,omitempty"`
	GUI           ComponentRelease `json:"gui,omitempty"`
	Plugins       []RegistryPlugin `json:"plugins"`
}

// LocalPluginRecord persists installed/known state for a plugin in plugins-manifest.json.
type LocalPluginRecord struct {
	ID                 string             `json:"id"`
	Repository         string             `json:"repository"`
	Source             string             `json:"source"`
	InstalledVersion   string             `json:"installed_version,omitempty"`
	LatestVersion      string             `json:"latest_version,omitempty"`
	MinCoreVersion     string             `json:"min_core_version,omitempty"`
	ReleaseURL         string             `json:"release_url,omitempty"`
	LastCheckedAt      string             `json:"last_checked_at,omitempty"`
	MinProtocolVersion uint32             `json:"min_protocol_version,omitempty"`
	MaxProtocolVersion uint32             `json:"max_protocol_version,omitempty"`
	Dependencies       []PluginDependency `json:"dependencies,omitempty"`
}

// LocalManifest is the root schema persisted in <dataDir>/plugins-manifest.json.
type LocalManifest struct {
	SchemaVersion int                          `json:"schema_version"`
	UpdatedAt     string                       `json:"updated_at,omitempty"`
	Core          ComponentRelease             `json:"core,omitempty"`
	GUI           ComponentRelease             `json:"gui,omitempty"`
	Plugins       map[string]LocalPluginRecord `json:"plugins"`
}

// ManifestStore manages thread-safe atomic persistence of plugins-manifest.json.
type ManifestStore struct {
	path string
	mu   sync.Mutex
}

// NewManifestStore creates a ManifestStore backed by path.
func NewManifestStore(path string) *ManifestStore {
	return &ManifestStore{path: path}
}

// Load reads and decodes the local manifest, returning an empty initialized manifest
// when the file does not yet exist.
func (s *ManifestStore) Load() (*LocalManifest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked()
}

func (s *ManifestStore) loadLocked() (*LocalManifest, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &LocalManifest{
				SchemaVersion: 1,
				Plugins:       map[string]LocalPluginRecord{},
			}, nil
		}
		return nil, fmt.Errorf("reading manifest %s: %w", s.path, err)
	}

	var m LocalManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("decoding manifest %s: %w", s.path, err)
	}
	if m.SchemaVersion == 0 {
		m.SchemaVersion = 1
	}
	if m.Plugins == nil {
		m.Plugins = map[string]LocalPluginRecord{}
	}
	for id, rec := range m.Plugins {
		if rec.ID == "" {
			rec.ID = id
			m.Plugins[id] = rec
		}
	}
	return &m, nil
}

// Save atomically persists the local manifest to disk.
func (s *ManifestStore) Save(m *LocalManifest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked(m)
}

func (s *ManifestStore) saveLocked(m *LocalManifest) error {
	if m == nil {
		return errors.New("manifest cannot be nil")
	}
	if m.SchemaVersion == 0 {
		m.SchemaVersion = 1
	}
	if m.Plugins == nil {
		m.Plugins = map[string]LocalPluginRecord{}
	}

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating manifest dir %s: %w", dir, err)
	}

	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding manifest: %w", err)
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(dir, ".plugins-manifest-*.tmp")
	if err != nil {
		return fmt.Errorf("creating temp manifest: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}

	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return fmt.Errorf("writing temp manifest: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("syncing temp manifest: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("closing temp manifest: %w", err)
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("replacing manifest %s: %w", s.path, err)
	}
	return nil
}

// UpsertThirdParty records or updates a third-party plugin entry in the local manifest.
func (s *ManifestStore) UpsertThirdParty(id, repo, version string) error {
	id = strings.TrimSpace(id)
	repo = strings.TrimSpace(repo)
	if id == "" {
		return errors.New("plugin id is required")
	}
	if repo == "" {
		return errors.New("plugin repository is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	m, err := s.loadLocked()
	if err != nil {
		return err
	}

	rec := m.Plugins[id]
	rec.ID = id
	rec.Repository = repo
	rec.Source = SourceThirdParty
	if version != "" {
		rec.InstalledVersion = strings.TrimPrefix(strings.TrimSpace(version), "v")
	}
	m.Plugins[id] = rec
	m.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	return s.saveLocked(m)
}

// Remove deletes a plugin record from the local manifest.
func (s *ManifestStore) Remove(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	m, err := s.loadLocked()
	if err != nil {
		return err
	}
	if _, exists := m.Plugins[id]; !exists {
		return nil
	}
	delete(m.Plugins, id)
	m.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	return s.saveLocked(m)
}

// MergeRemote updates official catalog entries and component releases from reg without
// overwriting third-party records, losing installed versions on official plugins, or
// clobbering cached LatestVersion/MinCoreVersion/ReleaseURL metadata with empty fallback
// values (e.g. from CatalogRegistry()). Callers should normally invoke MergeRemote only
// when RegistryClient.Fetch reports reachable == true.
func (s *ManifestStore) MergeRemote(reg *Registry) error {
	if reg == nil {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	m, err := s.loadLocked()
	if err != nil {
		return err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	if reg.Core.Version != "" || reg.Core.Tag != "" || reg.Core.ReleaseURL != "" {
		m.Core = reg.Core
	}
	if reg.GUI.Version != "" || reg.GUI.Tag != "" || reg.GUI.ReleaseURL != "" {
		m.GUI = reg.GUI
	}
	if reg.UpdatedAt != "" {
		m.UpdatedAt = reg.UpdatedAt
	} else if m.UpdatedAt == "" {
		m.UpdatedAt = now
	}

	for _, p := range reg.Plugins {
		if p.ID == "" {
			continue
		}
		existing, exists := m.Plugins[p.ID]
		if exists && existing.Source == SourceThirdParty {
			continue
		}
		latestVersion := p.LatestVersion
		if latestVersion == "" {
			latestVersion = existing.LatestVersion
		}
		minCoreVersion := p.MinCoreVersion
		if minCoreVersion == "" {
			minCoreVersion = existing.MinCoreVersion
		}
		releaseURL := p.ReleaseURL
		if (p.LatestVersion == "" && existing.LatestVersion != "" && existing.ReleaseURL != "") || releaseURL == "" {
			releaseURL = existing.ReleaseURL
		}
		deps := cloneDependencies(p.Dependencies)
		if p.LatestVersion == "" && len(deps) == 0 && len(existing.Dependencies) > 0 {
			deps = cloneDependencies(existing.Dependencies)
		}
		lastCheckedAt := existing.LastCheckedAt
		if p.LatestVersion != "" || lastCheckedAt == "" {
			lastCheckedAt = now
		}

		rec := LocalPluginRecord{
			ID:                 p.ID,
			Repository:         p.Repository,
			Source:             SourceOfficial,
			InstalledVersion:   existing.InstalledVersion,
			LatestVersion:      latestVersion,
			MinCoreVersion:     minCoreVersion,
			ReleaseURL:         releaseURL,
			LastCheckedAt:      lastCheckedAt,
			MinProtocolVersion: p.MinProtocolVersion,
			MaxProtocolVersion: p.MaxProtocolVersion,
			Dependencies:       deps,
		}
		m.Plugins[p.ID] = rec
	}

	return s.saveLocked(m)
}

type repoCacheEntry struct {
	version    string
	releaseURL string
	fetchedAt  time.Time
}

// RegistryClient fetches the remote registry.json and third-party GitHub release versions
// with in-memory TTL caching and offline fallback.
type RegistryClient struct {
	url           string
	githubAPIBase string
	ttl           time.Duration
	http          *http.Client
	now           func() time.Time

	mu        sync.Mutex
	cached    *Registry
	fetchedAt time.Time
	repoCache map[string]repoCacheEntry
}

// NewRegistryClient constructs a RegistryClient respecting CONDUIT_REGISTRY_URL and
// CONDUIT_GITHUB_API when url or githubAPIBase are empty.
func NewRegistryClient(url, githubAPIBase string, ttl time.Duration) *RegistryClient {
	if strings.TrimSpace(url) == "" {
		url = strings.TrimSpace(os.Getenv(RegistryURLEnv))
	}
	if url == "" {
		url = DefaultRegistryURL
	}

	if strings.TrimSpace(githubAPIBase) == "" {
		githubAPIBase = strings.TrimSpace(os.Getenv(GitHubAPIBaseEnv))
	}
	if githubAPIBase == "" {
		githubAPIBase = DefaultGitHubAPIBase
	}
	githubAPIBase = strings.TrimRight(githubAPIBase, "/")

	if ttl <= 0 {
		ttl = DefaultRegistryTTL
	}

	return &RegistryClient{
		url:           url,
		githubAPIBase: githubAPIBase,
		ttl:           ttl,
		http:          &http.Client{Timeout: DefaultRegistryHTTPTimeout},
		now:           time.Now,
		repoCache:     map[string]repoCacheEntry{},
	}
}

// CatalogRegistry synthesizes a baseline Registry from the embedded Catalog and SDK protocol version.
func CatalogRegistry() *Registry {
	reg := &Registry{
		SchemaVersion: 1,
		Plugins:       make([]RegistryPlugin, 0, len(Catalog)),
	}
	if v := strings.TrimSpace(buildinfo.Version); v != "" && v != "dev" {
		clean := strings.TrimPrefix(v, "v")
		reg.Core = ComponentRelease{Version: clean, Tag: "v" + clean}
	}
	for _, entry := range Catalog {
		reg.Plugins = append(reg.Plugins, RegistryPlugin{
			ID:                 entry.ID,
			Name:               entry.Name,
			Description:        entry.Description,
			Repository:         entry.Repository,
			MinProtocolVersion: pluginsdk.ProtocolVersion,
			MaxProtocolVersion: pluginsdk.ProtocolVersion,
			ReleaseURL:         fmt.Sprintf("https://github.com/%s/releases", entry.Repository),
		})
	}
	return reg
}

// Fetch retrieves the remote registry, serving from TTL cache unless force is true.
// When the remote registry is unreachable or fails, Fetch returns a non-nil fallback
// *Registry (the last cached registry or CatalogRegistry()), reachable=false, and a
// non-nil error describing the transport/HTTP/decode failure so callers can log the
// cause while still falling back gracefully without failing the user operation.
func (c *RegistryClient) Fetch(ctx context.Context, force bool) (*Registry, bool, error) {
	c.mu.Lock()
	if !force && c.cached != nil && c.now().Sub(c.fetchedAt) < c.ttl {
		out := cloneRegistry(c.cached)
		c.mu.Unlock()
		return out, true, nil
	}
	fallback := cloneRegistry(c.cached)
	c.mu.Unlock()

	if fallback == nil {
		fallback = CatalogRegistry()
	}

	reg, err := c.fetchRemote(ctx)
	if err != nil {
		return fallback, false, err
	}

	c.mu.Lock()
	c.cached = cloneRegistry(reg)
	c.fetchedAt = c.now()
	c.mu.Unlock()

	return cloneRegistry(reg), true, nil
}

func (c *RegistryClient) fetchRemote(ctx context.Context) (*Registry, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return nil, fmt.Errorf("building registry request: %w", err)
	}
	req.Header.Set("User-Agent", "conduitd-registry-client")
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching registry %s: %w", c.url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("registry %s returned HTTP %d", c.url, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxRegistryBytes+1))
	if err != nil {
		return nil, fmt.Errorf("reading registry %s: %w", c.url, err)
	}
	if len(body) > maxRegistryBytes {
		return nil, fmt.Errorf("registry %s exceeds %d bytes", c.url, maxRegistryBytes)
	}

	var reg Registry
	if err := json.Unmarshal(body, &reg); err != nil {
		return nil, fmt.Errorf("decoding registry %s: %w", c.url, err)
	}
	if reg.SchemaVersion == 0 {
		reg.SchemaVersion = 1
	}
	return &reg, nil
}

// FetchLatestRepoVersion queries GitHub Releases for the latest release version and URL of owner/repo.
func (c *RegistryClient) FetchLatestRepoVersion(ctx context.Context, repo string) (version, releaseURL string, err error) {
	repo = strings.TrimSpace(repo)
	parts := strings.Split(repo, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" || parts[0] == ".." || parts[1] == ".." || strings.ContainsAny(repo, "?#") {
		return "", "", fmt.Errorf("invalid repository %q", repo)
	}

	c.mu.Lock()
	if cached, ok := c.repoCache[repo]; ok && c.now().Sub(cached.fetchedAt) < c.ttl {
		c.mu.Unlock()
		return cached.version, cached.releaseURL, nil
	}
	c.mu.Unlock()

	endpoint := c.githubAPIBase + "/repos/" + repo + "/releases/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", "", fmt.Errorf("building release request for %s: %w", repo, err)
	}
	req.Header.Set("User-Agent", "conduitd-registry-client")
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("fetching latest release for %s: %w", repo, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("github releases API for %s returned HTTP %d", repo, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxRegistryBytes+1))
	if err != nil {
		return "", "", fmt.Errorf("reading release response for %s: %w", repo, err)
	}
	if len(body) > maxRegistryBytes {
		return "", "", fmt.Errorf("release response for %s exceeds %d bytes", repo, maxRegistryBytes)
	}

	var rel struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
	}
	if err := json.Unmarshal(body, &rel); err != nil {
		return "", "", fmt.Errorf("decoding release response for %s: %w", repo, err)
	}
	tag := strings.TrimSpace(rel.TagName)
	if tag == "" {
		return "", "", fmt.Errorf("latest release for %s has empty tag_name", repo)
	}
	version = strings.TrimPrefix(tag, "v")
	releaseURL = strings.TrimSpace(rel.HTMLURL)
	if releaseURL == "" {
		releaseURL = fmt.Sprintf("https://github.com/%s/releases/tag/%s", repo, tag)
	}

	c.mu.Lock()
	c.repoCache[repo] = repoCacheEntry{
		version:    version,
		releaseURL: releaseURL,
		fetchedAt:  c.now(),
	}
	c.mu.Unlock()

	return version, releaseURL, nil
}

func cloneDependencies(deps []PluginDependency) []PluginDependency {
	if len(deps) == 0 {
		return nil
	}
	out := make([]PluginDependency, len(deps))
	copy(out, deps)
	return out
}

func cloneRegistry(reg *Registry) *Registry {
	if reg == nil {
		return nil
	}
	cp := *reg
	if reg.Plugins != nil {
		cp.Plugins = make([]RegistryPlugin, len(reg.Plugins))
		for i, p := range reg.Plugins {
			p.Dependencies = cloneDependencies(p.Dependencies)
			cp.Plugins[i] = p
		}
	}
	return &cp
}
