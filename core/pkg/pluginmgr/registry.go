package pluginmgr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
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
	// DefaultGitHubWebBase is the GitHub web root used for redirect fallback on API rate limits.
	DefaultGitHubWebBase = "https://github.com"
	// DefaultAppRepository is the GitHub repository hosting Conduit Core and GUI releases.
	DefaultAppRepository = "cestila/ConduitOmniChat"

	// DefaultRegistryTTL is the default in-memory cache TTL for registry and repo lookups.
	DefaultRegistryTTL = 15 * time.Minute
	// DefaultRegistryHTTPTimeout bounds remote registry and GitHub API requests.
	DefaultRegistryHTTPTimeout = 5 * time.Second

	// SourceOfficial identifies plugins from the official registry/catalog.
	SourceOfficial = "official"
	// SourceThirdParty identifies plugins installed from a custom owner/repo.
	SourceThirdParty = "third-party"

	maxRegistryBytes = 4 << 20
	maxMetadataBytes = 64 << 10
)

var metadataAssetNames = map[string]bool{
	"plugin-metadata.json":   true,
	"registry-metadata.json": true,
	"metadata.json":          true,
}

// RepoRelease describes the latest release and optional compatibility metadata
// resolved directly from a repository's GitHub Releases.
type RepoRelease struct {
	Version            string
	Tag                string
	ReleaseURL         string
	MinCoreVersion     string
	MinProtocolVersion uint32
	MaxProtocolVersion uint32
	Dependencies       []PluginDependency
	HasMetadata        bool
}

type pluginMetadata struct {
	MinCoreVersion     string             `json:"min_core_version,omitempty"`
	MinProtocolVersion uint32             `json:"min_protocol_version,omitempty"`
	MaxProtocolVersion uint32             `json:"max_protocol_version,omitempty"`
	Dependencies       []PluginDependency `json:"dependencies,omitempty"`
}

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
	release   RepoRelease
	etag      string
	fetchedAt time.Time
}

// RegistryClient resolves the remote catalog and live GitHub release versions
// per repository with in-memory TTL/ETag caching and offline fallback.
type RegistryClient struct {
	url           string
	githubAPIBase string
	githubWebBase string
	appRepository string
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

	githubWebBase := ""
	if githubAPIBase == DefaultGitHubAPIBase {
		githubWebBase = DefaultGitHubWebBase
	}

	if ttl <= 0 {
		ttl = DefaultRegistryTTL
	}

	return &RegistryClient{
		url:           url,
		githubAPIBase: githubAPIBase,
		githubWebBase: githubWebBase,
		appRepository: DefaultAppRepository,
		ttl:           ttl,
		http:          &http.Client{Timeout: DefaultRegistryHTTPTimeout},
		now:           time.Now,
		repoCache:     map[string]repoCacheEntry{},
	}
}

// SetGitHubWebBase overrides the GitHub web base URL used when API rate limits trigger fallback.
func (c *RegistryClient) SetGitHubWebBase(base string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.githubWebBase = strings.TrimRight(strings.TrimSpace(base), "/")
}

func (c *RegistryClient) httpsRequired() bool {
	return c.githubAPIBase == DefaultGitHubAPIBase
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

// Fetch retrieves the remote catalog and resolves live versions directly from each repository's
// GitHub Releases (with TTL caching unless force is true). When both catalog and live repository
// checks fail, Fetch returns a non-nil fallback *Registry, reachable=false, and a diagnostic error.
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

	reg, catalogErr := c.fetchRemote(ctx)
	catalogReachable := catalogErr == nil
	if !catalogReachable {
		reg = cloneRegistry(fallback)
	}

	reposReachable := c.enrichFromRepos(ctx, reg, force)
	if !catalogReachable && !reposReachable {
		return fallback, false, catalogErr
	}

	c.mu.Lock()
	c.cached = cloneRegistry(reg)
	c.fetchedAt = c.now()
	c.mu.Unlock()

	return cloneRegistry(reg), true, nil
}

