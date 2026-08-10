package filelock

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestAcquireHonorsDeadlineAndRecoversAfterRelease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.lock")
	release, err := Acquire(context.Background(), path)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := Acquire(ctx, path); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("contended acquire error = %v, want deadline exceeded", err)
	}

	release()
	secondRelease, err := Acquire(context.Background(), path)
	if err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
	secondRelease()
}

func TestAcquireWithExternalTimeoutBoundsOnlyFileLockRetries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.lock")
	releaseExternal, err := tryLockFile(path)
	if err != nil {
		t.Fatalf("hold external file lock: %v", err)
	}
	defer releaseExternal()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	started := time.Now()
	_, err = AcquireWithExternalTimeout(ctx, path, 60*time.Millisecond)
	elapsed := time.Since(started)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("external acquire error = %v, want deadline exceeded", err)
	}
	if elapsed >= time.Second {
		t.Fatalf("external acquire waited %v, want the short external budget", elapsed)
	}
}

func TestAcquireWithExternalTimeoutRejectsInvalidBudget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.lock")
	if _, err := AcquireWithExternalTimeout(context.Background(), path, 0); err == nil {
		t.Fatal("zero external timeout should be rejected")
	}
}

func TestLocalRegistryReclaimsReleasedEntries(t *testing.T) {
	before := RegistrySizeForTest()
	path := filepath.Join(t.TempDir(), "ephemeral.lock")
	release, err := Acquire(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if RegistrySizeForTest() <= before {
		t.Fatal("registry should grow while lock is held")
	}
	release()
	if got := RegistrySizeForTest(); got != before {
		t.Fatalf("registry size after release = %d, want %d (reclaimed)", got, before)
	}

	// Re-acquire still works after reclaim.
	release2, err := Acquire(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	release2()
	if got := RegistrySizeForTest(); got != before {
		t.Fatalf("registry size after second cycle = %d, want %d", got, before)
	}
}
