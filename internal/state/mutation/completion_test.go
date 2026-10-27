package mutation

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestEndedTransactionRetainsResourcesUntilAllExecutionsSettle(t *testing.T) {
	root, state := t.TempDir(), t.TempDir()
	first, second := testCoordinator(t, root, state), testCoordinator(t, root, state)
	l, err := first.Acquire(t.Context(), Intent{Effect: Workspace})
	if err != nil {
		t.Fatal(err)
	}
	a, b := make(chan struct{}), make(chan struct{})
	if err := HoldUntil(l.Context(t.Context()), a); err != nil {
		t.Fatal(err)
	}
	if err := l.HoldUntil(b); err != nil {
		t.Fatal(err)
	}
	l.End(Unknown)
	if l.HoldingPaths() || l.CheckPaths([]string{root}) == nil {
		t.Fatal("ended caller retained execution authority")
	}
	if err := l.HoldUntil(make(chan struct{})); !errors.Is(err, ErrInvalidIntent) {
		t.Fatalf("ended caller registered new execution: %v", err)
	}
	assertBusy := func() {
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
		defer cancel()
		other, err := second.Acquire(ctx, Intent{Effect: Workspace})
		if other != nil {
			other.End(NotExecuted)
		}
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("unsettled execution lost exclusion: %v", err)
		}
	}
	assertBusy()
	close(a)
	assertBusy()
	close(b)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	next, err := second.Acquire(ctx, Intent{Effect: Workspace})
	if err != nil {
		t.Fatal(err)
	}
	defer next.End(NotExecuted)
	l.End(NotExecuted)
	assertBusy()
}