func (c *RegistryClient) enrichFromRepos(ctx context.Context, reg *Registry, force bool) bool {
	if reg == nil || ctx.Err() != nil {
		return false
	}
	var (
		wg           sync.WaitGroup
		sem          = make(chan struct{}, 4)
		anyReachable atomic.Bool
		coreRel      RepoRelease
		hasCoreRel   bool
	)

	if c.appRepository != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			rel, err := c.FetchLatestRepoRelease(ctx, c.appRepository, force)
			if err == nil && strings.HasPrefix(rel.Tag, "v") {
				coreRel = rel
				hasCoreRel = true
				anyReachable.Store(true)
			}
		}()
	}

	for i := range reg.Plugins {
		repo := strings.TrimSpace(reg.Plugins[i].Repository)
		if repo == "" {
			continue
		}
		wg.Add(1)
		go func(idx int, repoSlug string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			rel, err := c.FetchLatestRepoRelease(ctx, repoSlug, force)
			if err != nil {
				return
			}
			mergeRepoRelease(&reg.Plugins[idx], rel)
			anyReachable.Store(true)
		}(i, repo)
	}

	wg.Wait()

	if hasCoreRel {
		comp := ComponentRelease{
			Version:    coreRel.Version,
			Tag:        coreRel.Tag,
			ReleaseURL: coreRel.ReleaseURL,
		}
		reg.Core = comp
		reg.GUI = comp
	}
	return anyReachable.Load()
}

func mergeRepoRelease(p *RegistryPlugin, rel RepoRelease) {
	if rel.Version != "" {
		p.LatestVersion = rel.Version
	}
	if rel.ReleaseURL != "" {
		p.ReleaseURL = rel.ReleaseURL
	}
	if !rel.HasMetadata {
		return
	}
	if rel.MinProtocolVersion > 0 {
		p.MinProtocolVersion = rel.MinProtocolVersion
	}
	if rel.MaxProtocolVersion > 0 {
		p.MaxProtocolVersion = rel.MaxProtocolVersion
	}
	if rel.MinCoreVersion != "" {
		p.MinCoreVersion = rel.MinCoreVersion
	}
	if rel.Dependencies != nil {
		p.Dependencies = cloneDependencies(rel.Dependencies)
	}
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
	rel, err := c.FetchLatestRepoRelease(ctx, repo, false)
	if err != nil {
		return "", "", err
	}
	return rel.Version, rel.ReleaseURL, nil
}

// FetchLatestRepoRelease resolves the latest GitHub Release and optional compatibility metadata
// for owner/repo using TTL caching, ETag revalidation, and web redirect fallback on rate limits.
func (c *RegistryClient) FetchLatestRepoRelease(ctx context.Context, repo string, force bool) (RepoRelease, error) {
	repo = strings.TrimSpace(repo)
	if err := validateRepoSlug(repo); err != nil {
		return RepoRelease{}, err
	}

	c.mu.Lock()
	cached, hasCached := c.repoCache[repo]
	webBase := c.githubWebBase
	if hasCached && !force && c.now().Sub(cached.fetchedAt) < c.ttl {
		out := cloneRepoRelease(cached.release)
		c.mu.Unlock()
		return out, nil
	}
	c.mu.Unlock()

	cachedETag := ""
	if hasCached {
		cachedETag = cached.etag
	}

	rel, etag, notModified, status, err := c.fetchRepoReleaseAPI(ctx, repo, cachedETag)
	if notModified && hasCached {
		c.mu.Lock()
		cached.fetchedAt = c.now()
		c.repoCache[repo] = cached
		out := cloneRepoRelease(cached.release)
		c.mu.Unlock()
		return out, nil
	}
	if err != nil && (status == http.StatusForbidden || status == http.StatusTooManyRequests) && webBase != "" {
		webRel, webErr := c.fetchRepoReleaseWeb(ctx, webBase, repo)
		if webErr == nil {
			rel = webRel
			etag = ""
			err = nil
		}
	}
	if err != nil {
		return RepoRelease{}, err
	}

	c.mu.Lock()
	c.repoCache[repo] = repoCacheEntry{
		release:   cloneRepoRelease(rel),
		etag:      etag,
		fetchedAt: c.now(),
	}
	c.mu.Unlock()

	return cloneRepoRelease(rel), nil
}

