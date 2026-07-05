package agent

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestSessionLeaseRejectsConcurrentWriterAndReleases(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	first, err := TryAcquireSessionLease(path)
	if err != nil {
		t.Fatalf("first TryAcquireSessionLease: %v", err)
	}
	if first.Path() == "" {
		t.Fatal("first lease path is empty")
	}
	info, err := LoadSessionLeaseInfo(path)
	if err != nil {
		t.Fatalf("LoadSessionLeaseInfo: %v", err)
	}
	if info.WriterID == "" || info.PID == 0 || info.SessionPath == "" {
		t.Fatalf("lease info = %+v, want writer metadata", info)
	}

	second, err := TryAcquireSessionLease(path)
	if !errors.Is(err, ErrSessionLeaseHeld) {
		t.Fatalf("second TryAcquireSessionLease err = %v, want ErrSessionLeaseHeld", err)
	}
	if second != nil {
		second.Release()
		t.Fatal("second lease unexpectedly acquired")
	}

	first.Release()
	third, err := TryAcquireSessionLease(path)
	if err != nil {
		t.Fatalf("third TryAcquireSessionLease after release: %v", err)
	}
	third.Release()
}

func TestSessionLeaseReclaimsCurrentProcessStaleOwner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	path = canonicalSessionSavePath(path)
	sessionLeaseOwners.Store(path, struct{}{})
	t.Cleanup(func() {
		sessionLeaseOwners.Delete(path)
		_ = os.Remove(sessionLeaseInfoPath(path))
	})
	if err := SaveSessionLeaseInfo(path, SessionLeaseInfo{
		SessionPath: path,
		WriterID:    SessionWriterID(),
		PID:         os.Getpid(),
		AcquiredAt:  time.Now().UTC(),
	}); err != nil {
		t.Fatalf("SaveSessionLeaseInfo: %v", err)
	}
	if lease, err := TryAcquireSessionLease(path); !errors.Is(err, ErrSessionLeaseHeld) {
		if lease != nil {
			lease.Release()
		}
		t.Fatalf("TryAcquireSessionLease err = %v, want ErrSessionLeaseHeld", err)
	}
	lease, err := TryReclaimCurrentProcessSessionLease(path)
	if err != nil {
		t.Fatalf("TryReclaimCurrentProcessSessionLease: %v", err)
	}
	lease.Release()
}

func TestSessionLeaseConcurrentReclaimSingleWinner(t *testing.T) {
	path := canonicalSessionSavePath(filepath.Join(t.TempDir(), "session.jsonl"))
	sessionLeaseOwners.Store(path, struct{}{})
	t.Cleanup(func() {
		sessionLeaseOwners.Delete(path)
		_ = os.Remove(sessionLeaseInfoPath(path))
	})
	if err := SaveSessionLeaseInfo(path, SessionLeaseInfo{
		SessionPath: path,
		WriterID:    SessionWriterID(),
		PID:         os.Getpid(),
		AcquiredAt:  time.Now().UTC(),
	}); err != nil {
		t.Fatalf("SaveSessionLeaseInfo: %v", err)
	}

	const attempts = 16
	var wg sync.WaitGroup
	leases := make(chan *SessionLease, attempts)
	start := make(chan struct{})
	for range attempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if lease, err := TryReclaimCurrentProcessSessionLease(path); err == nil && lease != nil {
				leases <- lease
			}
		}()
	}
	close(start)
	wg.Wait()
	close(leases)

	var won []*SessionLease
	for lease := range leases {
		won = append(won, lease)
	}
	if len(won) != 1 {
		t.Fatalf("concurrent reclaim produced %d leases, want exactly 1", len(won))
	}
	// The losers must not have evicted the winner's owner entry.
	if _, ok := sessionLeaseOwners.Load(path); !ok {
		t.Fatal("winner's owner entry was evicted by a failed concurrent reclaim")
	}
	if lease, err := TryAcquireSessionLease(path); !errors.Is(err, ErrSessionLeaseHeld) {
		if lease != nil {
			lease.Release()
		}
		t.Fatalf("TryAcquireSessionLease while reclaimed lease is held err = %v, want ErrSessionLeaseHeld", err)
	}
	won[0].Release()
	lease, err := TryAcquireSessionLease(path)
	if err != nil {
		t.Fatalf("TryAcquireSessionLease after release: %v", err)
	}
	lease.Release()
}

