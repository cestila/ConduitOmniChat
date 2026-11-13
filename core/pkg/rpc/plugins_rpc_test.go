package rpc

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cestila/ConduitOmniChat/core/pkg/plugininstall"
	"github.com/cestila/ConduitOmniChat/core/pkg/pluginmgr"
	conduitv1 "github.com/cestila/ConduitOmniChat/sdk/conduitv1"
)

func listPluginsByID(t *testing.T, srv *Server) map[string]*conduitv1.ProtoPluginInfo {
	t.Helper()
	resp, err := srv.ListPlugins(context.Background(), &conduitv1.ListPluginsRequest{})
	if err != nil {
		t.Fatalf("ListPlugins falló: %v", err)
	}
	out := map[string]*conduitv1.ProtoPluginInfo{}
	for _, p := range resp.Plugins {
		out[p.Id] = p
	}
	return out
}

func TestListPluginsDiscoveredAndCatalog(t *testing.T) {
	srv, db := newMediaTestServer(t)
	mgr := pluginmgr.NewManager()
	manifest := &conduitv1.PluginManifest{Id: "telegram", DisplayName: "Telegram", Description: "desc", Version: "1.2.3", Author: "me"}
	mgr.RegisterInstance("telegram", &pluginmgr.Instance{Name: "telegram", Manifest: manifest, Path: "/x/conduit-plugin-telegram"})
	srv.plugins = mgr
	_ = db.SetSetting(context.Background(), "plugin_telegram_enabled", "false")

	got := listPluginsByID(t, srv)
	tg := got["telegram"]
	if tg == nil || !tg.IsInstalled || tg.IsEnabled || tg.Manifest == nil || tg.Path != "/x/conduit-plugin-telegram" {
		t.Fatalf("telegram mal listado: %+v", tg)
	}
	if tg.Name != "Telegram" || tg.Version != "1.2.3" || tg.Author != "me" || tg.Description != "desc" {
		t.Fatalf("datos del manifiesto no copiados: %+v", tg)
	}
	if tg.Repository != "cestila/conduit-plugin-telegram" {
		t.Fatalf("repositorio: %q", tg.Repository)
	}
	for _, id := range []string{"whatsapp", "discord"} {
		p := got[id]
		if p == nil || p.IsInstalled || p.IsEnabled || p.Repository != "cestila/conduit-plugin-"+id || p.Manifest != nil {
			t.Fatalf("%s del catálogo mal listado: %+v", id, p)
		}
	}
	if _, ok := got["gmail"]; ok {
		t.Fatal("gmail no debe listarse")
	}
}

func TestListPluginsEnabledDefaultsTrue(t *testing.T) {
	srv, _ := newMediaTestServer(t)
	mgr := pluginmgr.NewManager()
	mgr.RegisterInstance("discord", &pluginmgr.Instance{Name: "discord", Manifest: &conduitv1.PluginManifest{Id: "discord"}})
	srv.plugins = mgr
	if p := listPluginsByID(t, srv)["discord"]; p == nil || !p.IsEnabled || !p.IsInstalled {
		t.Fatalf("discord debería estar habilitado: %+v", p)
	}
}

type fakePluginInstaller struct {
	spec, version string
	err           error
}

func (f *fakePluginInstaller) Install(_ context.Context, spec, version string) (plugininstall.Result, error) {
	f.spec, f.version = spec, version
	if f.err != nil {
		return plugininstall.Result{}, f.err
	}
	return plugininstall.Result{ID: spec, Version: "v1.2.3", Path: "/p/conduit-plugin-" + spec}, nil
}