func validateRepoSlug(repo string) error {
	parts := strings.Split(repo, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" || parts[0] == ".." || parts[1] == ".." || strings.ContainsAny(repo, "?#") {
		return fmt.Errorf("invalid repository %q", repo)
	}
	return nil
}

func (c *RegistryClient) fetchRepoReleaseAPI(ctx context.Context, repo, cachedETag string) (rel RepoRelease, etag string, notModified bool, status int, err error) {
	endpoint := c.githubAPIBase + "/repos/" + repo + "/releases/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return RepoRelease{}, "", false, 0, fmt.Errorf("building release request for %s: %w", repo, err)
	}
	req.Header.Set("User-Agent", "conduitd-registry-client")
	req.Header.Set("Accept", "application/vnd.github+json")
	if cachedETag != "" {
		req.Header.Set("If-None-Match", cachedETag)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return RepoRelease{}, "", false, 0, fmt.Errorf("fetching latest release for %s: %w", repo, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		return RepoRelease{}, cachedETag, true, resp.StatusCode, nil
	}
	if resp.StatusCode != http.StatusOK {
		return RepoRelease{}, "", false, resp.StatusCode, fmt.Errorf("github releases API for %s returned HTTP %d", repo, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxRegistryBytes+1))
	if err != nil {
		return RepoRelease{}, "", false, resp.StatusCode, fmt.Errorf("reading release response for %s: %w", repo, err)
	}
	if len(body) > maxRegistryBytes {
		return RepoRelease{}, "", false, resp.StatusCode, fmt.Errorf("release response for %s exceeds %d bytes", repo, maxRegistryBytes)
	}

	var payload struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
		Assets  []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return RepoRelease{}, "", false, resp.StatusCode, fmt.Errorf("decoding release response for %s: %w", repo, err)
	}
	tag := strings.TrimSpace(payload.TagName)
	if tag == "" {
		return RepoRelease{}, "", false, resp.StatusCode, fmt.Errorf("latest release for %s has empty tag_name", repo)
	}

	releaseURL := strings.TrimSpace(payload.HTMLURL)
	if releaseURL == "" {
		releaseURL = fmt.Sprintf("https://github.com/%s/releases/tag/%s", repo, tag)
	}
	rel = RepoRelease{
		Version:    strings.TrimPrefix(tag, "v"),
		Tag:        tag,
		ReleaseURL: releaseURL,
	}
	for _, asset := range payload.Assets {
		if metadataAssetNames[strings.ToLower(strings.TrimSpace(asset.Name))] {
			if meta, ok := c.fetchMetadataAsset(ctx, asset.BrowserDownloadURL); ok {
				applyMetadata(&rel, meta)
			}
			break
		}
	}
	return rel, strings.TrimSpace(resp.Header.Get("ETag")), false, resp.StatusCode, nil
}

