package pluginmgr

import (
	"context"
	"testing"

	conduitv1 "github.com/cestila/ConduitOmniChat/sdk/conduitv1"
)

func TestManagerStopPluginRemovesFromActiveAndDisabled(t *testing.T) {
	mgr := NewManager()

	mgr.RegisterInstance("active", &Instance{
		Name:     "active",
		Manifest: &conduitv1.PluginManifest{Id: "active", DisplayName: "Active", Version: "1.0.0"},
		Path:     "/tmp/conduit-plugin-active",
	})
	mgr.mu.Lock()
	mgr.disabled["disabled"] = Discovered{
		Manifest: &conduitv1.PluginManifest{Id: "disabled", DisplayName: "Disabled", Version: "1.0.0"},
		Path:     "/tmp/conduit-plugin-disabled",
	}
	mgr.mu.Unlock()

	if err := mgr.StopPlugin("active"); err != nil {
		t.Fatalf("StopPlugin(active) unexpected error: %v", err)
	}
	if _, ok := mgr.GetInstance("active"); ok {
		t.Fatal("active plugin still present in m.plugins after StopPlugin")
	}

	if err := mgr.StopPlugin("disabled"); err != nil {
		t.Fatalf("StopPlugin(disabled) unexpected error: %v", err)
	}
	if found := mgr.Discovered(); len(found) != 0 {
		t.Fatalf("Discovered() = %+v, want empty after stopping active and disabled plugins", found)
	}

	if err := mgr.StopPlugin("missing"); err != nil {
		t.Fatalf("StopPlugin(missing) = %v, want nil", err)
	}
}

func TestManagerStopPluginKillsRunningClient(t *testing.T) {
	path := stubPluginBinary(t)
	mgr := NewManager()
	defer mgr.StopAll()

	inst, err := mgr.LaunchDiscovered(context.Background(), path, nil)
	if err != nil {
		t.Fatalf("LaunchDiscovered: %v", err)
	}
	if inst.Client == nil {
		t.Fatal("expected non-nil plugin Client")
	}

	if err := mgr.StopPlugin("stub"); err != nil {
		t.Fatalf("StopPlugin(stub) error: %v", err)
	}
	if !inst.Client.Exited() {
		t.Fatal("expected plugin process to exit after StopPlugin")
	}
	if _, ok := mgr.GetInstance("stub"); ok {
		t.Fatal("stub still registered after StopPlugin")
	}
}
