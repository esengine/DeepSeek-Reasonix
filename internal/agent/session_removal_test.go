package agent

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/provider"
	"reasonix/internal/store"
)

func TestSessionRemovalGuardBlocksWhileLeaseHeld(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	s := NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "work"})
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot: %v", err)
	}

	lease, err := TryAcquireSessionLease(path)
	if err != nil {
		t.Fatalf("TryAcquireSessionLease: %v", err)
	}
	defer lease.Release()

	if _, err := TryAcquireSessionRemovalGuard(path); !errors.Is(err, ErrSessionLeaseHeld) {
		t.Fatalf("guard under live lease err = %v, want ErrSessionLeaseHeld", err)
	}
	if _, err := os.Stat(store.SessionLeaseLock(path)); err != nil {
		t.Fatalf("lease lock disturbed by failed guard: %v", err)
	}

	lease.Release()
	guard, err := TryAcquireSessionRemovalGuard(path)
	if err != nil {
		t.Fatalf("guard after release: %v", err)
	}
	// While the guard holds both locks, no new lease can be acquired: this is
	// the window that used to allow probe-then-delete races.
	if _, err := TryAcquireSessionLease(path); !errors.Is(err, ErrSessionLeaseHeld) {
		guard.Release()
		t.Fatalf("lease acquired while removal guard held, err = %v", err)
	}
	if err := guard.RemoveSidecarsAndRelease(); err != nil {
		t.Fatalf("RemoveSidecarsAndRelease: %v", err)
	}
	for _, p := range []string{
		store.SessionLockFile(path),
		store.SessionLeaseLock(path),
		store.SessionLeaseInfo(path),
	} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("sidecar survived removal: %s (err=%v)", p, err)
		}
	}
	// The path is free again for a normal acquire afterwards.
	after, err := TryAcquireSessionLease(path)
	if err != nil {
		t.Fatalf("lease after removal: %v", err)
	}
	after.Release()
}

func TestSessionRemovalGuardReleaseKeepsSidecars(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	guard, err := TryAcquireSessionRemovalGuard(path)
	if err != nil {
		t.Fatalf("guard: %v", err)
	}
	guard.Release()
	// Abort path: locks released, files left in place for the next owner.
	if _, err := os.Stat(store.SessionLeaseLock(path)); err != nil {
		t.Fatalf("lease lock missing after abort: %v", err)
	}
	lease, err := TryAcquireSessionLease(path)
	if err != nil {
		t.Fatalf("lease after abort: %v", err)
	}
	lease.Release()
}
