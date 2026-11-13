package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cestila/ConduitOmniChat/core/pkg/plugininstall"
)

type fakeInstaller struct {
	spec, version string
	err           error
	removed       string
}

func (f *fakeInstaller) Install(_ context.Context, spec, version string) (plugininstall.Result, error) {
	f.spec, f.version = spec, version
	if f.err != nil {
		return plugininstall.Result{}, f.err
	}
	return plugininstall.Result{ID: "telegram", Version: "v1.0.0", Path: "/p/conduit-plugin-telegram"}, nil
}

func (f *fakeInstaller) Remove(id string) (string, error) {
	f.removed = id
	if f.err != nil {
		return "", f.err
	}
	return "/p/conduit-plugin-" + id, nil
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
		fi := &fakeInstaller{}
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
