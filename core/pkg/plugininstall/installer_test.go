package plugininstall

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type entry struct {
	name     string
	body     string
	typeflag byte
	link     string
}

func makeTarGz(t *testing.T, entries ...entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		flag := e.typeflag
		if flag == 0 {
			flag = tar.TypeReg
		}
		h := &tar.Header{Name: e.name, Mode: 0o644, Typeflag: flag, Linkname: e.link}
		if flag == tar.TypeReg {
			h.Size = int64(len(e.body))
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if flag == tar.TypeReg {
			_, _ = tw.Write([]byte(e.body))
		}
	}
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

type fakeRelease struct {
	t         *testing.T
	repo      string
	tag       string
	archive   []byte
	checksums func(assetName string, archive []byte) string
	assetName string // override; "" = computed for linux/amd64
	noAsset   bool
	paths     []string
	srv       *httptest.Server
}

func (f *fakeRelease) start() *Installer {
	if f.assetName == "" {
		id := strings.TrimPrefix(f.repo[strings.Index(f.repo, "/")+1:], "conduit-plugin-")
		f.assetName = fmt.Sprintf("conduit-plugin-%s_%s_linux_amd64.tar.gz", id, strings.TrimPrefix(f.tag, "v"))
	}
	mux := http.NewServeMux()
	release := func(w http.ResponseWriter, r *http.Request) {
		f.paths = append(f.paths, r.URL.Path)
		if r.Header.Get("User-Agent") != "conduitd-plugin-installer" {
			f.t.Errorf("User-Agent inesperado: %q", r.Header.Get("User-Agent"))
		}
		assets := ""
		if !f.noAsset {
			assets = fmt.Sprintf(`{"name":%q,"browser_download_url":%q},`, f.assetName, f.srv.URL+"/dl/"+f.assetName)
		}
		assets += fmt.Sprintf(`{"name":"checksums.txt","browser_download_url":%q}`, f.srv.URL+"/dl/checksums.txt")
		fmt.Fprintf(w, `{"tag_name":%q,"assets":[%s]}`, f.tag, assets)
	}
	mux.HandleFunc("/repos/"+f.repo+"/releases/latest", release)
	mux.HandleFunc("/repos/"+f.repo+"/releases/tags/", release)
	mux.HandleFunc("/dl/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
		cs := fmt.Sprintf("%s  %s\n", sum(f.archive), f.assetName)
		if f.checksums != nil {
			cs = f.checksums(f.assetName, f.archive)
		}
		_, _ = w.Write([]byte(cs))
	})
	mux.HandleFunc("/dl/"+f.assetName, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(f.archive) })
	f.srv = httptest.NewServer(mux)
	f.t.Cleanup(f.srv.Close)
	in := New(filepath.Join(f.t.TempDir(), "plugins"))
	in.APIBase = f.srv.URL
	in.GOOS, in.GOARCH = "linux", "amd64"
	return in
}

func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	es, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var n []string
	for _, e := range es {
		n = append(n, e.Name())
	}
	return n
}

func TestInstallOfficialID(t *testing.T) {
	f := &fakeRelease{t: t, repo: "cestila/conduit-plugin-telegram", tag: "v1.2.3",
		archive: makeTarGz(t, entry{name: "conduit-plugin-telegram", body: "BINARY"})}
	in := f.start()
	res, err := in.Install(t.Context(), "telegram", "")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(in.PluginDir, "conduit-plugin-telegram")
	if res.ID != "telegram" || res.Version != "v1.2.3" || res.Path != want {
		t.Fatalf("resultado: %+v", res)
	}
	got, _ := os.ReadFile(want)
	if string(got) != "BINARY" {
		t.Fatalf("contenido: %q", got)
	}
	if st, _ := os.Stat(want); st.Mode().Perm() != 0o755 {
		t.Fatalf("modo: %v", st.Mode())
	}
	if len(f.paths) != 1 || f.paths[0] != "/repos/cestila/conduit-plugin-telegram/releases/latest" {
		t.Fatalf("paths: %v", f.paths)
	}
	if n := dirNames(t, in.PluginDir); len(n) != 1 {
		t.Fatalf("archivos sobrantes: %v", n)
	}
}

func TestInstallOwnerRepoAndExplicitVersion(t *testing.T) {
	f := &fakeRelease{t: t, repo: "acme/conduit-plugin-signal", tag: "v0.4.0",
		archive: makeTarGz(t, entry{name: "./conduit-plugin-signal", body: "SIG"})}
	in := f.start()
	res, err := in.Install(t.Context(), "acme/conduit-plugin-signal", "v0.4.0")
	if err != nil {
		t.Fatal(err)
	}
	if res.ID != "signal" || res.Version != "v0.4.0" {
		t.Fatalf("resultado: %+v", res)
	}
	if len(f.paths) != 1 || f.paths[0] != "/repos/acme/conduit-plugin-signal/releases/tags/v0.4.0" {
		t.Fatalf("paths: %v", f.paths)
	}
}

