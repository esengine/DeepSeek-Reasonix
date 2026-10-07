package sessionstore

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestReconcilePendingDoesNotDeletePublishedFork(t *testing.T) {
	path := filepath.Join(testenv.TempDir(t), "fork.jsonl")
	if err := MarkCleanupPending(path, ForkPendingOperation); err != nil {
		t.Fatal(err)
	}
	pending, err := ListCleanupPending(filepath.Dir(path))
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending fork: %v, %v", pending, err)
	}
	if err := ClearCleanupPending(path); err != nil {
		t.Fatal(err)
	}
	handled, err := reconcilePending(filepath.Dir(path), pending[0], func(CleanupPendingInfo) error {
		t.Fatal("fork published after the scan must not be cleaned up")
		return nil
	})
	if !handled || err != nil {
		t.Fatalf("stale scan: handled=%v, err=%v", handled, err)
	}
}

func TestReconcilePendingRejectsMismatchedPaths(t *testing.T) {
	dir := testenv.TempDir(t)
	path := filepath.Join(dir, "fork.jsonl")
	for _, scenario := range []struct {
		name    string
		session string
		marker  string
	}{
		{"other-marker", path, CleanupPendingPath(filepath.Join(dir, "other.jsonl"))},
		{"outside-session", filepath.Join(testenv.TempDir(t), "fork.jsonl"), CleanupPendingPath(path)},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			item := CleanupPendingInfo{
				SessionPath: scenario.session,
				MarkerPath:  scenario.marker,
				Meta:        CleanupPendingMeta{Operation: ForkPendingOperation},
			}
			handled, err := reconcilePending(dir, item, func(CleanupPendingInfo) error {
				t.Fatal("invalid pending fork reached cleanup")
				return nil
			})
			if !handled || err == nil {
				t.Fatalf("invalid paths accepted: handled=%v, err=%v", handled, err)
			}
		})
	}
}

func TestReconcilePendingRejectsMarkerSymlink(t *testing.T) {
	dir := testenv.TempDir(t)
	path := filepath.Join(dir, "fork.jsonl")
	target := filepath.Join(testenv.TempDir(t), "outside-marker.json")
	if err := os.WriteFile(target, []byte(`{"operation":"fork"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	marker := CleanupPendingPath(path)
	if err := os.Symlink(target, marker); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	pending, err := ListCleanupPending(dir)
	if err != nil || len(pending) != 1 {
		t.Fatalf("symlink marker scan: %v, %v", pending, err)
	}
	handled, err := reconcilePending(dir, pending[0], func(CleanupPendingInfo) error {
		t.Fatal("marker symlink reached cleanup")
		return nil
	})
	if !handled || err == nil {
		t.Fatalf("marker symlink accepted: handled=%v, err=%v", handled, err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("outside marker was changed: %v", err)
	}
}
