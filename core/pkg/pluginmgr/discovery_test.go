package pluginmgr

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSearchDirsOrderAndDedup(t *testing.T) {
	env := "/env/a" + string(os.PathListSeparator) + "/env/b" + string(os.PathListSeparator)
	got := SearchDirs(env, "/data", "/exec", "/cwd")
	want := []string{
		"/env/a",
		"/env/b",
		filepath.Join("/data", "plugins"),
		filepath.Join("/exec", "plugins"),
		"/exec",
		filepath.Join("/cwd", "bin", "plugins"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SearchDirs() = %v, want %v", got, want)
	}

	// Same directory reachable twice is listed once, keeping the first position.
	got = SearchDirs("", "/same", "/same/plugins/..", "/cwd")
	want = []string{filepath.Join("/same", "plugins"), "/same", filepath.Join("/cwd", "bin", "plugins")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SearchDirs() dedup = %v, want %v", got, want)
	}
}

func writeFile(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), mode); err != nil {
		t.Fatal(err)
	}
}

func TestFindBinariesFiltersAndKeepsFirstByName(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	writeFile(t, filepath.Join(first, "conduit-plugin-telegram"), 0o755)
	writeFile(t, filepath.Join(first, "conduit-plugin-notexec"), 0o644)
	writeFile(t, filepath.Join(first, "other-binary"), 0o755)
	if err := os.Mkdir(filepath.Join(first, "conduit-plugin-dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(second, "conduit-plugin-telegram"), 0o755)
	writeFile(t, filepath.Join(second, "conduit-plugin-discord"), 0o755)

	got := FindBinaries([]string{first, filepath.Join(first, "missing"), second})
	want := []string{
		filepath.Join(first, "conduit-plugin-telegram"),
		filepath.Join(second, "conduit-plugin-discord"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("FindBinaries() = %v, want %v", got, want)
	}
}

func builtPlugin(t *testing.T, name string) string {
	t.Helper()
	path, err := filepath.Abs("../../../bin/plugins/conduit-plugin-" + name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Skipf("plugin binary not built (run make build): %v", err)
	}
	return path
}

func TestLaunchDiscoveredRegistersByManifestID(t *testing.T) {
	path := builtPlugin(t, "discord")
	mgr := NewManager()
	defer mgr.StopAll()

	inst, err := mgr.LaunchDiscovered(context.Background(), path, func(string) bool { return true })
	if err != nil {
		t.Fatalf("LaunchDiscovered: %v", err)
	}
	if inst.Name != "discord" || inst.Manifest.GetId() != "discord" || inst.Path != path {
		t.Fatalf("instance = %+v", inst)
	}
	if got, ok := mgr.GetInstance("discord"); !ok || got != inst {
		t.Fatalf("instance not registered under manifest id")
	}

	if _, err := mgr.LaunchDiscovered(context.Background(), path, func(string) bool { return true }); !errors.Is(err, ErrDuplicatePlugin) {
		t.Fatalf("second launch error = %v, want ErrDuplicatePlugin", err)
	}

	found := mgr.Discovered()
	if len(found) != 1 || !found[0].Running || found[0].Manifest.GetId() != "discord" {
		t.Fatalf("Discovered() = %+v", found)
	}
}

func TestLaunchDiscoveredDisabledIsKnownButNotRunning(t *testing.T) {
	path := builtPlugin(t, "discord")
	mgr := NewManager()
	defer mgr.StopAll()

	_, err := mgr.LaunchDiscovered(context.Background(), path, func(id string) bool { return id != "discord" })
	if !errors.Is(err, ErrPluginDisabled) {
		t.Fatalf("error = %v, want ErrPluginDisabled", err)
	}
	if _, ok := mgr.GetInstance("discord"); ok {
		t.Fatal("disabled plugin must not be registered as running")
	}
	found := mgr.Discovered()
	if len(found) != 1 || found[0].Running || found[0].Path != path || found[0].Manifest.GetId() != "discord" {
		t.Fatalf("Discovered() = %+v", found)
	}
}

func TestStopToleratesInstancesWithoutClient(t *testing.T) {
	mgr := NewManager()
	mgr.RegisterInstance("fake", &Instance{Name: "fake"})
	mgr.Stop("fake")
	mgr.RegisterInstance("fake", &Instance{Name: "fake"})
	mgr.StopAll()
}