func TestInstallChecksumMismatchKeepsPrevious(t *testing.T) {
	f := &fakeRelease{t: t, repo: "cestila/conduit-plugin-discord", tag: "v1.0.0",
		archive: makeTarGz(t, entry{name: "conduit-plugin-discord", body: "NEW"})}
	f.checksums = func(name string, _ []byte) string { return strings.Repeat("0", 64) + "  " + name + "\n" }
	in := f.start()
	if err := os.MkdirAll(in.PluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(in.PluginDir, "conduit-plugin-discord")
	if err := os.WriteFile(old, []byte("OLD"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := in.Install(t.Context(), "discord", "")
	if err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("esperaba error de checksum: %v", err)
	}
	if b, _ := os.ReadFile(old); string(b) != "OLD" {
		t.Fatalf("binario previo alterado: %q", b)
	}
	if n := dirNames(t, in.PluginDir); len(n) != 1 {
		t.Fatalf("archivos temporales sobrantes: %v", n)
	}
}

func TestInstallMissingChecksumLine(t *testing.T) {
	f := &fakeRelease{t: t, repo: "cestila/conduit-plugin-discord", tag: "v1.0.0",
		archive: makeTarGz(t, entry{name: "conduit-plugin-discord", body: "NEW"})}
	f.checksums = func(string, []byte) string { return sum([]byte("x")) + "  other_file.tar.gz\n" }
	in := f.start()
	_, err := in.Install(t.Context(), "discord", "")
	if err == nil || !strings.Contains(err.Error(), "checksums.txt") {
		t.Fatalf("esperaba error de línea faltante: %v", err)
	}
	if _, serr := os.Stat(filepath.Join(in.PluginDir, "conduit-plugin-discord")); serr == nil {
		t.Fatal("no debía instalarse")
	}
}

func TestInstallMissingPlatformAsset(t *testing.T) {
	f := &fakeRelease{t: t, repo: "cestila/conduit-plugin-discord", tag: "v1.0.0", noAsset: true, assetName: "x"}
	in := f.start()
	in.GOOS, in.GOARCH = "plan9", "mips"
	_, err := in.Install(t.Context(), "discord", "")
	if err == nil || !strings.Contains(err.Error(), "plan9") || !strings.Contains(err.Error(), "mips") {
		t.Fatalf("el error debe nombrar os/arch: %v", err)
	}
}

func TestInstallArchiveWithoutBinary(t *testing.T) {
	f := &fakeRelease{t: t, repo: "cestila/conduit-plugin-discord", tag: "v1.0.0",
		archive: makeTarGz(t, entry{name: "README.md", body: "hi"})}
	in := f.start()
	_, err := in.Install(t.Context(), "discord", "")
	if err == nil || !strings.Contains(err.Error(), "conduit-plugin-discord") {
		t.Fatalf("esperaba error de binario ausente: %v", err)
	}
	if n := dirNames(t, in.PluginDir); len(n) != 0 {
		t.Fatalf("sobrantes: %v", n)
	}
}

func TestInstallRejectsUnsafeEntries(t *testing.T) {
	cases := map[string][]entry{
		"traversal": {{name: "../conduit-plugin-discord", body: "EVIL"}},
		"absolute":  {{name: "/tmp/conduit-plugin-discord", body: "EVIL"}},
		"symlink":   {{name: "conduit-plugin-discord", typeflag: tar.TypeSymlink, link: "/etc/passwd"}},
	}
	for name, entries := range cases {
		t.Run(name, func(t *testing.T) {
			f := &fakeRelease{t: t, repo: "cestila/conduit-plugin-discord", tag: "v1.0.0", archive: makeTarGz(t, entries...)}
			in := f.start()
			if _, err := in.Install(t.Context(), "discord", ""); err == nil {
				t.Fatal("esperaba error")
			}
			if n := dirNames(t, in.PluginDir); len(n) != 0 {
				t.Fatalf("sobrantes: %v", n)
			}
			if _, err := os.Stat(filepath.Join(filepath.Dir(in.PluginDir), "conduit-plugin-discord")); err == nil {
				t.Fatal("escribió fuera del directorio")
			}
		})
	}
}

func TestResolveInvalidSpecs(t *testing.T) {
	in := New(t.TempDir())
	for _, spec := range []string{"", "../x", "Foo", "owner/notaplugin", "a/b/c", "owner/conduit-plugin-Bad", "-o/conduit-plugin-x", "owner/conduit-plugin-"} {
		if _, _, err := in.Resolve(spec); err == nil {
			t.Errorf("spec %q debería fallar", spec)
		} else if !strings.Contains(err.Error(), "owner/repo") {
			t.Errorf("el error de %q debe explicar formas aceptadas: %v", spec, err)
		}
	}
	if id, repo, err := in.Resolve("whatsapp"); err != nil || id != "whatsapp" || repo != "cestila/conduit-plugin-whatsapp" {
		t.Fatalf("whatsapp: %q %q %v", id, repo, err)
	}
	if id, repo, err := in.Resolve("me/conduit-plugin-foo"); err != nil || id != "foo" || repo != "me/conduit-plugin-foo" {
		t.Fatalf("owner/repo: %q %q %v", id, repo, err)
	}
}

func TestInstallRejectsBadVersion(t *testing.T) {
	in := New(t.TempDir())
	if _, err := in.Install(t.Context(), "discord", "../../x"); err == nil {
		t.Fatal("versión inválida debe fallar")
	}
}

func TestRejectsPlainHTTPDownloadWhenNotOverridden(t *testing.T) {
	in := New(t.TempDir())
	if err := in.checkURL("http://example.com/x"); err == nil {
		t.Fatal("http debe rechazarse con la API por defecto")
	}
	if err := in.checkURL("https://example.com/x"); err != nil {
		t.Fatal(err)
	}
}

func TestRemove(t *testing.T) {
	in := New(t.TempDir())
	p := filepath.Join(in.PluginDir, "conduit-plugin-foo")
	if err := os.WriteFile(p, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := in.Remove("foo")
	if err != nil || got != p {
		t.Fatalf("remove: %q %v", got, err)
	}
	if _, err := os.Stat(p); err == nil {
		t.Fatal("no se borró")
	}
	if _, err := in.Remove("foo"); err == nil {
		t.Fatal("segundo remove debe fallar")
	}
	if _, err := in.Remove("../foo"); err == nil {
		t.Fatal("id inválido debe fallar")
	}
}
