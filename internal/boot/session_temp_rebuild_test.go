package boot

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/sessiontemp"
)

func TestRebuildReusesSessionTempManager(t *testing.T) {
	// Isolate config/home so boot.Build does not touch the developer home.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("REASONIX_HOME", filepath.Join(home, ".reasonix"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))

	workspace := t.TempDir()
	fenceBootTestHistoryCatalog(t)
	m := sessiontemp.New()
	old := control.New(control.Options{
		SessionTemp: m,
		Sink:        event.Discard,
		Label:       "test",
	})
	lease, err := old.SessionTemp().Acquire()
	if err != nil {
		t.Fatal(err)
	}
	dir := lease.Dir()
	if err := os.WriteFile(filepath.Join(dir, "keep.txt"), []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	lease.Release()

	res, err := Rebuild(context.Background(), old, Options{
		WorkspaceRoot: workspace,
		Sink:          event.Discard,
		RequireKey:    false,
		Stderr:        os.Stderr,
	})
	if err != nil {
		// Fail-atomic: old manager still works and is not sealed.
		again, aerr := old.SessionTemp().Acquire()
		if aerr != nil {
			t.Fatalf("old session temp broken after failed rebuild: %v", aerr)
		}
		if again.Dir() != dir {
			t.Fatalf("failed rebuild rotated old temp: got %q want %q", again.Dir(), dir)
		}
		body, rerr := os.ReadFile(filepath.Join(dir, "keep.txt"))
		if rerr != nil || string(body) != "ok" {
			t.Fatalf("temp file lost after failed rebuild: %q %v", body, rerr)
		}
		again.Release()
		old.Close()
		return
	}

	// Successful rebuild: replacement shares the Manager and the generation.
	if res == nil || res.Controller == nil {
		t.Fatal("Rebuild returned nil result without error")
	}
	newCtrl := res.Controller
	if newCtrl.SessionTemp() != m {
		t.Fatal("replacement controller must share the old SessionTemp Manager")
	}
	if old.SessionTemp() != m {
		t.Fatal("old controller lost SessionTemp identity during rebuild")
	}
	again, err := newCtrl.SessionTemp().Acquire()
	if err != nil {
		t.Fatal(err)
	}
	if again.Dir() != dir {
		t.Fatalf("rebuild changed generation: got %q want %q", again.Dir(), dir)
	}
	body, err := os.ReadFile(filepath.Join(dir, "keep.txt"))
	if err != nil || string(body) != "ok" {
		t.Fatalf("temp file lost across rebuild: %q %v", body, err)
	}
	again.Release()

	// Publish-order: release old after replacement is live.
	old.ReleaseResources()
	if m.Sealed() {
		t.Fatal("shared manager must not seal while replacement still owns it")
	}
	still, err := newCtrl.SessionTemp().Acquire()
	if err != nil {
		t.Fatalf("replacement lost temp after old close: %v", err)
	}
	still.Release()
	newCtrl.Close()
}
