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

func TestSessionLeaseConvertsToRemovalGuardWithoutOwnershipGap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	lease, err := TryAcquireSessionLease(path)
	if err != nil {
		t.Fatalf("TryAcquireSessionLease: %v", err)
	}
	guard, err := lease.TryConvertToRemovalGuard()
	if err != nil {
		lease.Release()
		t.Fatalf("TryConvertToRemovalGuard: %v", err)
	}
	defer guard.Release()

	// Conversion consumes the original lease but retains its exact lease lock,
	// so no competing runtime can acquire in the handoff window.
	lease.Release()
	if next, err := TryAcquireSessionLease(path); !errors.Is(err, ErrSessionLeaseHeld) {
		if next != nil {
			next.Release()
		}
		t.Fatalf("TryAcquireSessionLease during converted guard err = %v, want ErrSessionLeaseHeld", err)
	}
	if err := guard.RemoveSidecarsAndRelease(); err != nil {
		t.Fatalf("RemoveSidecarsAndRelease: %v", err)
	}
	if next, err := TryAcquireSessionLease(path); err != nil {
		t.Fatalf("TryAcquireSessionLease after guard release: %v", err)
	} else {
		next.Release()
	}
}

func TestConvertedRemovalGuardRestoresSessionLease(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	lease, err := TryAcquireSessionLease(path)
	if err != nil {
		t.Fatalf("TryAcquireSessionLease: %v", err)
	}
	guard, err := lease.TryConvertToRemovalGuard()
	if err != nil {
		lease.Release()
		t.Fatalf("TryConvertToRemovalGuard: %v", err)
	}
	restored, err := guard.RestoreSessionLease()
	if err != nil {
		guard.Release()
		t.Fatalf("RestoreSessionLease: %v", err)
	}
	defer restored.Release()
	if !SessionLeaseHeldByCurrentRuntime(path) {
		t.Fatal("restored lease is not registered as current runtime ownership")
	}
	if next, err := TryAcquireSessionLease(path); !errors.Is(err, ErrSessionLeaseHeld) {
		if next != nil {
			next.Release()
		}
		t.Fatalf("TryAcquireSessionLease during restored lease err = %v, want ErrSessionLeaseHeld", err)
	}
	restored.Release()
	if next, err := TryAcquireSessionLease(path); err != nil {
		t.Fatalf("TryAcquireSessionLease after restored lease release: %v", err)
	} else {
		next.Release()
	}
}

func TestSessionLeaseConversionFailureKeepsLeaseActive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	lease, err := TryAcquireSessionLease(path)
	if err != nil {
		t.Fatalf("TryAcquireSessionLease: %v", err)
	}
	defer lease.Release()
	saveLock, err := tryTakeSessionLockFile(store.SessionLockFile(path))
	if err != nil {
		t.Fatalf("take save lock: %v", err)
	}
	defer saveLock.Unlock()

	if guard, err := lease.TryConvertToRemovalGuard(); !errors.Is(err, ErrSessionLeaseHeld) {
		if guard != nil {
			guard.Release()
		}
		t.Fatalf("TryConvertToRemovalGuard under save lock err = %v, want ErrSessionLeaseHeld", err)
	}
	if !SessionLeaseHeldByCurrentRuntime(path) {
		t.Fatal("failed conversion revoked the original runtime lease")
	}
	if next, err := TryAcquireSessionLease(path); !errors.Is(err, ErrSessionLeaseHeld) {
		if next != nil {
			next.Release()
		}
		t.Fatalf("competing lease after failed conversion err = %v, want ErrSessionLeaseHeld", err)
	}
}
