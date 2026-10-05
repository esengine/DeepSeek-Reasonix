package sessionstore

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/state/store"
)

// The sweep exists for spill directories every deleter skipped, so it must take
// exactly those: transcript gone, and old enough that no session elsewhere is
// mid-creation.
func TestOrphanOutputsSweep(t *testing.T) {
	dir := testenv.TempDir(t)

	live := filepath.Join(dir, "live.jsonl")
	if err := writeFile(live, []byte("{}\n")); err != nil {
		t.Fatal(err)
	}
	liveOutputs := store.SessionOutputsDir(live)
	mkOutputs(t, liveOutputs, true)

	orphan := filepath.Join(dir, "deleted.outputs")
	mkOutputs(t, orphan, true)

	young := filepath.Join(dir, "just-made.outputs")
	mkOutputs(t, young, false)

	unrelated := filepath.Join(dir, "notes")
	mkOutputs(t, unrelated, true)

	if err := reconcileOrphanOutputs(dir); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	for _, keep := range []string{liveOutputs, young, unrelated} {
		if _, err := os.Stat(keep); err != nil {
			t.Errorf("swept something it should not have: %s (%v)", filepath.Base(keep), err)
		}
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Errorf("the orphan survived the sweep: %v", err)
	}
}

// A missing directory is not an error: most session dirs have never spilled.
func TestOrphanOutputsSweepToleratesMissingDir(t *testing.T) {
	if err := reconcileOrphanOutputs(filepath.Join(testenv.TempDir(t), "never-existed")); err != nil {
		t.Errorf("sweep of an absent directory: %v", err)
	}
}

func mkOutputs(t *testing.T, path string, aged bool) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(filepath.Join(path, "call-1.txt"), []byte("spilled output\n")); err != nil {
		t.Fatal(err)
	}
	if !aged {
		return
	}
	old := time.Now().Add(-2 * orphanOutputsGrace)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
}
