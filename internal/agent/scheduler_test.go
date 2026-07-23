package agent

import (
	"context"
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

	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
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
		}()
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
