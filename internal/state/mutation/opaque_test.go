package mutation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func opaqueIntent() Intent { return Intent{Effect: Effect("opaque_write")} }

func TestOpaqueWriteExcludesExternalHardlinkInBothOrders(t *testing.T) {
	for _, opaqueFirst := range []bool{true, false} {
		t.Run(map[bool]string{true: "opaque_first", false: "named_first"}[opaqueFirst], func(t *testing.T) {
			root, extra, state := t.TempDir(), t.TempDir(), t.TempDir()
			source, alias := filepath.Join(root, "source"), filepath.Join(extra, "alias")
			if err := os.WriteFile(source, []byte("original"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Link(source, alias); err != nil {
				t.Fatal(err)
			}
			a, b := testCoordinator(t, root, state), testCoordinator(t, extra, state)
			first, second := opaqueIntent(), intent(alias)
			if !opaqueFirst {
				a, b, first, second = b, a, second, first
			}
			held, err := a.Acquire(t.Context(), first)
			if err != nil {
				t.Fatal(err)
			}
			defer held.End(NotExecuted)
			blocked, err := b.TryAcquire(t.Context(), second)
			if blocked != nil {
				blocked.End(NotExecuted)
			}
			if !errors.Is(err, ErrBusy) {
				t.Fatalf("cross-root alias admitted: %v", err)
			}
			held.End(NotExecuted)
			next, err := b.TryAcquire(t.Context(), second)
			if err != nil {
				t.Fatalf("ended opaque owner still blocks: %v", err)
			}
			defer next.End(Committed)
			if err := os.WriteFile(alias, []byte("changed"), 0600); err != nil {
				t.Fatal(err)
			}
			actual, err := os.ReadFile(source)
			if err != nil || string(actual) != "changed" {
				t.Fatalf("alias control did not mutate source: %q %v", actual, err)
			}
		})
	}
}

func TestOpaqueWriteDoesNotSerializeNamedOrModelWork(t *testing.T) {
	root, extra, state := t.TempDir(), t.TempDir(), t.TempDir()
	a, b := testCoordinator(t, root, state), testCoordinator(t, extra, state)
	held, err := a.Acquire(t.Context(), intent("first"))
	if err != nil {
		t.Fatal(err)
	}
	defer held.End(NotExecuted)
	disjoint, err := b.TryAcquire(t.Context(), intent("second"))
	if err != nil {
		t.Fatal(err)
	}
	disjoint.End(NotExecuted)
	held.End(NotExecuted)
	opaque, err := a.Acquire(t.Context(), opaqueIntent())
	if err != nil {
		t.Fatal(err)
	}
	defer opaque.End(NotExecuted)
	model, err := b.TryAcquire(t.Context(), Intent{Effect: CapacityOnly, Capacities: []Capacity{capacity("model")}})
	if err != nil {
		t.Fatalf("opaque file owner consumed unrelated model capacity: %v", err)
	}
	model.End(NotExecuted)
	for _, effect := range []Effect{ReadOnly, External} {
		lease, err := b.TryAcquire(t.Context(), Intent{Effect: effect})
		if err != nil {
			t.Fatalf("non-file admission: %v", err)
		}
		lease.End(NotExecuted)
	}
}

func TestOpaqueWriteNestedCoverageAndContinuation(t *testing.T) {
	c := testCoordinator(t, t.TempDir(), t.TempDir())
	for _, parent := range []Intent{{Effect: Workspace}, intent(c.root), opaqueIntent()} {
		held, err := c.Acquire(t.Context(), parent)
		if err != nil {
			t.Fatal(err)
		}
		ctx := held.Context(t.Context())
		child, err := c.TryAcquire(ctx, opaqueIntent())
		if parent.Effect != opaqueIntent().Effect {
			if child != nil || !errors.Is(err, ErrInvalidIntent) {
				t.Fatalf("directory scope upgraded to opaque: %v", err)
			}
		} else {
			if err != nil {
				t.Fatal(err)
			}
			child.End(NotExecuted)
		}
		if _, err := c.TryAcquire(ctx, intent(filepath.Join(t.TempDir(), "outside"))); !errors.Is(err, ErrInvalidIntent) {
			t.Fatalf("global exclusion expanded nested authority: %v", err)
		}
		held.End(NotExecuted)
	}
	held, err := c.Acquire(t.Context(), opaqueIntent())
	if err != nil {
		t.Fatal(err)
	}
	defer held.End(NotExecuted)
	continuation, err := held.PrepareContinuation()
	if err != nil {
		t.Fatal(err)
	}
	held.End(NotExecuted)
	next, err := continuation.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer next.End(NotExecuted)
	other := testCoordinator(t, t.TempDir(), c.compatDir)
	if lease, err := other.TryAcquire(t.Context(), intent("outside")); !errors.Is(err, ErrBusy) {
		if lease != nil {
			lease.End(NotExecuted)
		}
		t.Fatalf("continuation lost opaque exclusion: %v", err)
	}
}

func TestOpaqueWriteQueueCancellationDoesNotConsumeModelCapacity(t *testing.T) {
	root, state := t.TempDir(), t.TempDir()
	held, err := testCoordinator(t, root, state).Acquire(t.Context(), intent("held"))
	if err != nil {
		t.Fatal(err)
	}
	defer held.End(NotExecuted)
	notices := make(chan Notice, 8)
	c, err := New(t.TempDir(), state, func(n Notice) { notices <- n })
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	joined := make(chan struct{})
	t.Cleanup(func() { cancel(); <-joined })
	request := opaqueIntent()
	request.Capacities = []Capacity{capacity("model")}
	go func() {
		defer close(joined)
		lease, err := c.Acquire(ctx, request)
		if lease != nil {
			lease.End(NotExecuted)
		}
		done <- err
	}()
	awaitCapacityNotice(t, notices, "waiting")
	model, err := c.TryAcquire(t.Context(), Intent{Effect: CapacityOnly, Capacities: []Capacity{capacity("model")}})
	if err != nil {
		t.Fatalf("file waiter reserved model capacity: %v", err)
	}
	model.End(NotExecuted)
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if n := awaitCapacityNotice(t, notices, "abandoned"); n.Reason != "cancelled" {
		t.Fatal(n)
	}
	next, err := c.TryAcquire(t.Context(), intent("disjoint"))
	if err != nil {
		t.Fatalf("cancelled opaque waiter retained global exclusion: %v", err)
	}
	next.End(NotExecuted)
}

func TestOpaqueWriteQueuePreventsNamedBypass(t *testing.T) {
	root, state := t.TempDir(), t.TempDir()
	c := testCoordinator(t, root, state)
	held, err := c.Acquire(t.Context(), intent("held"))
	if err != nil {
		t.Fatal(err)
	}
	defer held.End(NotExecuted)
	notices := make(chan Notice, 8)
	waiter, err := New(t.TempDir(), state, func(n Notice) { notices <- n })
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	joined, release := make(chan struct{}), make(chan struct{})
	t.Cleanup(func() { cancel(); <-joined })
	go func() {
		defer close(joined)
		lease, err := waiter.Acquire(ctx, opaqueIntent())
		done <- err
		if lease != nil {
			defer lease.End(NotExecuted)
			select {
			case <-release:
			case <-ctx.Done():
			}
		}
	}()
	awaitCapacityNotice(t, notices, "waiting")
	if lease, err := c.TryAcquire(t.Context(), intent("unrelated")); !errors.Is(err, ErrBusy) {
		if lease != nil {
			lease.End(NotExecuted)
		}
		t.Fatalf("later named request bypassed opaque queue head: %v", err)
	}
	held.End(NotExecuted)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	awaitCapacityNotice(t, notices, "acquired")
	close(release)
	<-joined
	next, err := c.TryAcquire(t.Context(), intent("unrelated"))
	if err != nil {
		t.Fatalf("released opaque queue owner still blocks: %v", err)
	}
	next.End(NotExecuted)
}
