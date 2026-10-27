package mutation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testCoordinator(t *testing.T, root, state string) *Coordinator {
	t.Helper()
	c, err := New(root, state, nil)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func intent(paths ...string) Intent {
	return Intent{Effect: NamedPaths, Paths: paths, SessionID: "test", ExecutionID: "execution", CallID: "call"}
}

func TestTransactionReleasesBeforeSessionEnds(t *testing.T) {
	root, state := t.TempDir(), t.TempDir()
	a, b := testCoordinator(t, root, state), testCoordinator(t, root, state)
	first, err := a.Acquire(t.Context(), intent(filepath.Join(root, "a")))
	if err != nil {
		t.Fatal(err)
	}
	second, err := b.Acquire(t.Context(), intent(filepath.Join(root, "b")))
	if err != nil {
		t.Fatal(err)
	}
	second.End(NotExecuted)
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Millisecond)
	defer cancel()
	if _, err := b.Acquire(ctx, intent(filepath.Join(root, "a"))); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("overlap: %v", err)
	}
	first.End(Committed)
	next, err := b.Acquire(t.Context(), intent(filepath.Join(root, "a")))
	if err != nil {
		t.Fatal(err)
	}
	defer next.End(NotExecuted)
	first.End(NotExecuted)
	ctx2, cancel2 := context.WithTimeout(t.Context(), 40*time.Millisecond)
	defer cancel2()
	if _, err := a.Acquire(ctx2, intent(filepath.Join(root, "a"))); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("old release removed new lease: %v", err)
	}
}

func TestDifferentWorkspacesCoordinateExtraPathAndHardlinks(t *testing.T) {
	state, extra := t.TempDir(), filepath.Join(t.TempDir(), "shared")
	if err := os.WriteFile(extra, []byte("base"), 0600); err != nil {
		t.Fatal(err)
	}
	a, b := testCoordinator(t, t.TempDir(), state), testCoordinator(t, t.TempDir(), state)
	lease, err := a.Acquire(t.Context(), intent(extra))
	if err != nil {
		t.Fatal(err)
	}
	defer lease.End(NotExecuted)
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Link(extra, alias); err != nil {
		t.Skipf("hardlinks unavailable: %v", err)
	}
	for _, path := range []string{extra, alias} {
		ctx, cancel := context.WithTimeout(t.Context(), 40*time.Millisecond)
		_, err := b.Acquire(ctx, intent(path))
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("%s: %v", path, err)
		}
	}
}

func TestQueuedSetDoesNotHoldPartialResources(t *testing.T) {
	root, state := t.TempDir(), t.TempDir()
	c := testCoordinator(t, root, state)
	a, b := filepath.Join(root, "a"), filepath.Join(root, "b")
	first, err := c.Acquire(t.Context(), intent(a))
	if err != nil {
		t.Fatal(err)
	}
	defer first.End(NotExecuted)
	queued := make(chan Notice, 8)
	waiter, err := New(root, state, func(n Notice) { queued <- n })
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		l, e := waiter.Acquire(ctx, intent(a, b))
		if l != nil {
			l.End(NotExecuted)
		}
		done <- e
	}()
	select {
	case <-queued:
	case <-time.After(3 * time.Second):
		t.Fatal("wait was not reported")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	last, err := c.Acquire(t.Context(), intent(b))
	if err != nil {
		t.Fatal(err)
	}
	last.End(NotExecuted)
}

func TestExplicitEffectsAndContentPreconditions(t *testing.T) {
	root, state := t.TempDir(), t.TempDir()
	c := testCoordinator(t, root, state)
	if _, err := c.Acquire(t.Context(), intent()); !errors.Is(err, ErrInvalidIntent) {
		t.Fatalf("empty named set: %v", err)
	}
	path := filepath.Join(root, "a")
	if err := os.WriteFile(path, []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	version, err := ReadVersionContext(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("after"), 0600); err != nil {
		t.Fatal(err)
	}
	i := intent(path)
	i.Expected = map[string]ContentVersion{path: version}
	if _, err := c.Acquire(t.Context(), i); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale precondition: %v", err)
	}
	lease, err := c.Acquire(t.Context(), intent(path))
	if err != nil {
		t.Fatalf("failed precondition leaked resources: %v", err)
	}
	lease.End(NotExecuted)
}

func TestNestedTransactionCannotWidenOrReleaseParent(t *testing.T) {
	root, state := t.TempDir(), t.TempDir()
	c := testCoordinator(t, root, state)
	path := filepath.Join(root, "a")
	parent, err := c.Acquire(t.Context(), intent(path))
	if err != nil {
		t.Fatal(err)
	}
	defer parent.End(NotExecuted)
	ctx := parent.Context(t.Context())
	child, err := c.Acquire(ctx, intent(path))
	if err != nil {
		t.Fatal(err)
	}
	child.End(NotExecuted)
	if _, err := c.Acquire(ctx, intent(filepath.Join(root, "b"))); !errors.Is(err, ErrInvalidIntent) {
		t.Fatalf("nested scope widened: %v", err)
	}
	other, cancel := context.WithTimeout(t.Context(), 40*time.Millisecond)
	defer cancel()
	if _, err := c.Acquire(other, intent(path)); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("child released parent: %v", err)
	}
}
