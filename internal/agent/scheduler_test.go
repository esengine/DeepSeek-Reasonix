package agent

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSchedulerTotalConcurrencyQueues(t *testing.T) {
	s := NewSubagentScheduler(2, 2)
	root := t.TempDir()
	var started atomic.Int32
	var max atomic.Int32
	var wg sync.WaitGroup
	barrier := make(chan struct{})

	for range 4 {
		wg.Go(func() {
			release, err := s.Acquire(context.Background(), AcquireRequest{Writer: false})
			if err != nil {
				t.Errorf("acquire: %v", err)
				return
			}
			cur := started.Add(1)
			for {
				old := max.Load()
				if cur <= old || max.CompareAndSwap(old, cur) {
					break
				}
			}
			<-barrier
			started.Add(-1)
			release()
		})
	}

	// Wait until at least 2 are running, then release them.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if max.Load() >= 2 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := max.Load(); got > 2 {
		t.Fatalf("max concurrent = %d, want <= 2", got)
	}
	close(barrier)
	wg.Wait()
	_ = root
}

func TestSchedulerNestedFailsFast(t *testing.T) {
	s := NewSubagentScheduler(1, 1)
	release, err := s.Acquire(context.Background(), AcquireRequest{Writer: false})
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	_, err = s.Acquire(context.Background(), AcquireRequest{Writer: false, Nested: true})
	if err == nil {
		t.Fatal("nested acquire should fail fast at limit")
	}
}

func TestSchedulerWriterPathConflictQueues(t *testing.T) {
	s := NewSubagentScheduler(4, 2)
	root := t.TempDir()
	claim, err := NormalizeWritePaths(root, []string{"a.md"})
	if err != nil {
		t.Fatal(err)
	}
	release, err := s.Acquire(context.Background(), AcquireRequest{Writer: true, WritePaths: claim})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	// Same path cannot start while the first claim is held — with Nested it fails.
	_, err = s.Acquire(ctx, AcquireRequest{Writer: true, WritePaths: claim, Nested: true})
	if err == nil {
		t.Fatal("expected path conflict for nested acquire")
	}
	release()

	// After release, same path is free.
	release2, err := s.Acquire(context.Background(), AcquireRequest{Writer: true, WritePaths: claim})
	if err != nil {
		t.Fatal(err)
	}
	release2()
}

func TestSchedulerDirectoryClaimsStartInParallel(t *testing.T) {
	s := NewSubagentScheduler(4, 2)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	claim, err := NormalizeWritePaths(root, []string{"src/"})
	if err != nil {
		t.Fatal(err)
	}
	release1, id1, err := s.AcquireWithID(context.Background(), AcquireRequest{Writer: true, WritePaths: claim})
	if err != nil {
		t.Fatal(err)
	}
	defer release1()
	release2, id2, err := s.AcquireWithID(context.Background(), AcquireRequest{Writer: true, WritePaths: claim, Nested: true})
	if err != nil {
		t.Fatalf("second directory claim must start: %v", err)
	}
	defer release2()
	if id1 == 0 || id2 == 0 || id1 == id2 {
		t.Fatalf("claim ids = %d, %d", id1, id2)
	}
}

