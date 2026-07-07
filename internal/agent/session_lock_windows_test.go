//go:build windows

package agent

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestRemoveAndUnlockDeletesViaDispositionNotFallback pins the Windows delete
// path: the lock handle must carry DELETE access so the disposition call
// succeeds and the TOCTOU-free branch is the one actually taken. A fallback
// means the handle was opened without DELETE and the cleanup-vs-saver window
// is silently back.
func TestRemoveAndUnlockDeletesViaDispositionNotFallback(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "session.jsonl.lock")
	before := sessionLockDispositionFallbacks.Load()
	lock, err := tryTakeSessionLockFile(lockPath)
	if err != nil {
		t.Fatalf("tryTakeSessionLockFile: %v", err)
	}
	if err := lock.RemoveAndUnlock(); err != nil {
		t.Fatalf("RemoveAndUnlock: %v", err)
	}
	if got := sessionLockDispositionFallbacks.Load(); got != before {
		t.Fatalf("delete disposition fell back to path removal (%d -> %d); lock handle lacks DELETE access", before, got)
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("lock file still present after RemoveAndUnlock (err=%v)", err)
	}
}

// TestTryTakeSessionLockFileTreatsOpenHandleAsHeld pins the sharing-violation
// mapping: a plain Go open (no DELETE sharing) must read as "held", not as an
// error, because reconcile treats held lock files as live and skips them.
func TestTryTakeSessionLockFileTreatsOpenHandleAsHeld(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "session.jsonl.lock")
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := tryTakeSessionLockFile(lockPath); !errors.Is(err, errSessionFileLockHeld) {
		t.Fatalf("tryTakeSessionLockFile with plain open handle = %v, want errSessionFileLockHeld", err)
	}
}
