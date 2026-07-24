package agent

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"reasonix/internal/store"
)

// leaseTestPath returns a session path in "user shape" — mixed case, exactly
// as desktop/CLI callers pass it — plus its canonical registry key for
// internal-state setup and assertions. Tests must feed the user shape to the
// API under test: feeding pre-canonicalized paths is how the Windows
// case-fold mismatch (#5999) escaped this suite. On non-Windows hosts the two
// forms are identical; on Windows they differ and exercise the fold.
func leaseTestPath(t *testing.T) (userPath, key string) {
	t.Helper()
	userPath = filepath.Join(t.TempDir(), "Sessions-Dir", "Session-Test.jsonl")
	if err := os.MkdirAll(filepath.Dir(userPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	return userPath, canonicalSessionSavePath(userPath)
}

func TestSessionLeaseRejectsConcurrentWriterAndReleases(t *testing.T) {
	userPath, _ := leaseTestPath(t)
	first, err := TryAcquireSessionLease(userPath)
	if err != nil {
		t.Fatalf("first TryAcquireSessionLease: %v", err)
	}
	if first.Path() == "" {
		t.Fatal("first lease path is empty")
	}
	info, err := LoadSessionLeaseInfo(userPath)
	if err != nil {
		t.Fatalf("LoadSessionLeaseInfo: %v", err)
	}
	if info.WriterID == "" || info.PID == 0 || info.SessionPath == "" {
		t.Fatalf("lease info = %+v, want writer metadata", info)
	}

	second, err := TryAcquireSessionLease(userPath)
	if !errors.Is(err, ErrSessionLeaseHeld) {
		t.Fatalf("second TryAcquireSessionLease err = %v, want ErrSessionLeaseHeld", err)
	}
	if second != nil {
		second.Release()
		t.Fatal("second lease unexpectedly acquired")
	}

	first.Release()
	third, err := TryAcquireSessionLease(userPath)
	if err != nil {
		t.Fatalf("third TryAcquireSessionLease after release: %v", err)
	}
	third.Release()
}

func TestSessionLeaseReclaimsCurrentProcessStaleOwner(t *testing.T) {
	userPath, key := leaseTestPath(t)
	sessionLeaseOwners.Store(key, struct{}{})
	t.Cleanup(func() {
		sessionLeaseOwners.Delete(key)
		_ = os.Remove(sessionLeaseInfoPath(key))
	})
	if err := SaveSessionLeaseInfo(key, SessionLeaseInfo{
		SessionPath: key,
		WriterID:    SessionWriterID(),
		PID:         os.Getpid(),
		AcquiredAt:  time.Now().UTC(),
	}); err != nil {
		t.Fatalf("SaveSessionLeaseInfo: %v", err)
	}
	if lease, err := TryAcquireSessionLease(userPath); !errors.Is(err, ErrSessionLeaseHeld) {
		if lease != nil {
			lease.Release()
		}
		t.Fatalf("TryAcquireSessionLease err = %v, want ErrSessionLeaseHeld", err)
	}
	lease, err := TryReclaimCurrentProcessSessionLease(userPath)
	if err != nil {
		t.Fatalf("TryReclaimCurrentProcessSessionLease: %v", err)
	}
	lease.Release()
}

func TestSessionLeaseReclaimsOrphanedEntryWithoutInfo(t *testing.T) {
	// An orphaned in-process entry whose lease.json was deleted out from
	// under it (manual cleanup, AV quarantine). Nothing actually holds the
	// session — the OS lock is free — so reclaim must recover instead of
	// wedging every rebuild as busy. Before the lock-arbiter rework this
	// deadlocked: reclaim fell back to a plain acquire, which re-hit the
	// orphaned map entry forever.
	userPath, key := leaseTestPath(t)
	sessionLeaseOwners.Store(key, uint64(1<<61))
	t.Cleanup(func() { sessionLeaseOwners.Delete(key) })

	if lease, err := TryAcquireSessionLease(userPath); !errors.Is(err, ErrSessionLeaseHeld) {
		if lease != nil {
			lease.Release()
		}
		t.Fatalf("TryAcquireSessionLease err = %v, want ErrSessionLeaseHeld", err)
	}
	lease, err := TryReclaimCurrentProcessSessionLease(userPath)
	if err != nil {
		t.Fatalf("TryReclaimCurrentProcessSessionLease without info: %v", err)
	}
	if _, err := LoadSessionLeaseInfo(userPath); err != nil {
		t.Fatalf("reclaim should have rewritten lease info, load err = %v", err)
	}
	lease.Release()
}

func TestSessionLeaseReclaimsOrphanedEntryWithCorruptInfo(t *testing.T) {
	// Same as above but the sidecar is torn (empty/undecodable) rather than
	// missing: identity is unreadable, the lock is free, reclaim must win.
	userPath, key := leaseTestPath(t)
	sessionLeaseOwners.Store(key, uint64(1<<61))
	t.Cleanup(func() {
		sessionLeaseOwners.Delete(key)
		_ = os.Remove(sessionLeaseInfoPath(key))
	})
	if err := os.WriteFile(sessionLeaseInfoPath(key), []byte("{torn"), 0o644); err != nil {
		t.Fatalf("write corrupt lease info: %v", err)
	}

	lease, err := TryReclaimCurrentProcessSessionLease(userPath)
	if err != nil {
		t.Fatalf("TryReclaimCurrentProcessSessionLease with corrupt info: %v", err)
	}
	lease.Release()
}

func TestSessionLeaseReclaimRefusesForeignInfo(t *testing.T) {
	// A readable info naming another runtime is never stolen by reclaim,
	// even with the lock free — that separation belongs to
	// SessionLeaseHeldByOtherRuntime's cleanup, not to reclaim.
	userPath, key := leaseTestPath(t)
	if err := SaveSessionLeaseInfo(key, SessionLeaseInfo{
		SessionPath: key,
		WriterID:    "other-host-1234-deadbeef",
		PID:         os.Getpid() + 1,
		AcquiredAt:  time.Now().UTC(),
	}); err != nil {
		t.Fatalf("SaveSessionLeaseInfo: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(sessionLeaseInfoPath(key)) })

	if lease, err := TryReclaimCurrentProcessSessionLease(userPath); !errors.Is(err, ErrSessionLeaseHeld) {
		if lease != nil {
			lease.Release()
		}
		t.Fatalf("TryReclaimCurrentProcessSessionLease err = %v, want ErrSessionLeaseHeld", err)
	}
}

func TestSessionLeaseConcurrentReclaimSingleWinner(t *testing.T) {
	userPath, key := leaseTestPath(t)
	sessionLeaseOwners.Store(key, struct{}{})
	t.Cleanup(func() {
		sessionLeaseOwners.Delete(key)
		_ = os.Remove(sessionLeaseInfoPath(key))
	})
	if err := SaveSessionLeaseInfo(key, SessionLeaseInfo{
		SessionPath: key,
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
			if lease, err := TryReclaimCurrentProcessSessionLease(userPath); err == nil && lease != nil {
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
	if _, ok := sessionLeaseOwners.Load(key); !ok {
		t.Fatal("winner's owner entry was evicted by a failed concurrent reclaim")
	}
	if lease, err := TryAcquireSessionLease(userPath); !errors.Is(err, ErrSessionLeaseHeld) {
		if lease != nil {
			lease.Release()
		}
		t.Fatalf("TryAcquireSessionLease while reclaimed lease is held err = %v, want ErrSessionLeaseHeld", err)
	}
	won[0].Release()
	lease, err := TryAcquireSessionLease(userPath)
	if err != nil {
		t.Fatalf("TryAcquireSessionLease after release: %v", err)
	}
	lease.Release()
}

func TestSessionLeaseReclaimRefusesActiveHolder(t *testing.T) {
	userPath, key := leaseTestPath(t)
	holder, err := TryAcquireSessionLease(userPath)
	if err != nil {
		t.Fatalf("TryAcquireSessionLease: %v", err)
	}
	defer holder.Release()

	if lease, err := TryReclaimCurrentProcessSessionLease(userPath); !errors.Is(err, ErrSessionLeaseHeld) {
		if lease != nil {
			lease.Release()
		}
		t.Fatalf("TryReclaimCurrentProcessSessionLease err = %v, want ErrSessionLeaseHeld", err)
	}
	// The failed reclaim must leave the holder's owner entry intact.
	if _, ok := sessionLeaseOwners.Load(key); !ok {
		t.Fatal("active holder's owner entry was evicted by a failed reclaim")
	}
	if lease, err := TryAcquireSessionLease(userPath); !errors.Is(err, ErrSessionLeaseHeld) {
		if lease != nil {
			lease.Release()
		}
		t.Fatalf("TryAcquireSessionLease err = %v, want ErrSessionLeaseHeld", err)
	}
}

func TestSessionLeaseReclaimAfterHolderReleased(t *testing.T) {
	userPath, _ := leaseTestPath(t)
	holder, err := TryAcquireSessionLease(userPath)
	if err != nil {
		t.Fatalf("TryAcquireSessionLease: %v", err)
	}
	holder.Release()

	// The holder released between the caller's failed acquire and the
	// reclaim: the lease info file is gone and the lock is free, so the
	// reclaim must win the lease cleanly.
	lease, err := TryReclaimCurrentProcessSessionLease(userPath)
	if err != nil {
		t.Fatalf("TryReclaimCurrentProcessSessionLease after release: %v", err)
	}
	lease.Release()
}

func TestSessionLeaseStaleReleaseKeepsNewOwnerEntry(t *testing.T) {
	userPath, key := leaseTestPath(t)
	stale, err := TryAcquireSessionLease(userPath)
	if err != nil {
		t.Fatalf("TryAcquireSessionLease: %v", err)
	}
	// Simulate a reclaim that took over the entry while the stale lease was
	// still alive: the map now names a different owner.
	sessionLeaseOwners.Store(key, uint64(1<<62))
	t.Cleanup(func() { sessionLeaseOwners.Delete(key) })

	stale.Release()
	if _, ok := sessionLeaseOwners.Load(key); !ok {
		t.Fatal("stale Release evicted the new owner's entry")
	}
}

func TestSessionLeaseHeldByOtherRuntime(t *testing.T) {
	t.Run("no lease", func(t *testing.T) {
		userPath, _ := leaseTestPath(t)
		if SessionLeaseHeldByOtherRuntime(userPath) {
			t.Fatal("unheld session reported as held by another runtime")
		}
	})
	t.Run("held by this process", func(t *testing.T) {
		userPath, _ := leaseTestPath(t)
		lease, err := TryAcquireSessionLease(userPath)
		if err != nil {
			t.Fatalf("TryAcquireSessionLease: %v", err)
		}
		defer lease.Release()
		if SessionLeaseHeldByOtherRuntime(userPath) {
			t.Fatal("own lease reported as held by another runtime")
		}
	})
	t.Run("foreign info with live lock", func(t *testing.T) {
		userPath, key := leaseTestPath(t)
		unlock, err := tryLockSessionLeaseFile(key)
		if err != nil {
			t.Fatalf("tryLockSessionLeaseFile: %v", err)
		}
		defer unlock()
		if err := SaveSessionLeaseInfo(key, SessionLeaseInfo{
			SessionPath: key,
			WriterID:    "other-host-1234-deadbeef",
			PID:         os.Getpid() + 1,
			AcquiredAt:  time.Now().UTC(),
		}); err != nil {
			t.Fatalf("SaveSessionLeaseInfo: %v", err)
		}
		t.Cleanup(func() { _ = os.Remove(sessionLeaseInfoPath(key)) })
		if !SessionLeaseHeldByOtherRuntime(userPath) {
			t.Fatal("foreign-held session not reported as held by another runtime")
		}
	})
	t.Run("foreign info from crashed process", func(t *testing.T) {
		userPath, key := leaseTestPath(t)
		if err := SaveSessionLeaseInfo(key, SessionLeaseInfo{
			SessionPath: key,
			WriterID:    "other-host-1234-deadbeef",
			PID:         os.Getpid() + 1,
			AcquiredAt:  time.Now().UTC(),
		}); err != nil {
			t.Fatalf("SaveSessionLeaseInfo: %v", err)
		}
		t.Cleanup(func() { _ = os.Remove(sessionLeaseInfoPath(key)) })
		// Info file left behind but the lock is free: the holder crashed, so
		// the session is not considered held.
		if SessionLeaseHeldByOtherRuntime(userPath) {
			t.Fatal("crashed holder's leftover info reported as held")
		}
		if _, err := os.Stat(sessionLeaseInfoPath(key)); !os.IsNotExist(err) {
			t.Fatalf("crashed holder's leftover info should be removed, stat err = %v", err)
		}
	})
	t.Run("corrupt info from crashed process", func(t *testing.T) {
		userPath, key := leaseTestPath(t)
		if err := os.WriteFile(sessionLeaseInfoPath(key), nil, 0o644); err != nil {
			t.Fatalf("write corrupt lease info: %v", err)
		}
		if SessionLeaseHeldByOtherRuntime(userPath) {
			t.Fatal("corrupt crashed holder info reported as held")
		}
		if _, err := os.Stat(sessionLeaseInfoPath(key)); !os.IsNotExist(err) {
			t.Fatalf("corrupt lease info should be removed, stat err = %v", err)
		}
	})
}

func TestSessionLeaseHeldByCurrentRuntime(t *testing.T) {
	userPath, _ := leaseTestPath(t)
	if SessionLeaseHeldByCurrentRuntime(userPath) {
		t.Fatal("unheld session reported as owned by the current runtime")
	}
	lease, err := TryAcquireSessionLease(userPath)
	if err != nil {
		t.Fatalf("TryAcquireSessionLease: %v", err)
	}
	if !SessionLeaseHeldByCurrentRuntime(userPath) {
		lease.Release()
		t.Fatal("held session was not reported as owned by the current runtime")
	}
	lease.Release()
	if SessionLeaseHeldByCurrentRuntime(userPath) {
		t.Fatal("released session remained owned by the current runtime")
	}
}

func TestSessionLeaseHeldByCurrentRuntimeRejectsPendingReservation(t *testing.T) {
	userPath, key := leaseTestPath(t)
	ownerID := sessionLeaseSeq.Add(1)
	sessionLeaseOwners.Store(key, ownerID)
	t.Cleanup(func() {
		sessionLeaseOwners.CompareAndDelete(key, ownerID)
		sessionLeaseActiveOwners.CompareAndDelete(key, ownerID)
	})

	if SessionLeaseHeldByCurrentRuntime(userPath) {
		t.Fatal("pending acquisition reservation authorized ownership-sensitive repair")
	}
}

func TestSessionLeaseReleaseRevokesRepairAuthorizationBeforeUnlock(t *testing.T) {
	userPath, _ := leaseTestPath(t)
	lease, err := TryAcquireSessionLease(userPath)
	if err != nil {
		t.Fatalf("TryAcquireSessionLease: %v", err)
	}
	unlock := lease.unlock
	checked := false
	lease.unlock = func() {
		checked = true
		if SessionLeaseHeldByCurrentRuntime(userPath) {
			t.Error("release kept repair authorization active while unlocking the OS lease")
		}
		unlock()
	}

	lease.Release()
	if !checked {
		t.Fatal("release did not invoke the controlled unlock")
	}
}

func TestSessionLeaseReleaseRetiresLockSidecars(t *testing.T) {
	userPath, key := leaseTestPath(t)
	lease, err := TryAcquireSessionLease(userPath)
	if err != nil {
		t.Fatalf("TryAcquireSessionLease: %v", err)
	}
	leaseLock := store.SessionLeaseLock(key)
	if _, err := os.Stat(leaseLock); err != nil {
		t.Fatalf("lease lock should exist while held: %v", err)
	}
	lease.Release()
	if _, err := os.Stat(leaseLock); !os.IsNotExist(err) {
		t.Fatalf("lease lock should be retired on release, stat err = %v", err)
	}
	if _, err := os.Stat(store.SessionLockFile(key)); !os.IsNotExist(err) {
		t.Fatalf("save lock should be retired on release, stat err = %v", err)
	}

	// A release racing a live successor must not strip the successor's lock.
	first, err := TryAcquireSessionLease(userPath)
	if err != nil {
		t.Fatalf("reacquire: %v", err)
	}
	second, err := TryAcquireSessionLease(userPath)
	if !errors.Is(err, ErrSessionLeaseHeld) {
		if second != nil {
			second.Release()
		}
		t.Fatalf("second acquire err = %v, want ErrSessionLeaseHeld", err)
	}
	if _, err := os.Stat(leaseLock); err != nil {
		t.Fatalf("holder's lease lock must survive a failed acquire: %v", err)
	}
	first.Release()
}