func TestInstallPluginSuccessEnablesAndLoads(t *testing.T) {
	srv, db := newMediaTestServer(t)
	_ = db.SetSetting(context.Background(), "plugin_telegram_enabled", "false")
	fi := &fakePluginInstaller{}
	srv.SetPluginInstaller(fi)
	var loaded string
	srv.SetPluginLoader(func(_ context.Context, path string) error { loaded = path; return nil })

	resp, err := srv.InstallPlugin(context.Background(), &conduitv1.InstallPluginRequest{PluginId: "telegram"})
	if err != nil || !resp.Success {
		t.Fatalf("resp=%+v err=%v", resp, err)
	}
	if fi.spec != "telegram" || fi.version != "" {
		t.Fatalf("instalador: %q %q", fi.spec, fi.version)
	}
	if got := db.GetSetting(context.Background(), "plugin_telegram_enabled", ""); got != "true" {
		t.Fatalf("enabled=%q", got)
	}
	if loaded != "/p/conduit-plugin-telegram" {
		t.Fatalf("loader: %q", loaded)
	}
	if !strings.Contains(resp.Message, "v1.2.3") || !strings.Contains(resp.Message, "/p/conduit-plugin-telegram") {
		t.Fatalf("mensaje: %q", resp.Message)
	}
}

func TestInstallPluginWithoutLoaderOrLoaderFailureNeedsRestart(t *testing.T) {
	for name, withLoader := range map[string]bool{"sin loader": false, "loader falla": true} {
		t.Run(name, func(t *testing.T) {
			srv, db := newMediaTestServer(t)
			srv.SetPluginInstaller(&fakePluginInstaller{})
			if withLoader {
				srv.SetPluginLoader(func(context.Context, string) error { return errors.New("no arranca") })
			}
			resp, err := srv.InstallPlugin(context.Background(), &conduitv1.InstallPluginRequest{PluginId: "discord"})
			if err != nil || !resp.Success {
				t.Fatalf("resp=%+v err=%v", resp, err)
			}
			if !strings.Contains(strings.ToLower(resp.Message), "restart") {
				t.Fatalf("debe pedir reinicio: %q", resp.Message)
			}
			if got := db.GetSetting(context.Background(), "plugin_discord_enabled", ""); got != "true" {
				t.Fatalf("enabled=%q", got)
			}
		})
	}
}

func TestInstallPluginFailureLeavesSettingsUnchanged(t *testing.T) {
	srv, db := newMediaTestServer(t)
	_ = db.SetSetting(context.Background(), "plugin_discord_enabled", "false")
	srv.SetPluginInstaller(&fakePluginInstaller{err: errors.New("checksum mismatch")})
	called := false
	srv.SetPluginLoader(func(context.Context, string) error { called = true; return nil })
	resp, err := srv.InstallPlugin(context.Background(), &conduitv1.InstallPluginRequest{PluginId: "discord"})
	if err != nil || resp.Success || resp.Message != "checksum mismatch" {
		t.Fatalf("resp=%+v err=%v", resp, err)
	}
	if got := db.GetSetting(context.Background(), "plugin_discord_enabled", ""); got != "false" || called {
		t.Fatalf("enabled=%q loader=%v", got, called)
	}
}

func TestInstallPluginWithoutInstallerFails(t *testing.T) {
	srv, db := newMediaTestServer(t)
	resp, err := srv.InstallPlugin(context.Background(), &conduitv1.InstallPluginRequest{PluginId: "discord"})
	if err != nil || resp.Success {
		t.Fatalf("resp=%+v err=%v", resp, err)
	}
	if got := db.GetSetting(context.Background(), "plugin_discord_enabled", "unset"); got != "unset" {
		t.Fatalf("enabled=%q", got)
	}
}

func TestInstallPluginRejectsNonCatalogID(t *testing.T) {
	srv, _ := newMediaTestServer(t)
	fi := &fakePluginInstaller{}
	srv.SetPluginInstaller(fi)
	for _, id := range []string{"evil/conduit-plugin-x", "../x", "unknown"} {
		resp, err := srv.InstallPlugin(context.Background(), &conduitv1.InstallPluginRequest{PluginId: id})
		if err != nil || resp.Success || fi.spec != "" {
			t.Fatalf("%q: resp=%+v spec=%q", id, resp, fi.spec)
		}
	}
}
