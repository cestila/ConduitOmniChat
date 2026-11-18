package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cestila/ConduitOmniChat/core/pkg/buildinfo"
	"github.com/cestila/ConduitOmniChat/core/pkg/plugininstall"
	"github.com/cestila/ConduitOmniChat/core/pkg/pluginmgr"
)

type fakeInstaller struct {
	spec, version string
	resVersion    string
	err           error
	removed       string
}

func (f *fakeInstaller) Install(_ context.Context, spec, version string) (plugininstall.Result, error) {
	f.spec, f.version = spec, version
	if f.err != nil {
		return plugininstall.Result{}, f.err
	}
	id := spec
	if id == "" {
		id = "telegram"
	}
	ver := "v1.0.0"
	if f.resVersion != "" {
		ver = f.resVersion
	}
	return plugininstall.Result{ID: id, Version: ver, Path: "/p/conduit-plugin-" + id}, nil
}

func (f *fakeInstaller) Remove(id string) (string, error) {
	f.removed = id
	if f.err != nil {
		return "", f.err
	}
	return "/p/conduit-plugin-" + id, nil
}

func (f *fakeInstaller) IsManagedPath(string) bool {
	return true
}

func run(t *testing.T, deps cliDeps, args ...string) (handled bool, code int, out, errOut string) {
	t.Helper()
	var o, e bytes.Buffer
	handled, code = runCLI(args, &o, &e, deps)
	return handled, code, o.String(), e.String()
}

func TestCLINoArgsRunsDaemon(t *testing.T) {
	handled, code, out, errOut := run(t, cliDeps{})
	if handled || code != 0 || out != "" || errOut != "" {
		t.Fatalf("got %v %d %q %q", handled, code, out, errOut)
	}
}

func TestCLIHelp(t *testing.T) {
	deps := cliDeps{PluginDirs: []string{"/a/plugins", "/b"}}
	for _, arg := range []string{"help", "-h", "--help"} {
		handled, code, out, errOut := run(t, deps, arg)
		if !handled || code != 0 || errOut != "" {
			t.Fatalf("%s: %v %d %q", arg, handled, code, errOut)
		}
		want := "conduitd - ConduitOmniChat daemon\n\n" + cliUsage +
			"\nCommands:\n" +
			"  plugin install   download a plugin from its GitHub release, verify its SHA-256 and install it\n" +
			"  plugin list      list the installed plugin binaries\n" +
			"  plugin remove    delete an installed plugin binary\n" +
			"  plugin check     check for plugin, Core, and TUI updates\n" +
			"  plugin update    update one plugin or all managed plugins\n" +
			"  version          print version and build information\n" +
			"\nPlugin directories (highest precedence first):\n  /a/plugins\n  /b\n" +
			"\nEnvironment:\n" +
			"  CONDUIT_PLUGIN_DIR   extra plugin directories searched first\n" +
			"  CONDUIT_GITHUB_API   GitHub API base URL (default https://api.github.com)\n"
		if out != want {
			t.Fatalf("%s: help distinto:\n%s", arg, out)
		}
	}
}

func TestCLIUsageErrors(t *testing.T) {
	cases := []struct {
		args []string
		msg  string
	}{
		{[]string{"bogus"}, `unknown command "bogus"`},
		{[]string{"plugin"}, `missing plugin subcommand`},
		{[]string{"plugin", "frob"}, `unknown plugin subcommand "frob"`},
		{[]string{"plugin", "install"}, `plugin install needs a plugin id or owner/repo`},
		{[]string{"plugin", "install", "x", "--nope"}, `unknown flag "--nope"`},
		{[]string{"plugin", "install", "x", "--version"}, `flag --version needs a value`},
		{[]string{"plugin", "install", "a", "b"}, `unexpected argument "b"`},
		{[]string{"plugin", "remove"}, `plugin remove needs a plugin id`},
		{[]string{"plugin", "remove", "-x"}, `unknown flag "-x"`},
		{[]string{"plugin", "list", "extra"}, `unexpected argument "extra"`},
	}
	for _, c := range cases {
		fi := &fakeInstaller{resVersion: "1.2.0"}
		handled, code, out, errOut := run(t, cliDeps{Installer: fi}, c.args...)
		want := "conduitd: " + c.msg + "\n\n" + cliUsage
		if !handled || code != 2 || out != "" || errOut != want {
			t.Errorf("%v: %v %d %q %q", c.args, handled, code, out, errOut)
		}
		if fi.spec != "" || fi.removed != "" {
			t.Errorf("%v: no debía invocar al instalador", c.args)
		}
	}
}

