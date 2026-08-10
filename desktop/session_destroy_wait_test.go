package main

import (
	"testing"
	"time"

	"reasonix/internal/control"
	"reasonix/internal/jobs"
)

func TestWaitDestroyHandlesPrefersBoundedWait(t *testing.T) {
	requested := make(chan time.Duration, 1)
	unboundedCalled := make(chan struct{}, 1)
	timedOut := waitDestroyHandles([]control.SessionDestroyHandle{{
		Wait: func() jobs.TeardownResult {
			unboundedCalled <- struct{}{}
			return jobs.TeardownResult{}
		},
		WaitFor: func(grace time.Duration) jobs.TeardownResult {
			requested <- grace
			return jobs.TeardownResult{}
		},
	}})
	if timedOut {
		t.Fatal("bounded wait reported an unexpected timeout")
	}
	select {
	case grace := <-requested:
		if grace != desktopSessionRemovalGrace {
			t.Fatalf("bounded wait grace = %s, want %s", grace, desktopSessionRemovalGrace)
		}
	default:
		t.Fatal("bounded wait was not called")
	}
	select {
	case <-unboundedCalled:
		t.Fatal("waitDestroyHandles used the unbounded wait when WaitFor was available")
	default:
	}
}

func TestWaitDestroyHandlesBoundsLegacyWait(t *testing.T) {
	release := make(chan struct{})
	returned := make(chan struct{})
	defer func() {
		close(release)
		select {
		case <-returned:
		case <-time.After(time.Second):
			t.Error("legacy wait goroutine did not finish after release")
		}
	}()
	started := time.Now()
	timedOut := waitDestroyHandles([]control.SessionDestroyHandle{{
		Wait: func() jobs.TeardownResult {
			<-release
			close(returned)
			return jobs.TeardownResult{}
		},
	}})
	elapsed := time.Since(started)
	if !timedOut {
		t.Fatal("non-cooperative legacy wait did not report a timeout")
	}
	if elapsed < desktopSessionRemovalGrace || elapsed > 2*time.Second {
		t.Fatalf("legacy wait returned after %s, want a bounded wait near %s", elapsed, desktopSessionRemovalGrace)
	}
}

func TestWaitDestroyHandleBatchesWaitsAcrossSessionsConcurrently(t *testing.T) {
	started := make(chan int, 2)
	release := make(chan struct{})
	batches := make([][]control.SessionDestroyHandle, 2)
	for i := range batches {
		index := i
		batches[i] = []control.SessionDestroyHandle{{
			WaitFor: func(time.Duration) jobs.TeardownResult {
				started <- index
				<-release
				return jobs.TeardownResult{}
			},
		}}
	}
	done := make(chan []bool, 1)
	go func() { done <- waitDestroyHandleBatches(batches) }()
	seen := map[int]bool{}
	for range batches {
		select {
		case index := <-started:
			seen[index] = true
		case <-time.After(time.Second):
			t.Fatal("session destroy batches did not start concurrently")
		}
	}
	close(release)
	if !seen[0] || !seen[1] {
		t.Fatalf("started batches = %v, want both sessions", seen)
	}
	select {
	case timedOut := <-done:
		if timedOut[0] || timedOut[1] {
			t.Fatalf("completed destroy batches timed out: %v", timedOut)
		}
	case <-time.After(time.Second):
		t.Fatal("concurrent destroy batches did not finish after release")
	}
}
