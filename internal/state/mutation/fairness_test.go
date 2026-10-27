package mutation

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestOverlappingQueueIsFIFOAndDisjointWorkProceeds(t *testing.T) {
	root, state := t.TempDir(), t.TempDir()
	coordinator := testCoordinator(t, root, state)
	a, b, c := filepath.Join(root, "a"), filepath.Join(root, "b"), filepath.Join(root, "c")
	held, err := coordinator.Acquire(t.Context(), intent(a))
	if err != nil {
		t.Fatal(err)
	}
	defer held.End(NotExecuted)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	type result struct {
		name  string
		lease *Lease
		err   error
	}
	done := make(chan result, 2)
	start := func(name string, paths ...string) {
		queued := make(chan struct{}, 1)
		waiter, err := New(root, state, func(n Notice) {
			if n.State == "waiting" {
				queued <- struct{}{}
			}
		})
		if err != nil {
			t.Fatal(err)
		}
		go func() { lease, err := waiter.Acquire(ctx, intent(paths...)); done <- result{name, lease, err} }()
		select {
		case <-queued:
		case <-ctx.Done():
			t.Fatal("waiter did not enter queue")
		}
	}
	start("first", a, b)
	start("second", a)
	disjoint, err := coordinator.Acquire(ctx, intent(c))
	if err != nil {
		t.Fatalf("disjoint work blocked: %v", err)
	}
	disjoint.End(NotExecuted)
	held.End(NotExecuted)
	first := <-done
	if first.lease != nil {
		defer first.lease.End(NotExecuted)
	}
	if first.err != nil || first.name != "first" {
		t.Fatalf("queue head: %+v", first)
	}
	first.lease.End(NotExecuted)
	second := <-done
	if second.lease != nil {
		defer second.lease.End(NotExecuted)
	}
	if second.err != nil || second.name != "second" {
		t.Fatalf("queue successor: %+v", second)
	}
}
