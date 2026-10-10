package boot

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/state/sessionstore"
	"reasonix/internal/state/store"
)

func sessionDirListing(t *testing.T, dir string) map[string]bool {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, e := range entries {
		if !store.IsSessionTranscriptName(e.Name()) {
			continue
		}
		out[e.Name()] = true
	}
	return out
}

func TestEffect1xOpenedWithoutATurnWritesNothing(t *testing.T) {
	src, err := filepath.Abs(filepath.Join("testdata", "session1x"))
	if err != nil {
		t.Fatal(err)
	}
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	rec := &effectRecordingProvider{}
	provider.Register("effect-1x-open", func(provider.Config) (provider.Provider, error) { return rec, nil })
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "effect-1x-open"
model = "x"
`)
	approveWorkspace(t, dir)
	sessions := filepath.Join(dir, "sessions")
	path, files := copySession1x(t, src, sessions, nil)
	before := sessionDirListing(t, sessions)

	ctrl, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	loaded, err := sessionstore.LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := ctrl.Resume(loaded, path); err != nil {
		t.Fatal(err)
	}
	if err := ctrl.Snapshot(); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	ctrl.SnapshotForShutdown()
	next, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := ApplyRuntimeMigration(next, ctrl, CaptureRuntimeMigration(ctrl)); err != nil {
		t.Fatal(err)
	}
	if err := next.Snapshot(); err != nil {
		t.Fatalf("Snapshot after rebuild: %v", err)
	}
	next.SnapshotForShutdown()
	next.Close()
	ctrl.Close()
	if next.SessionPath() != path {
		t.Fatalf("rebuilding moved the session to %s", next.SessionPath())
	}

	if ctrl.SessionPath() != path {
		t.Fatalf("opening moved the session to %s", ctrl.SessionPath())
	}
	for name := range sessionDirListing(t, sessions) {
		if !before[name] {
			t.Errorf("opening wrote %s", name)
		}
	}
	assert1xFilesUntouched(t, "after open and close", files)
}
