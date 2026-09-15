package session

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestRebuildSlotsPriorityOrder(t *testing.T) {
	slots := newRebuildSlots(1)
	if !slots.tryAcquire() {
		t.Fatal("initial tryAcquire failed")
	}
	if slots.tryAcquire() {
		t.Fatal("over-capacity tryAcquire succeeded")
	}

	// Queue one waiter per priority while the only slot is held.
	order := make([]rebuildPriority, 0, 3)
	var mu sync.Mutex
	ready := make(chan struct{})
	var wg sync.WaitGroup
	start := func(prio rebuildPriority) {
		wg.Go(func() {
			if err := slots.acquire(context.Background(), prio); err != nil {
				t.Errorf("acquire(%d): %v", prio, err)
				return
			}
			mu.Lock()
			order = append(order, prio)
			mu.Unlock()
			slots.release()
			ready <- struct{}{}
		})
	}
	start(rebuildPriorityPrefetch)
	time.Sleep(20 * time.Millisecond) // let the prefetch waiter queue first
	start(rebuildPrioritySearch)
	start(rebuildPriorityUser)
	time.Sleep(50 * time.Millisecond)

	slots.release()
	for range 3 {
		select {
		case <-ready:
		case <-time.After(5 * time.Second):
			t.Fatal("waiter never granted")
		}
	}
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	// Highest priority wins each freed slot regardless of arrival order.
	if len(order) != 3 || order[0] != rebuildPriorityUser || order[1] != rebuildPrioritySearch || order[2] != rebuildPriorityPrefetch {
		t.Fatalf("grant order %v, want user,search,prefetch", order)
	}
}

func TestRebuildSlotsCancelDoesNotDropGrant(t *testing.T) {
	slots := newRebuildSlots(1)
	if !slots.tryAcquire() {
		t.Fatal("initial tryAcquire failed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- slots.acquire(ctx, rebuildPriorityUser)
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()
	slots.release()
	select {
	case err := <-errCh:
		// cancel precedes release, so either interleaving is correct; what must
		// never happen is a lost grant. A cancelled waiter leaves the freed
		// slot available, and a successful one owns it and must release it.
		if err == nil {
			slots.release()
			break
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled acquire returned %v, want context.Canceled", err)
		}
		if !slots.tryAcquire() {
			t.Fatal("cancelled acquire consumed the freed slot")
		}
		slots.release()
	case <-time.After(5 * time.Second):
		t.Fatal("acquire never returned")
	}
}

func TestRebuildSlotsCancelRemovesWaiter(t *testing.T) {
	slots := newRebuildSlots(1)
	if !slots.tryAcquire() {
		t.Fatal("initial tryAcquire failed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- slots.acquire(ctx, rebuildPrioritySearch)
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()
	if err := <-errCh; err == nil {
		t.Fatal("cancelled waiter acquired without grant")
	}
	slots.release()
	// The queue must be empty: the released slot is free for the next caller.
	if !slots.tryAcquire() {
		t.Fatal("cancelled waiter still queued in front of new callers")
	}
	slots.release()
}
