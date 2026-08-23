package filelock

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestTryAcquireModeSharedIsNonBlocking(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.lock")
	first, err := TryAcquireMode(path, ModeShared)
	if err != nil {
		t.Fatal(err)
	}
	second, err := TryAcquireMode(path, ModeShared)
	if err != nil {
		first()
		t.Fatal(err)
	}
	first()
	second()
}

func TestWaitingWriterBlocksNewLocalReaders(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.lock")
	reader, err := AcquireMode(context.Background(), path, ModeShared)
	if err != nil {
		t.Fatal(err)
	}
	writerAcquired := make(chan func(), 1)
	go func() {
		release, acquireErr := Acquire(context.Background(), path)
		if acquireErr == nil {
			writerAcquired <- release
		}
	}()
	deadline := time.After(2 * time.Second)
	for {
		release, tryErr := TryAcquireMode(path, ModeShared)
		if errors.Is(tryErr, ErrHeld) {
			break
		}
		if tryErr != nil {
			t.Fatal(tryErr)
		}
		release()
		select {
		case <-deadline:
			t.Fatal("new readers continued to bypass the waiting writer")
		default:
		}
	}
	reader()
	select {
	case release := <-writerAcquired:
		release()
	case <-time.After(2 * time.Second):
		t.Fatal("waiting writer did not acquire after reader release")
	}
}

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

func TestAcquireSharedAllowsConcurrentReaders(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.lock")
	first, err := AcquireMode(context.Background(), path, ModeShared)
	if err != nil {
		t.Fatal(err)
	}
	second, err := AcquireMode(context.Background(), path, ModeShared)
	if err != nil {
		first()
		t.Fatal(err)
	}
	first()
	second()
}

func TestAcquireSharedConflictsWithExclusive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.lock")
	shared, err := AcquireMode(context.Background(), path, ModeShared)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := Acquire(ctx, path); !errors.Is(err, context.DeadlineExceeded) {
		shared()
		t.Fatalf("exclusive vs shared error = %v, want deadline exceeded", err)
	}
	shared()
	exclusive, err := Acquire(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	exclusive()
}

func TestAcquireZeroValueRemainsExclusive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.lock")
	first, err := AcquireMode(context.Background(), path, ModeExclusive)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := Acquire(ctx, path); !errors.Is(err, context.DeadlineExceeded) {
		first()
		t.Fatalf("zero-value exclusive error = %v", err)
	}
	first()
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
