package mutation

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestOpaqueWorktreeWriteCoordinatesSharedMetadataOnlyDuringMutation(t *testing.T) {
	root, linked, state := t.TempDir(), t.TempDir(), t.TempDir()
	common := filepath.Join(root, ".git")
	owner := testCoordinator(t, root, state)
	held, err := owner.Acquire(t.Context(), Intent{Effect: GitMetadata, Paths: []string{common}})
	if err != nil {
		t.Fatal(err)
	}
	defer held.End(NotExecuted)
	notices := make(chan Notice, 8)
	c, err := New(linked, state, func(n Notice) { notices <- n }, Options{WorkspaceResources: []string{common}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		lease, err := c.Acquire(ctx, Intent{Effect: Workspace})
		if lease != nil {
			lease.End(NotExecuted)
		}
		done <- err
	}()
	start := awaitCapacityNotice(t, notices, "waiting")
	if start.HolderTransactionID != held.ID {
		t.Fatal("opaque write did not wait for shared metadata")
	}
	other, err := owner.Acquire(t.Context(), Intent{Effect: NamedPaths, Paths: []string{filepath.Join(root, "independent.txt")}})
	if err != nil {
		t.Fatal(err)
	}
	other.End(NotExecuted)
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	end := awaitCapacityNotice(t, notices, "abandoned")
	if end.TransactionID != start.TransactionID {
		t.Fatal("unclosed opaque metadata wait")
	}
}

func TestContinuationRepeatsOpaqueResourceAdmission(t *testing.T) {
	var changed atomic.Bool
	c, err := New(t.TempDir(), t.TempDir(), nil, Options{AdmissionCheck: func(_ context.Context, i Intent) error {
		if i.Effect == Workspace && changed.Load() {
			return ErrInvalidIntent
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := c.Acquire(t.Context(), Intent{Effect: Workspace})
	if err != nil {
		t.Fatal(err)
	}
	continuation, err := lease.PrepareContinuation()
	if err != nil {
		t.Fatal(err)
	}
	lease.End(NotExecuted)
	changed.Store(true)
	if next, err := continuation.Acquire(t.Context()); !errors.Is(err, ErrInvalidIntent) {
		if next != nil {
			next.End(NotExecuted)
		}
		t.Fatalf("background admission forgot opaque resource identity: %v", err)
	}
}

func TestContextAdmissionCancellationReleasesAllResources(t *testing.T) {
	entered := make(chan struct{})
	root, state := t.TempDir(), t.TempDir()
	c, err := New(root, state, nil, Options{AdmissionCheck: func(ctx context.Context, _ Intent) error {
		close(entered)
		<-ctx.Done()
		return ctx.Err()
	}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		lease, err := c.Acquire(ctx, Intent{Effect: Workspace})
		lease.End(NotExecuted)
		done <- err
	}()
	<-entered
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled admission: %v", err)
	}
	other := testCoordinator(t, root, state)
	lease, err := other.Acquire(t.Context(), Intent{Effect: Workspace})
	if err != nil {
		t.Fatal(err)
	}
	lease.End(NotExecuted)
}
