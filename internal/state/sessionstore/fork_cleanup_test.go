package sessionstore

import (
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
	handled, err := reconcilePending(pending[0], func(CleanupPendingInfo) error {
		t.Fatal("fork published after the scan must not be cleaned up")
		return nil
	})
	if !handled || err != nil {
		t.Fatalf("stale scan: handled=%v, err=%v", handled, err)
	}
}
