package mutation

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"testing"

	"reasonix/internal/base/filelock"
	"reasonix/internal/state/workspacelease"
)

func TestCompatibilityPreparationDoesNotHoldTheGlobalRegistry(t *testing.T) {
	root, state := t.TempDir(), t.TempDir()
	first := testCoordinator(t, root, state)
	a, b := filepath.Join(root, "a"), filepath.Join(root, "b")
	entered, resume := make(chan struct{}), make(chan struct{})
	first.compatClaim = func(root, dir, session string, paths []string) (func(), *workspacelease.ConflictError, error) {
		if slices.ContainsFunc(paths, func(path string) bool { return filepath.Base(path) == "a" }) {
			close(entered)
			<-resume
		}
		return workspacelease.TryClaimPaths(root, dir, session, paths)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		lease, err := first.Acquire(ctx, intent(a))
		if lease != nil {
			lease.End(NotExecuted)
		}
		done <- err
	}()
	select {
	case <-entered:
	case err := <-done:
		t.Fatalf("call returned before compatibility preparation: %v", err)
	}
	defer func() { close(resume); <-done }()
	unlock, err := filelock.TryAcquire(first.guardPath())
	if err != nil {
		t.Fatalf("compatibility discovery retained the global registry: %v", err)
	}
	unlock()
	second := testCoordinator(t, root, state)
	lease, err := second.Acquire(t.Context(), intent(b))
	if err != nil {
		t.Fatalf("unrelated resources blocked by compatibility preparation: %v", err)
	}
	lease.End(NotExecuted)
}

func TestPreparingCompatibilityClaimRetainsCapacityQueuePriority(t *testing.T) {
	root, state := t.TempDir(), t.TempDir()
	first := testCoordinator(t, root, state)
	claimed, resume := make(chan struct{}), make(chan struct{})
	first.compatClaim = func(root, dir, session string, paths []string) (func(), *workspacelease.ConflictError, error) {
		release, conflict, err := workspacelease.TryClaimPaths(root, dir, session, paths)
		if err == nil {
			close(claimed)
			<-resume
		}
		return release, conflict, err
	}
	pool := []Capacity{{ID: "writers", Limit: 1, Units: 1}}
	want := intent(filepath.Join(root, "a"))
	want.Capacities = pool
	done := make(chan error, 1)
	go func() {
		lease, err := first.Acquire(t.Context(), want)
		if lease != nil {
			lease.End(NotExecuted)
		}
		done <- err
	}()
	select {
	case <-claimed:
	case err := <-done:
		t.Fatalf("first call returned before compatibility grant: %v", err)
	}
	defer func() {
		close(resume)
		if err := <-done; err != nil {
			t.Errorf("first call lost admission: %v", err)
		}
	}()
	second := testCoordinator(t, root, state)
	want = intent(filepath.Join(root, "b"))
	want.Capacities = pool
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	second.notify = func(notice Notice) {
		if notice.State == "waiting" {
			cancel()
		}
	}
	lease, err := second.Acquire(ctx, want)
	if lease != nil {
		lease.End(NotExecuted)
		t.Fatal("later disjoint path stole the preparing transaction's capacity")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("later capacity waiter: %v", err)
	}
}

func TestCancellationAfterCompatibilityGrantLeavesNoClaims(t *testing.T) {
	root, state := t.TempDir(), t.TempDir()
	c := testCoordinator(t, root, state)
	path := filepath.Join(root, "a")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	c.compatClaim = func(root, dir, session string, paths []string) (func(), *workspacelease.ConflictError, error) {
		release, conflict, err := workspacelease.TryClaimPaths(root, dir, session, paths)
		if err == nil {
			cancel()
		}
		return release, conflict, err
	}
	if _, err := c.Acquire(ctx, intent(path)); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled compatibility admission: %v", err)
	}
	second := testCoordinator(t, root, state)
	lease, err := second.Acquire(t.Context(), intent(path))
	if err != nil {
		t.Fatalf("cancelled admission retained new claims: %v", err)
	}
	lease.End(NotExecuted)
}
