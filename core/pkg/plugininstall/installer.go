// Package plugininstall downloads official or third-party plugins from GitHub
// Releases, verifies them against the release's checksums.txt and installs the
// executable into a plugin directory.
//
// Release contract (produced by GoReleaser):
//
//	conduit-plugin-<id>_<version without leading v>_<goos>_<goarch>.tar.gz (.zip on windows)
//	  containing the executable conduit-plugin-<id> (.exe on windows) at the archive root
//	checksums.txt with "<sha256hex>  <filename>" lines
package plugininstall

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/cestila/ConduitOmniChat/core/pkg/pluginmgr"
	"github.com/cestila/ConduitOmniChat/sdk/pluginsdk"
)

const (
	// DefaultAPIBase is the GitHub REST API root.
	DefaultAPIBase = "https://api.github.com"
	// APIEnv overrides the API root (used by tests).
	APIEnv = "CONDUIT_GITHUB_API"

	userAgent     = "conduitd-plugin-installer"
	checksumsName = "checksums.txt"

	maxAPIBytes       = 4 << 20
	maxChecksumsBytes = 1 << 20
	maxArchiveBytes   = 256 << 20
	maxBinaryBytes    = 256 << 20
)

var (
	ownerPattern   = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,38})$`)
	repoPattern    = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}$`)
	versionPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,63}$`)
	sha256Pattern  = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)
)

// Installer installs plugins from GitHub Releases into PluginDir.
type Installer struct {
	APIBase   string
	HTTP      *http.Client
	PluginDir string
	GOOS      string
	GOARCH    string
}

// Result describes a completed installation.
type Result struct {
	ID      string
	Version string
	Path    string
}

// New returns an Installer with production defaults installing into pluginDir.
func New(pluginDir string) *Installer {
	base := DefaultAPIBase
	if v := strings.TrimRight(strings.TrimSpace(os.Getenv(APIEnv)), "/"); v != "" {
		base = v
	}
	in := &Installer{
		APIBase:   base,
		PluginDir: pluginDir,
		GOOS:      runtime.GOOS,
		GOARCH:    runtime.GOARCH,
	}
	in.HTTP = &http.Client{
		Timeout: 5 * time.Minute,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("too many redirects")
			}
			return in.checkURL(req.URL.String())
		},
	}
	return in
}

// httpsRequired is true unless the API base was overridden (tests).
func (in *Installer) httpsRequired() bool {
	return strings.TrimRight(in.APIBase, "/") == DefaultAPIBase
}

func (in *Installer) checkURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return fmt.Errorf("invalid download URL %q", raw)
	}
	if u.Scheme != "https" && (in.httpsRequired() || u.Scheme != "http") {
		return fmt.Errorf("refusing non-https download URL %q", raw)
	}
	return nil
}

const specHelp = "accepted forms: an official plugin id (e.g. telegram) or owner/repo where repo is conduit-plugin-<id>"

// Resolve maps a spec (official id or owner/repo) to a plugin id and GitHub repo.
func (in *Installer) Resolve(spec string) (id, repo string, err error) {
	if !strings.Contains(spec, "/") {
		if e, ok := pluginmgr.CatalogEntryFor(spec); ok {
			return e.ID, e.Repository, nil
		}
		return "", "", fmt.Errorf("unknown plugin %q: %s", spec, specHelp)
	}
	parts := strings.Split(spec, "/")
	if len(parts) != 2 || !ownerPattern.MatchString(parts[0]) || !repoPattern.MatchString(parts[1]) ||
		!strings.HasPrefix(parts[1], pluginmgr.BinaryPrefix) {
		return "", "", fmt.Errorf("invalid plugin spec %q: %s", spec, specHelp)
	}
	id = strings.TrimPrefix(parts[1], pluginmgr.BinaryPrefix)
	if !pluginsdk.ValidID(id) {
		return "", "", fmt.Errorf("invalid plugin spec %q: %q is not a valid plugin id; %s", spec, id, specHelp)
	}
	return id, spec, nil
}

type release struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

func (in *Installer) binaryName(id string) string {
	name := pluginmgr.BinaryPrefix + id
	if in.GOOS == "windows" {
		name += ".exe"
	}
	return name
}

// Install downloads and installs the plugin spec at version ("" = latest).
func (in *Installer) Install(ctx context.Context, spec, version string) (Result, error) {
	id, repo, err := in.Resolve(spec)
	if err != nil {
		return Result{}, err
	}
	endpoint := "/repos/" + repo + "/releases/latest"
	if version != "" {
		if !versionPattern.MatchString(version) {
			return Result{}, fmt.Errorf("invalid version %q", version)
		}
		endpoint = "/repos/" + repo + "/releases/tags/" + version
	}

	var rel release
	var body strings.Builder
	if err := in.fetch(ctx, strings.TrimRight(in.APIBase, "/")+endpoint, maxAPIBytes, &body); err != nil {
		return Result{}, fmt.Errorf("fetching release of %s: %w", repo, err)
	}
	if err := json.Unmarshal([]byte(body.String()), &rel); err != nil {
		return Result{}, fmt.Errorf("decoding release of %s: %w", repo, err)
	}
	tag := rel.TagName
	if tag == "" {
		tag = version
	}
	if !versionPattern.MatchString(tag) {
		return Result{}, fmt.Errorf("release of %s has an invalid tag %q", repo, tag)
	}

	ext := ".tar.gz"
	if in.GOOS == "windows" {
		ext = ".zip"
	}
	assetName := fmt.Sprintf("%s%s_%s_%s_%s%s", pluginmgr.BinaryPrefix, id, strings.TrimPrefix(tag, "v"), in.GOOS, in.GOARCH, ext)
	var assetURL, sumsURL string
	for _, a := range rel.Assets {
		switch a.Name {
		case assetName:
			assetURL = a.URL
		case checksumsName:
			sumsURL = a.URL
		}
	}
	if assetURL == "" {
		return Result{}, fmt.Errorf("release %s of %s has no asset for %s/%s (expected %s)", tag, repo, in.GOOS, in.GOARCH, assetName)
	}
	if sumsURL == "" {
		return Result{}, fmt.Errorf("release %s of %s has no %s", tag, repo, checksumsName)
	}

	var sums strings.Builder
	if err := in.fetch(ctx, sumsURL, maxChecksumsBytes, &sums); err != nil {
		return Result{}, fmt.Errorf("downloading %s: %w", checksumsName, err)
	}
	want, err := checksumFor(sums.String(), assetName)
	if err != nil {
		return Result{}, err
	}

	if err := os.MkdirAll(in.PluginDir, 0o755); err != nil {
		return Result{}, fmt.Errorf("creating plugin directory: %w", err)
	}
	archive, err := os.CreateTemp(in.PluginDir, ".download-*")
	if err != nil {
		return Result{}, fmt.Errorf("creating temp file: %w", err)
	}
	defer func() { archive.Close(); os.Remove(archive.Name()) }()

	hasher := sha256.New()
	if err := in.fetch(ctx, assetURL, maxArchiveBytes, io.MultiWriter(archive, hasher)); err != nil {
		return Result{}, fmt.Errorf("downloading %s: %w", assetName, err)
	}
	if got := hex.EncodeToString(hasher.Sum(nil)); got != want {
		return Result{}, fmt.Errorf("checksum mismatch for %s: expected %s, got %s", assetName, want, got)
	}

	bin, err := os.CreateTemp(in.PluginDir, ".extract-*")
	if err != nil {
		return Result{}, fmt.Errorf("creating temp file: %w", err)
	}
	defer func() { bin.Close(); os.Remove(bin.Name()) }()
	binName := in.binaryName(id)
	if in.GOOS == "windows" {
		err = extractZip(archive, binName, bin)
	} else {
		err = extractTarGz(archive, binName, bin)
	}
	if err != nil {
		return Result{}, fmt.Errorf("extracting %s: %w", assetName, err)
	}
	if err := bin.Chmod(0o755); err != nil {
		return Result{}, err
	}
	if err := bin.Close(); err != nil {
		return Result{}, err
	}
	dest := filepath.Join(in.PluginDir, binName)
	if err := os.Rename(bin.Name(), dest); err != nil {
		return Result{}, fmt.Errorf("installing %s: %w", dest, err)
	}
	return Result{ID: id, Version: tag, Path: dest}, nil
}

// Remove deletes the installed plugin binary from PluginDir.
func (in *Installer) Remove(id string) (string, error) {
	if !pluginsdk.ValidID(id) {
		return "", fmt.Errorf("invalid plugin id %q", id)
	}
	p := filepath.Join(in.PluginDir, in.binaryName(id))
	info, err := os.Lstat(p)
	if err != nil || !info.Mode().IsRegular() {
		return "", fmt.Errorf("plugin %q is not installed in %s", id, in.PluginDir)
	}
	if err := os.Remove(p); err != nil {
		return "", err
	}
	return p, nil
}

// fetch GETs rawURL into w, failing if the body exceeds limit bytes.
func (in *Installer) fetch(ctx context.Context, rawURL string, limit int64, w io.Writer) error {
	if err := in.checkURL(rawURL); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/vnd.github+json, application/octet-stream")
	client := in.HTTP
	if client == nil {
		client = New(in.PluginDir).HTTP
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected HTTP status %s", resp.Status)
	}
	n, err := io.Copy(w, io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return err
	}
	if n > limit {
		return fmt.Errorf("response exceeds the %d byte limit", limit)
	}
	return nil
}

func checksumFor(sums, assetName string) (string, error) {
	for _, line := range strings.Split(sums, "\n") {
		f := strings.Fields(line)
		if len(f) != 2 || strings.TrimPrefix(f[1], "*") != assetName {
			continue
		}
		if !sha256Pattern.MatchString(f[0]) {
			return "", fmt.Errorf("malformed checksum for %s in %s", assetName, checksumsName)
		}
		return strings.ToLower(f[0]), nil
	}
	return "", fmt.Errorf("%s has no entry for %s", checksumsName, assetName)
}

// entryName validates an archive entry name and returns its cleaned form.
func entryName(raw string) (string, error) {
	if raw == "" || strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "\\") || strings.Contains(raw, "\\") {
		return "", fmt.Errorf("unsafe archive entry %q", raw)
	}
	for _, part := range strings.Split(raw, "/") {
		if part == ".." {
			return "", fmt.Errorf("unsafe archive entry %q", raw)
		}
	}
	return path.Clean(raw), nil
}

func copyBinary(dst io.Writer, src io.Reader) error {
	n, err := io.Copy(dst, io.LimitReader(src, maxBinaryBytes+1))
	if err != nil {
		return err
	}
	if n > maxBinaryBytes {
		return errors.New("binary exceeds the size limit")
	}
	return nil
}

func extractTarGz(f *os.File, binName string, dst io.Writer) error {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	found := false
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		name, err := entryName(h.Name)
		if err != nil {
			return err
		}
		switch h.Typeflag {
		case tar.TypeSymlink, tar.TypeLink:
			return fmt.Errorf("archive entry %q is a link", h.Name)
		case tar.TypeReg:
			if name != binName {
				continue
			}
			if found {
				return fmt.Errorf("archive contains %s twice", binName)
			}
			if err := copyBinary(dst, tr); err != nil {
				return err
			}
			found = true
		}
	}
	if !found {
		return fmt.Errorf("archive does not contain %s", binName)
	}
	return nil
}

func extractZip(f *os.File, binName string, dst io.Writer) error {
	info, err := f.Stat()
	if err != nil {
		return err
	}
	zr, err := zip.NewReader(f, info.Size())
	if err != nil {
		return err
	}
	found := false
	for _, zf := range zr.File {
		name, err := entryName(zf.Name)
		if err != nil {
			return err
		}
		if zf.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("archive entry %q is a link", zf.Name)
		}
		if !zf.Mode().IsRegular() || name != binName {
			continue
		}
		if found {
			return fmt.Errorf("archive contains %s twice", binName)
		}
		rc, err := zf.Open()
		if err != nil {
			return err
		}
		err = copyBinary(dst, rc)
		rc.Close()
		if err != nil {
			return err
		}
		found = true
	}
	if !found {
		return fmt.Errorf("archive does not contain %s", binName)
	}
	return nil
}