func TestSchedulerWholeClaimCannotStartBehindUnrealizedDirectoryWriter(t *testing.T) {
	s := NewSubagentScheduler(4, 2)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	dir, err := NormalizeWritePaths(root, []string{"src/"})
	if err != nil {
		t.Fatal(err)
	}
	releaseDir, _, err := s.AcquireWithID(context.Background(), AcquireRequest{Writer: true, WritePaths: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer releaseDir()
	whole, err := WholeWorkspaceWriteClaim(root)
	if err != nil {
		t.Fatal(err)
	}
	releaseWhole, _, err := s.AcquireWithID(context.Background(), AcquireRequest{
		Writer: true, WritePaths: whole, Nested: true,
	})
	if err == nil {
		releaseWhole()
		t.Fatal("whole-workspace claim bypassed an active unrealized directory writer")
	}
	releaseDir()
	releaseWhole, _, err = s.AcquireWithID(context.Background(), AcquireRequest{
		Writer: true, WritePaths: whole, Nested: true,
	})
	if err != nil {
		t.Fatalf("whole-workspace claim after directory writer release: %v", err)
	}
	releaseWhole()
}

func TestSchedulerRealizeSameFileConflicts(t *testing.T) {
	s := NewSubagentScheduler(4, 2)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	claim, err := NormalizeWritePaths(root, []string{"src/"})
	if err != nil {
		t.Fatal(err)
	}
	_, id1, err := s.AcquireWithID(context.Background(), AcquireRequest{Writer: true, WritePaths: claim})
	if err != nil {
		t.Fatal(err)
	}
	_, id2, err := s.AcquireWithID(context.Background(), AcquireRequest{Writer: true, WritePaths: claim})
	if err != nil {
		t.Fatal(err)
	}
	file, err := NormalizeWritePaths(root, []string{"src/a.go"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Realize(id1, file); err != nil {
		t.Fatalf("first realize: %v", err)
	}
	if err := s.Realize(id2, file); err == nil {
		t.Fatal("second realize of the same file must fail")
	}
	other, err := NormalizeWritePaths(root, []string{"src/b.go"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Realize(id2, other); err != nil {
		t.Fatalf("disjoint realize: %v", err)
	}
}

func TestSchedulerMarkOpaqueBlocksRealize(t *testing.T) {
	s := NewSubagentScheduler(4, 2)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	claim, err := NormalizeWritePaths(root, []string{"src/"})
	if err != nil {
		t.Fatal(err)
	}
	_, id1, err := s.AcquireWithID(context.Background(), AcquireRequest{Writer: true, WritePaths: claim})
	if err != nil {
		t.Fatal(err)
	}
	_, id2, err := s.AcquireWithID(context.Background(), AcquireRequest{Writer: true, WritePaths: claim})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.MarkOpaque(id1); err != nil {
		t.Fatal(err)
	}
	file, err := NormalizeWritePaths(root, []string{"src/a.go"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Realize(id2, file); err == nil {
		t.Fatal("realize must fail after sibling goes opaque")
	}
}

func TestSchedulerParentFileWriteAfterChildRealize(t *testing.T) {
	s := NewSubagentScheduler(4, 2)
	root := t.TempDir()
	whole, err := WholeWorkspaceWriteClaim(root)
	if err != nil {
		t.Fatal(err)
	}
	_, id, err := s.AcquireWithID(context.Background(), AcquireRequest{Writer: true, WritePaths: whole})
	if err != nil {
		t.Fatal(err)
	}
	before, err := NormalizeWritePaths(root, []string{"b.go"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReserveParentWrite(before); err == nil {
		t.Fatal("parent file write must wait while child still claims the whole workspace")
	}
	fileA, err := NormalizeWritePaths(root, []string{"a.go"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Realize(id, fileA); err != nil {
		t.Fatal(err)
	}
	release, err := s.ReserveParentWrite(before)
	if err != nil {
		t.Fatalf("parent write of disjoint file after realize: %v", err)
	}
	release()
	if err := s.MarkOpaque(id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReserveParentWrite(before); err == nil {
		t.Fatal("parent write must fail after child goes opaque")
	}
}

func TestSchedulerWholeClaimNarrowsForNewSiblingsAndOpaqueRestoresExclusion(t *testing.T) {
	s := NewSubagentScheduler(4, 3)
	root := t.TempDir()
	whole, err := WholeWorkspaceWriteClaim(root)
	if err != nil {
		t.Fatal(err)
	}
	releaseWhole, id, err := s.AcquireWithID(context.Background(), AcquireRequest{Writer: true, WritePaths: whole})
	if err != nil {
		t.Fatal(err)
	}
	defer releaseWhole()

	fileA, err := NormalizeWritePaths(root, []string{"a.go"})
	if err != nil {
		t.Fatal(err)
	}
	fileB, err := NormalizeWritePaths(root, []string{"b.go"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AcquireWithID(context.Background(), AcquireRequest{
		Writer: true, WritePaths: fileB, Nested: true,
	}); err == nil {
		t.Fatal("new sibling must wait before the whole claim realizes a path")
	}
	if err := s.Realize(id, fileA); err != nil {
		t.Fatal(err)
	}
	releaseB, _, err := s.AcquireWithID(context.Background(), AcquireRequest{
		Writer: true, WritePaths: fileB, Nested: true,
	})
	if err != nil {
		t.Fatalf("new disjoint sibling after realize: %v", err)
	}
	if _, _, err := s.AcquireWithID(context.Background(), AcquireRequest{
		Writer: true, WritePaths: fileA, Nested: true,
	}); err == nil {
		t.Fatal("new same-file sibling must remain blocked")
	}
	releaseB()
	if err := s.MarkOpaque(id); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AcquireWithID(context.Background(), AcquireRequest{
		Writer: true, WritePaths: fileB, Nested: true,
	}); err == nil {
		t.Fatal("opaque mutation must restore whole-workspace exclusion")
	}
}

func TestSchedulerTryClaimWritePaths(t *testing.T) {
	s := NewSubagentScheduler(4, 2)
	root := t.TempDir()
	claim, _ := NormalizeWritePaths(root, []string{"a.md"})
	release, err := s.Acquire(context.Background(), AcquireRequest{Writer: true, WritePaths: claim})
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if err := s.TryClaimWritePaths(claim); err == nil {
		t.Fatal("parent should see active claim")
	}
	other, _ := NormalizeWritePaths(root, []string{"b.md"})
	if err := s.TryClaimWritePaths(other); err != nil {
		t.Fatalf("disjoint claim should be free: %v", err)
	}
}