func TestSessionLeaseReclaimRefusesActiveHolder(t *testing.T) {
	path := canonicalSessionSavePath(filepath.Join(t.TempDir(), "session.jsonl"))
	holder, err := TryAcquireSessionLease(path)
	if err != nil {
		t.Fatalf("TryAcquireSessionLease: %v", err)
	}
	defer holder.Release()

	if lease, err := TryReclaimCurrentProcessSessionLease(path); !errors.Is(err, ErrSessionLeaseHeld) {
		if lease != nil {
			lease.Release()
		}
		t.Fatalf("TryReclaimCurrentProcessSessionLease err = %v, want ErrSessionLeaseHeld", err)
	}
	// The failed reclaim must leave the holder's owner entry intact.
	if _, ok := sessionLeaseOwners.Load(path); !ok {
		t.Fatal("active holder's owner entry was evicted by a failed reclaim")
	}
	if lease, err := TryAcquireSessionLease(path); !errors.Is(err, ErrSessionLeaseHeld) {
		if lease != nil {
			lease.Release()
		}
		t.Fatalf("TryAcquireSessionLease err = %v, want ErrSessionLeaseHeld", err)
	}
}

func TestSessionLeaseReclaimAfterHolderReleased(t *testing.T) {
	path := canonicalSessionSavePath(filepath.Join(t.TempDir(), "session.jsonl"))
	holder, err := TryAcquireSessionLease(path)
	if err != nil {
		t.Fatalf("TryAcquireSessionLease: %v", err)
	}
	holder.Release()

	// The holder released between the caller's failed acquire and the
	// reclaim: the lease info file is gone and a plain acquire must win.
	lease, err := TryReclaimCurrentProcessSessionLease(path)
	if err != nil {
		t.Fatalf("TryReclaimCurrentProcessSessionLease after release: %v", err)
	}
	lease.Release()
}

func TestSessionLeaseStaleReleaseKeepsNewOwnerEntry(t *testing.T) {
	path := canonicalSessionSavePath(filepath.Join(t.TempDir(), "session.jsonl"))
	stale, err := TryAcquireSessionLease(path)
	if err != nil {
		t.Fatalf("TryAcquireSessionLease: %v", err)
	}
	// Simulate a reclaim that took over the entry while the stale lease was
	// still alive: the map now names a different owner.
	sessionLeaseOwners.Store(path, uint64(1<<62))
	t.Cleanup(func() { sessionLeaseOwners.Delete(path) })

	stale.Release()
	if _, ok := sessionLeaseOwners.Load(path); !ok {
		t.Fatal("stale Release evicted the new owner's entry")
	}
}

func TestSessionLeaseHeldByOtherRuntime(t *testing.T) {
	t.Run("no lease", func(t *testing.T) {
		path := canonicalSessionSavePath(filepath.Join(t.TempDir(), "session.jsonl"))
		if SessionLeaseHeldByOtherRuntime(path) {
			t.Fatal("unheld session reported as held by another runtime")
		}
	})
	t.Run("held by this process", func(t *testing.T) {
		path := canonicalSessionSavePath(filepath.Join(t.TempDir(), "session.jsonl"))
		lease, err := TryAcquireSessionLease(path)
		if err != nil {
			t.Fatalf("TryAcquireSessionLease: %v", err)
		}
		defer lease.Release()
		if SessionLeaseHeldByOtherRuntime(path) {
			t.Fatal("own lease reported as held by another runtime")
		}
	})
	t.Run("foreign info with live lock", func(t *testing.T) {
		path := canonicalSessionSavePath(filepath.Join(t.TempDir(), "session.jsonl"))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		unlock, err := tryLockSessionLeaseFile(path)
		if err != nil {
			t.Fatalf("tryLockSessionLeaseFile: %v", err)
		}
		defer unlock()
		if err := SaveSessionLeaseInfo(path, SessionLeaseInfo{
			SessionPath: path,
			WriterID:    "other-host-1234-deadbeef",
			PID:         os.Getpid() + 1,
			AcquiredAt:  time.Now().UTC(),
		}); err != nil {
			t.Fatalf("SaveSessionLeaseInfo: %v", err)
		}
		t.Cleanup(func() { _ = os.Remove(sessionLeaseInfoPath(path)) })
		if !SessionLeaseHeldByOtherRuntime(path) {
			t.Fatal("foreign-held session not reported as held by another runtime")
		}
	})
	t.Run("foreign info from crashed process", func(t *testing.T) {
		path := canonicalSessionSavePath(filepath.Join(t.TempDir(), "session.jsonl"))
		if err := SaveSessionLeaseInfo(path, SessionLeaseInfo{
			SessionPath: path,
			WriterID:    "other-host-1234-deadbeef",
			PID:         os.Getpid() + 1,
			AcquiredAt:  time.Now().UTC(),
		}); err != nil {
			t.Fatalf("SaveSessionLeaseInfo: %v", err)
		}
		t.Cleanup(func() { _ = os.Remove(sessionLeaseInfoPath(path)) })
		// Info file left behind but the lock is free: the holder crashed, so
		// the session is not considered held.
		if SessionLeaseHeldByOtherRuntime(path) {
			t.Fatal("crashed holder's leftover info reported as held")
		}
	})
}