func TestCLIListEmptyAndNonEmpty(t *testing.T) {
	dir := t.TempDir()
	deps := cliDeps{PluginDirs: []string{dir}}
	_, code, out, errOut := run(t, deps, "plugin", "list")
	if code != 0 || out != "no plugins installed\n" || errOut != "" {
		t.Fatalf("vacío: %d %q %q", code, out, errOut)
	}
	p := filepath.Join(dir, "conduit-plugin-foo")
	if err := os.WriteFile(p, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "conduit-plugin-noexec"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, code, out, errOut = run(t, deps, "plugin", "list")
	if code != 0 || out != "foo\t"+p+"\n" || errOut != "" {
		t.Fatalf("lleno: %d %q %q", code, out, errOut)
	}
}

func TestCLIInstall(t *testing.T) {
	fi := &fakeInstaller{}
	_, code, out, errOut := run(t, cliDeps{Installer: fi}, "plugin", "install", "telegram", "--version", "v1.0.0")
	if code != 0 || out != "installed telegram v1.0.0 -> /p/conduit-plugin-telegram\n" || errOut != "" {
		t.Fatalf("%d %q %q", code, out, errOut)
	}
	if fi.spec != "telegram" || fi.version != "v1.0.0" {
		t.Fatalf("args: %q %q", fi.spec, fi.version)
	}
	fi = &fakeInstaller{}
	run(t, cliDeps{Installer: fi}, "plugin", "install", "--version=v2", "acme/conduit-plugin-x")
	if fi.spec != "acme/conduit-plugin-x" || fi.version != "v2" {
		t.Fatalf("args: %q %q", fi.spec, fi.version)
	}
}

func TestCLIInstallAndRemoveFailures(t *testing.T) {
	fi := &fakeInstaller{err: errors.New("boom")}
	for _, args := range [][]string{{"plugin", "install", "telegram"}, {"plugin", "remove", "telegram"}} {
		handled, code, out, errOut := run(t, cliDeps{Installer: fi}, args...)
		if !handled || code != 1 || out != "" || errOut != "conduitd: boom\n" {
			t.Fatalf("%v: %d %q %q", args, code, out, errOut)
		}
	}
}

func TestCLIRemove(t *testing.T) {
	fi := &fakeInstaller{}
	_, code, out, errOut := run(t, cliDeps{Installer: fi}, "plugin", "remove", "telegram")
	if code != 0 || out != "removed telegram (/p/conduit-plugin-telegram)\n" || errOut != "" || fi.removed != "telegram" {
		t.Fatalf("%d %q %q %q", code, out, errOut, fi.removed)
	}
	if strings.Contains(errOut, "usage") {
		t.Fatal("sin uso")
	}
}

func TestRunCLIVersion(t *testing.T) {
	origVersion, origCommit, origDate := buildinfo.Version, buildinfo.Commit, buildinfo.Date
	t.Cleanup(func() {
		buildinfo.Version, buildinfo.Commit, buildinfo.Date = origVersion, origCommit, origDate
	})

	t.Run("default version flags", func(t *testing.T) {
		for _, arg := range []string{"version", "--version", "-v"} {
			handled, code, out, errOut := run(t, cliDeps{}, arg)
			if !handled || code != 0 || errOut != "" {
				t.Fatalf("%s: handled=%v code=%d errOut=%q", arg, handled, code, errOut)
			}
			if !strings.Contains(out, buildinfo.Version) {
				t.Fatalf("%s: output %q does not contain %q", arg, out, buildinfo.Version)
			}
		}
	})

	t.Run("injected build metadata", func(t *testing.T) {
		buildinfo.Version = "v0.1.2"
		buildinfo.Commit = "abc1234"
		buildinfo.Date = "2026-11-18"
		handled, code, out, errOut := run(t, cliDeps{}, "version")
		want := "conduitd v0.1.2 (commit abc1234, built 2026-11-18)\n"
		if !handled || code != 0 || errOut != "" || out != want {
			t.Fatalf("got handled=%v code=%d out=%q errOut=%q, want out=%q", handled, code, out, errOut, want)
		}
	})

	t.Run("unexpected extra argument", func(t *testing.T) {
		handled, code, out, errOut := run(t, cliDeps{}, "version", "extra")
		wantErr := "conduitd: unexpected argument \"extra\"\n\n" + cliUsage
		if !handled || code != 2 || out != "" || errOut != wantErr {
			t.Fatalf("got handled=%v code=%d out=%q errOut=%q", handled, code, out, errOut)
		}
	})
}

func TestCLIPluginCheckAndUpdate(t *testing.T) {
	t.Run("plugin check", func(t *testing.T) {
		dir := t.TempDir()
		binPath := filepath.Join(dir, "conduit-plugin-telegram")
		if err := os.WriteFile(binPath, []byte("x"), 0o755); err != nil {
			t.Fatal(err)
		}
		handled, code, out, errOut := run(t, cliDeps{
			Installer:  &fakeInstaller{},
			PluginDirs: []string{dir},
			TUIVersion: "0.4.0",
		}, "plugin", "check")
		if !handled || code != 0 || errOut != "" {
			t.Fatalf("plugin check failed: handled=%v code=%d out=%q errOut=%q", handled, code, out, errOut)
		}
		if !strings.Contains(out, "core\t") {
			t.Fatalf("expected core in check output, got %q", out)
		}
		if !strings.Contains(out, "tui\t0.4.0") {
			t.Fatalf("expected tui 0.4.0 in check output, got %q", out)
		}
		if !strings.Contains(out, "telegram") {
			t.Fatalf("expected telegram in check output, got %q", out)
		}
	})

	t.Run("plugin update --all", func(t *testing.T) {
		dir := t.TempDir()
		binPath := filepath.Join(dir, "conduit-plugin-telegram")
		if err := os.WriteFile(binPath, []byte("x"), 0o755); err != nil {
			t.Fatal(err)
		}
		fi := &fakeInstaller{resVersion: "1.2.0"}
		handled, code, out, errOut := run(t, cliDeps{
			Installer:  fi,
			PluginDirs: []string{dir},
		}, "plugin", "update", "--all")
		if !handled || code != 0 || errOut != "" {
			t.Fatalf("plugin update --all failed: handled=%v code=%d out=%q errOut=%q", handled, code, out, errOut)
		}
		if fi.spec != "telegram" {
			t.Fatalf("expected installer to be called for telegram, got %q", fi.spec)
		}
		if !strings.Contains(out, "telegram") {
			t.Fatalf("expected telegram in update output, got %q", out)
		}
	})

	t.Run("plugin update single id", func(t *testing.T) {
		fi := &fakeInstaller{resVersion: "1.2.0"}
		handled, code, out, errOut := run(t, cliDeps{Installer: fi}, "plugin", "update", "telegram")
		if !handled || code != 0 || errOut != "" {
			t.Fatalf("plugin update telegram failed: handled=%v code=%d out=%q errOut=%q", handled, code, out, errOut)
		}
		if fi.spec != "telegram" || !strings.Contains(out, "updated telegram") {
			t.Fatalf("unexpected output: spec=%q out=%q", fi.spec, out)
		}
	})

	t.Run("plugin update usage errors", func(t *testing.T) {
		for _, args := range [][]string{
			{"plugin", "update"},
			{"plugin", "update", "telegram", "--all"},
			{"plugin", "check", "extra"},
		} {
			handled, code, _, _ := run(t, cliDeps{Installer: &fakeInstaller{}}, args...)
			if !handled || code != 2 {
				t.Fatalf("%v: expected usage error code 2, got handled=%v code=%d", args, handled, code)
			}
		}
	})

	t.Run("plugin update updates manifest and skips up-to-date and incompatible", func(t *testing.T) {
		dir := t.TempDir()
		for _, id := range []string{"telegram", "whatsapp", "signal"} {
			if err := os.WriteFile(filepath.Join(dir, "conduit-plugin-"+id), []byte("x"), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		manifestPath := filepath.Join(t.TempDir(), "plugins-manifest.json")
		store := pluginmgr.NewManifestStore(manifestPath)
		if err := store.Save(&pluginmgr.LocalManifest{
			SchemaVersion: 1,
			Plugins: map[string]pluginmgr.LocalPluginRecord{
				"telegram": {
					ID:               "telegram",
					InstalledVersion: "1.0.0",
					LatestVersion:    "1.0.0",
				},
				"whatsapp": {
					ID:               "whatsapp",
					InstalledVersion: "1.0.0",
					LatestVersion:    "1.2.0",
				},
				"signal": {
					ID:                 "signal",
					InstalledVersion:   "1.0.0",
					LatestVersion:      "2.0.0",
					MinProtocolVersion: 999,
				},
			},
		}); err != nil {
			t.Fatal(err)
		}

		fi := &fakeInstaller{resVersion: "1.2.0"}
		handled, code, out, errOut := run(t, cliDeps{
			Installer:  fi,
			PluginDirs: []string{dir},
			Manifest:   store,
		}, "plugin", "update", "--all")
		if !handled || code != 0 || errOut != "" {
			t.Fatalf("plugin update --all failed: handled=%v code=%d out=%q errOut=%q", handled, code, out, errOut)
		}
		if !strings.Contains(out, "up-to-date telegram (1.0.0)") {
			t.Fatalf("expected telegram to be skipped as up-to-date, got %q", out)
		}
		if !strings.Contains(out, "skipped signal (incompatible:") {
			t.Fatalf("expected signal to be skipped as incompatible, got %q", out)
		}
		if !strings.Contains(out, "updated whatsapp") {
			t.Fatalf("expected whatsapp to be updated, got %q", out)
		}
		loaded, err := store.Load()
		if err != nil {
			t.Fatal(err)
		}
		if got := loaded.Plugins["whatsapp"].InstalledVersion; got != "1.2.0" {
			t.Fatalf("expected manifest InstalledVersion to be updated to 1.2.0, got %q", got)
		}

		// Single incompatible plugin update returns exit code 1.
		handled, code, _, errOut = run(t, cliDeps{
			Installer:  &fakeInstaller{},
			PluginDirs: []string{dir},
			Manifest:   store,
		}, "plugin", "update", "signal")
		if !handled || code != 1 || !strings.Contains(errOut, "incompatible") {
			t.Fatalf("expected incompatible error for single plugin update, got handled=%v code=%d errOut=%q", handled, code, errOut)
		}
	})
}