func (c *RegistryClient) fetchRepoReleaseWeb(ctx context.Context, webBase, repo string) (RepoRelease, error) {
	endpoint := webBase + "/" + repo + "/releases/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, endpoint, nil)
	if err != nil {
		return RepoRelease{}, fmt.Errorf("building web release request for %s: %w", repo, err)
	}
	req.Header.Set("User-Agent", "conduitd-registry-client")

	noRedirect := *c.http
	noRedirect.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	resp, err := noRedirect.Do(req)
	if err != nil {
		return RepoRelease{}, fmt.Errorf("fetching web release for %s: %w", repo, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 300 || resp.StatusCode >= 400 {
		return RepoRelease{}, fmt.Errorf("web releases endpoint for %s returned HTTP %d", repo, resp.StatusCode)
	}
	tag := extractTagFromLocation(resp.Header.Get("Location"))
	if tag == "" {
		return RepoRelease{}, fmt.Errorf("web releases redirect for %s missing tag in Location", repo)
	}

	rel := RepoRelease{
		Version:    strings.TrimPrefix(tag, "v"),
		Tag:        tag,
		ReleaseURL: fmt.Sprintf("%s/%s/releases/tag/%s", webBase, repo, tag),
	}
	metaURL := fmt.Sprintf("%s/%s/releases/download/%s/plugin-metadata.json", webBase, repo, tag)
	if meta, ok := c.fetchMetadataAsset(ctx, metaURL); ok {
		applyMetadata(&rel, meta)
	}
	return rel, nil
}

func extractTagFromLocation(loc string) string {
	u, err := url.Parse(strings.TrimSpace(loc))
	if err != nil {
		return ""
	}
	const marker = "/releases/tag/"
	idx := strings.Index(u.Path, marker)
	if idx < 0 {
		return ""
	}
	tag := strings.Trim(u.Path[idx+len(marker):], "/")
	if tag == "" || strings.Contains(tag, "/") {
		return ""
	}
	return tag
}

func (c *RegistryClient) fetchMetadataAsset(ctx context.Context, rawURL string) (pluginMetadata, bool) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Host == "" {
		return pluginMetadata{}, false
	}
	if u.Scheme != "https" && (c.httpsRequired() || u.Scheme != "http") {
		return pluginMetadata{}, false
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return pluginMetadata{}, false
	}
	req.Header.Set("User-Agent", "conduitd-registry-client")
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil {
			resp.Body.Close()
		}
		return pluginMetadata{}, false
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxMetadataBytes+1))
	if err != nil || len(body) > maxMetadataBytes {
		return pluginMetadata{}, false
	}

	var meta pluginMetadata
	if err := json.Unmarshal(body, &meta); err != nil {
		return pluginMetadata{}, false
	}
	return meta, true
}

func applyMetadata(rel *RepoRelease, meta pluginMetadata) {
	rel.MinCoreVersion = strings.TrimSpace(meta.MinCoreVersion)
	rel.MinProtocolVersion = meta.MinProtocolVersion
	rel.MaxProtocolVersion = meta.MaxProtocolVersion
	rel.Dependencies = cloneDependencies(meta.Dependencies)
	if meta.Dependencies != nil && rel.Dependencies == nil {
		rel.Dependencies = []PluginDependency{}
	}
	rel.HasMetadata = true
}

// RefreshThirdPartyManifest refreshes latest release versions and optional compatibility
// metadata for installed third-party plugins in local and persists changes via store.
func (c *RegistryClient) RefreshThirdPartyManifest(ctx context.Context, store *ManifestStore, local *LocalManifest, force bool) bool {
	if c == nil || local == nil || store == nil {
		return false
	}
	changed := false
	anyReachable := false
	now := c.now().UTC().Format(time.RFC3339)
	for id, rec := range local.Plugins {
		if rec.Source != SourceThirdParty || rec.Repository == "" {
			continue
		}
		rel, err := c.FetchLatestRepoRelease(ctx, rec.Repository, force)
		if err != nil || rel.Version == "" {
			continue
		}
		anyReachable = true
		rec.LatestVersion = rel.Version
		if rel.ReleaseURL != "" {
			rec.ReleaseURL = rel.ReleaseURL
		}
		if rel.HasMetadata {
			if rel.MinProtocolVersion > 0 {
				rec.MinProtocolVersion = rel.MinProtocolVersion
			}
			if rel.MaxProtocolVersion > 0 {
				rec.MaxProtocolVersion = rel.MaxProtocolVersion
			}
			if rel.MinCoreVersion != "" {
				rec.MinCoreVersion = rel.MinCoreVersion
			}
			if rel.Dependencies != nil {
				rec.Dependencies = cloneDependencies(rel.Dependencies)
			}
		}
		rec.LastCheckedAt = now
		local.Plugins[id] = rec
		changed = true
	}
	if changed {
		_ = store.Save(local)
	}
	return anyReachable
}

func cloneDependencies(deps []PluginDependency) []PluginDependency {
	if len(deps) == 0 {
		return nil
	}
	out := make([]PluginDependency, len(deps))
	copy(out, deps)
	return out
}

func cloneRepoRelease(rel RepoRelease) RepoRelease {
	rel.Dependencies = cloneDependencies(rel.Dependencies)
	return rel
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
