package mutation

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/state/workspacelease"
)

func capacity(id string) Capacity { return Capacity{ID: id, Limit: 1, Units: 1} }

func TestDisjointWriterCapacityWaitReportsActualBlockingResource(t *testing.T) {
	root, state := t.TempDir(), t.TempDir()
	c := testCoordinator(t, root, state)
	first := intent(filepath.Join(root, "first"))
	first.Capacities = []Capacity{capacity("writers")}
	held, err := c.Acquire(t.Context(), first)
	if err != nil {
		t.Fatal(err)
	}
	defer held.End(NotExecuted)
	var notices []Notice
	waiter, err := New(root, state, func(n Notice) {
		if n.State != "released" {
			notices = append(notices, n)
		}
		if n.State == "waiting" {
			held.End(NotExecuted)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	second := intent(filepath.Join(root, "second"))
	second.Capacities = first.Capacities
	lease, err := waiter.Acquire(t.Context(), second)
	if err != nil {
		t.Fatal(err)
	}
	lease.End(NotExecuted)
	if len(notices) != 2 {
		t.Fatalf("unpaired wait: %+v", notices)
	}
	for _, n := range notices {
		if n.Effect != NamedPaths || n.BlockedOn != CapacityOnly {
			t.Fatalf("request kind confused with blocking cause: %+v", n)
		}
	}
}

func awaitCapacityNotice(t *testing.T, notices <-chan Notice, state string) Notice {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	for {
		select {
		case n := <-notices:
			if n.State == state {
				return n
			}
		case <-ctx.Done():
			t.Fatalf("missing %s notice", state)
		}
	}
}

func TestWaitingFileTransactionDoesNotConsumeCapacity(t *testing.T) {
	root, state := t.TempDir(), t.TempDir()
	owner := testCoordinator(t, root, state)
	path := filepath.Join(root, "a")
	held, err := owner.Acquire(t.Context(), intent(path))
	if err != nil {
		t.Fatal(err)
	}
	defer held.End(NotExecuted)
	notices := make(chan Notice, 16)
	waiter, err := New(root, state, func(n Notice) { notices <- n })
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		request := intent(path)
		request.Capacities = []Capacity{capacity("root-total")}
		lease, err := waiter.Acquire(ctx, request)
		if lease != nil {
			lease.End(NotExecuted)
		}
		done <- err
	}()
	awaitCapacityNotice(t, notices, "waiting")
	model, err := owner.Acquire(t.Context(), Intent{Effect: CapacityOnly, Capacities: []Capacity{capacity("root-total")}})
	if err != nil {
		t.Fatalf("waiting writer consumed model capacity: %v", err)
	}
	defer model.End(NotExecuted)
	held.End(NotExecuted)
	model.End(NotExecuted)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("writer did not resume")
	}
}

func TestWaitingCapacityTransactionDoesNotHoldPaths(t *testing.T) {
	root, state := t.TempDir(), t.TempDir()
	owner := testCoordinator(t, root, state)
	held, err := owner.Acquire(t.Context(), Intent{Effect: CapacityOnly, Capacities: []Capacity{capacity("root-total")}})
	if err != nil {
		t.Fatal(err)
	}
	defer held.End(NotExecuted)
	notices := make(chan Notice, 16)
	waiter, err := New(root, state, func(n Notice) { notices <- n })
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	path := filepath.Join(root, "a")
	go func() {
		request := intent(path)
		request.Capacities = []Capacity{capacity("root-total")}
		lease, err := waiter.Acquire(ctx, request)
		if lease != nil {
			lease.End(NotExecuted)
		}
		done <- err
	}()
	awaitCapacityNotice(t, notices, "waiting")
	// An unrelated writer has no admission to this root's execution budget.
	other, err := testCoordinator(t, t.TempDir(), state).Acquire(t.Context(), intent(filepath.Join(root, "b")))
	if err != nil {
		t.Fatal(err)
	}
	other.End(NotExecuted)
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	lease, err := owner.Acquire(t.Context(), intent(path))
	if err != nil {
		t.Fatalf("cancelled capacity request retained paths: %v", err)
	}
	lease.End(NotExecuted)
}

func TestCapacityGrantsReadyWaitersInOrder(t *testing.T) {
	root, state := t.TempDir(), t.TempDir()
	owner := testCoordinator(t, root, state)
	held, err := owner.Acquire(t.Context(), Intent{Effect: CapacityOnly, Capacities: []Capacity{capacity("total")}})
	if err != nil {
		t.Fatal(err)
	}
	defer held.End(NotExecuted)
	granted := make(chan string, 2)
	releases := []chan struct{}{make(chan struct{}), make(chan struct{})}
	done := make(chan error, 2)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	for index, id := range []string{"first", "second"} {
		notices := make(chan Notice, 16)
		c, err := New(root, state, func(n Notice) { notices <- n })
		if err != nil {
			t.Fatal(err)
		}
		go func() {
			l, err := c.Acquire(ctx, Intent{Effect: CapacityOnly, ExecutionID: id, Capacities: []Capacity{capacity("total")}})
			if err == nil {
				granted <- id
				select {
				case <-releases[index]:
				case <-ctx.Done():
				}
				l.End(NotExecuted)
			}
			done <- err
		}()
		awaitCapacityNotice(t, notices, "waiting")
	}
	held.End(NotExecuted)
	select {
	case id := <-granted:
		if id != "first" {
			t.Fatal(id)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("first waiter stuck")
	}
	close(releases[0])
	select {
	case id := <-granted:
		if id != "second" {
			t.Fatal(id)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("second waiter stuck")
	}
	close(releases[1])
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
}

func TestCapacityIdentityCannotChangeLimit(t *testing.T) {
	c := testCoordinator(t, t.TempDir(), t.TempDir())
	l, err := c.Acquire(t.Context(), Intent{Effect: CapacityOnly, Capacities: []Capacity{capacity("total")}})
	if err != nil {
		t.Fatal(err)
	}
	defer l.End(NotExecuted)
	_, err = c.Acquire(t.Context(), Intent{Effect: CapacityOnly, Capacities: []Capacity{{ID: "total", Limit: 2, Units: 1}}})
	if !errors.Is(err, ErrProtocol) {
		t.Fatalf("capacity limit changed under an active owner: %v", err)
	}
}

func TestLegacyWaitDoesNotConsumeExecutionCapacity(t *testing.T) {
	root, state := t.TempDir(), t.TempDir()
	path := filepath.Join(root, "a")
	legacy, err := workspacelease.New(root, state, nil)
	if err != nil {
		t.Fatal(err)
	}
	legacy.BeginRun()
	defer legacy.EndRun()
	if err := legacy.AcquirePaths(t.Context(), []string{path}); err != nil {
		t.Fatal(err)
	}
	notices := make(chan Notice, 16)
	c, err := New(root, state, func(n Notice) { notices <- n })
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	defer func() { cancel(); <-done }()
	go func() {
		req := intent(path)
		req.Capacities = []Capacity{capacity("total")}
		l, err := c.Acquire(ctx, req)
		if l != nil {
			l.End(NotExecuted)
		}
		done <- err
	}()
	awaitCapacityNotice(t, notices, "waiting")
	probeCtx, stop := context.WithTimeout(t.Context(), time.Second)
	defer stop()
	l, err := testCoordinator(t, root, state).Acquire(probeCtx, Intent{Effect: CapacityOnly, Capacities: []Capacity{capacity("total")}})
	if err != nil {
		t.Fatalf("legacy file wait exhausted execution capacity: %v", err)
	}
	l.End(NotExecuted)
}

func TestLargeCapacityRequestCannotBeStarvedBySmallerLaterRequests(t *testing.T) {
	root, state := t.TempDir(), t.TempDir()
	one := Intent{Effect: CapacityOnly, Capacities: []Capacity{{ID: "total", Limit: 2, Units: 1}}}
	owner := testCoordinator(t, root, state)
	held, err := owner.Acquire(t.Context(), one)
	if err != nil {
		t.Fatal(err)
	}
	defer held.End(NotExecuted)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	type result struct {
		lease *Lease
		err   error
	}
	start := func(request Intent) (<-chan Notice, <-chan result) {
		notices, done := make(chan Notice, 16), make(chan result, 1)
		c, err := New(root, state, func(n Notice) { notices <- n })
		if err != nil {
			t.Fatal(err)
		}
		go func() { l, err := c.Acquire(ctx, request); done <- result{l, err} }()
		return notices, done
	}
	largeNotices, largeDone := start(Intent{Effect: CapacityOnly, Capacities: []Capacity{{ID: "total", Limit: 2, Units: 2}}})
	awaitCapacityNotice(t, largeNotices, "waiting")
	smallNotices, smallDone := start(one)
	select {
	case n := <-smallNotices:
		if n.State != "waiting" {
			t.Fatalf("unexpected event: %+v", n)
		}
	case got := <-smallDone:
		if got.lease != nil {
			got.lease.End(NotExecuted)
		}
		t.Fatalf("small request bypassed earlier large request: %v", got.err)
	case <-ctx.Done():
		t.Fatal("small request did not queue")
	}
	held.End(NotExecuted)
	var large result
	select {
	case large = <-largeDone:
	case <-ctx.Done():
		t.Fatal("large request starved")
	}
	if large.err != nil {
		t.Fatal(large.err)
	}
	defer large.lease.End(NotExecuted)
	large.lease.End(NotExecuted)
	select {
	case small := <-smallDone:
		if small.err != nil {
			t.Fatal(small.err)
		}
		small.lease.End(NotExecuted)
	case <-ctx.Done():
		t.Fatal("small request did not resume")
	}
}
