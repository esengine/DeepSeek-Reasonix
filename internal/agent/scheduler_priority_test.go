package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestSchedulerQueuedWholeWriterBlocksLaterDirectoryWriter(t *testing.T) {
	s := NewSubagentScheduler(4, 3)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	dir, err := NormalizeWritePaths(root, []string{"src/"})
	if err != nil {
		t.Fatal(err)
	}
	whole, err := WholeWorkspaceWriteClaim(root)
	if err != nil {
		t.Fatal(err)
	}
	releaseDir, err := s.Acquire(context.Background(), AcquireRequest{Writer: true, WritePaths: dir})
	if err != nil {
		t.Fatal(err)
	}

	type result struct {
		release func()
		err     error
	}
	wholeResult := make(chan result, 1)
	go func() {
		release, acquireErr := s.Acquire(context.Background(), AcquireRequest{Writer: true, WritePaths: whole})
		wholeResult <- result{release: release, err: acquireErr}
	}()
	waitForSchedulerWaiters(t, s, 1)

	lateRelease, err := s.Acquire(context.Background(), AcquireRequest{
		Writer: true, WritePaths: dir, Nested: true,
	})
	if err == nil {
		lateRelease()
		releaseDir()
		t.Fatal("later directory writer bypassed the queued whole-workspace writer")
	}
	releaseDir()

	acquired := <-wholeResult
	if acquired.err != nil {
		t.Fatal(acquired.err)
	}
	if lateRelease, err = s.Acquire(context.Background(), AcquireRequest{
		Writer: true, WritePaths: dir, Nested: true,
	}); err == nil {
		lateRelease()
		acquired.release()
		t.Fatal("directory writer started while the queued whole-workspace writer was active")
	}
	acquired.release()

	lateRelease, err = s.Acquire(context.Background(), AcquireRequest{
		Writer: true, WritePaths: dir, Nested: true,
	})
	if err != nil {
		t.Fatalf("directory writer after whole-workspace release: %v", err)
	}
	lateRelease()
}

func TestSchedulerQueuedWholeWriterStaysAheadWhenPumpRuns(t *testing.T) {
	s := NewSubagentScheduler(5, 4)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	dir, err := NormalizeWritePaths(root, []string{"src/"})
	if err != nil {
		t.Fatal(err)
	}
	file, err := NormalizeWritePaths(root, []string{"src/first.go"})
	if err != nil {
		t.Fatal(err)
	}
	whole, err := WholeWorkspaceWriteClaim(root)
	if err != nil {
		t.Fatal(err)
	}
	releaseDir, dirID, err := s.AcquireWithID(context.Background(), AcquireRequest{Writer: true, WritePaths: dir})
	if err != nil {
		t.Fatal(err)
	}

	type result struct {
		release func()
		err     error
	}
	wholeResult := make(chan result, 1)
	go func() {
		release, acquireErr := s.Acquire(context.Background(), AcquireRequest{Writer: true, WritePaths: whole})
		wholeResult <- result{release: release, err: acquireErr}
	}()
	waitForSchedulerWaiters(t, s, 1)

	lateResult := make(chan result, 1)
	go func() {
		release, acquireErr := s.Acquire(context.Background(), AcquireRequest{Writer: true, WritePaths: dir})
		lateResult <- result{release: release, err: acquireErr}
	}()
	waitForSchedulerWaiters(t, s, 2)

	readRelease, err := s.Acquire(context.Background(), AcquireRequest{Nested: true})
	if err != nil {
		t.Fatalf("read-only work should remain concurrent with a queued workspace writer: %v", err)
	}
	readRelease()
	if err := s.Realize(dirID, file); err != nil {
		t.Fatal(err)
	}
	select {
	case late := <-lateResult:
		late.release()
		releaseDir()
		acquiredWhole := <-wholeResult
		if acquiredWhole.err == nil {
			acquiredWhole.release()
		}
		t.Fatal("queued directory writer bypassed an earlier whole-workspace writer during pump")
	default:
	}

	releaseDir()
	acquiredWhole := <-wholeResult
	if acquiredWhole.err != nil {
		t.Fatal(acquiredWhole.err)
	}
	select {
	case late := <-lateResult:
		late.release()
		acquiredWhole.release()
		t.Fatal("queued directory writer started while the whole-workspace writer was active")
	default:
	}
	acquiredWhole.release()
	late := <-lateResult
	if late.err != nil {
		t.Fatal(late.err)
	}
	late.release()
}

func TestSchedulerCancelQueuedWholeWriterAdmitsLaterWriter(t *testing.T) {
	s := NewSubagentScheduler(4, 3)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	dir, err := NormalizeWritePaths(root, []string{"src/"})
	if err != nil {
		t.Fatal(err)
	}
	whole, err := WholeWorkspaceWriteClaim(root)
	if err != nil {
		t.Fatal(err)
	}
	releaseDir, err := s.Acquire(context.Background(), AcquireRequest{Writer: true, WritePaths: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer releaseDir()

	wholeCtx, cancelWhole := context.WithCancel(context.Background())
	wholeResult := make(chan error, 1)
	go func() {
		_, acquireErr := s.Acquire(wholeCtx, AcquireRequest{Writer: true, WritePaths: whole})
		wholeResult <- acquireErr
	}()
	waitForSchedulerWaiters(t, s, 1)

	type result struct {
		release func()
		err     error
	}
	lateResult := make(chan result, 1)
	go func() {
		release, acquireErr := s.Acquire(context.Background(), AcquireRequest{Writer: true, WritePaths: dir})
		lateResult <- result{release: release, err: acquireErr}
	}()
	waitForSchedulerWaiters(t, s, 2)

	cancelWhole()
	if err := <-wholeResult; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled whole-workspace waiter: %v", err)
	}
	select {
	case late := <-lateResult:
		if late.err != nil {
			t.Fatal(late.err)
		}
		late.release()
	case <-time.After(2 * time.Second):
		t.Fatal("later writer stayed queued after the whole-workspace waiter was cancelled")
	}
}

func waitForSchedulerWaiters(t *testing.T, s *SubagentScheduler, want int) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		s.mu.Lock()
		got := len(s.waiters)
		s.mu.Unlock()
		if got >= want {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("scheduler waiters = %d, want at least %d", got, want)
		default:
			runtime.Gosched()
		}
	}
}
