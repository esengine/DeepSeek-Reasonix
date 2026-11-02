package mutation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"

	"reasonix/internal/base/proc"
)

func TestMutationRootWaitHelper(t *testing.T) {
	if os.Getenv("REASONIX_MUTATION_ROOT_WAIT_HELPER") == "1" {
		fmt.Print("root-output")
		os.Exit(0)
	}
}

type rootWaitOutput struct {
	once    sync.Once
	entered chan struct{}
	release chan struct{}
}

func (w *rootWaitOutput) Write(p []byte) (int, error) {
	w.once.Do(func() { close(w.entered) })
	<-w.release
	return len(p), nil
}

func TestRootWaitRetainsLeaseAfterRealBoundedCancellation(t *testing.T) {
	root, state := t.TempDir(), t.TempDir()
	released := make(chan struct{}, 1)
	c, err := New(root, state, func(n Notice) {
		if n.State == "released" && n.CallID == "root-wait" {
			released <- struct{}{}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	request := intent("held")
	request.CallID = "root-wait"
	lease, err := c.Acquire(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.End(NotExecuted)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	output := &rootWaitOutput{entered: make(chan struct{}), release: make(chan struct{})}
	unblock := sync.OnceFunc(func() { close(output.release) })
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestMutationRootWaitHelper$")
	cmd.Env = append(os.Environ(), "REASONIX_MUTATION_ROOT_WAIT_HELPER=1")
	cmd.Stdout = output
	type result struct {
		tracked *proc.TrackedCommand
		err     error
	}
	returned, joined := make(chan result, 1), make(chan struct{})
	go func() {
		defer close(joined)
		tracked, err := proc.RunCommand(ctx, cmd, proc.RunOptions{Track: true,
			CancelWaitGrace: time.Millisecond, CancelRetryInterval: time.Millisecond, CancelRetryFor: time.Millisecond})
		returned <- result{tracked, err}
	}()
	t.Cleanup(func() { unblock(); cancel(); <-joined })
	select {
	case <-output.entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	cancel()
	res := <-returned
	if !errors.Is(res.err, context.Canceled) {
		t.Fatalf("bounded cancellation result: %v", res.err)
	}
	done := res.tracked.RootWaitCompleted()
	if done == nil {
		t.Fatal("real tracked wait was not registered")
	}
	t.Cleanup(func() { unblock(); <-done })
	select {
	case <-done:
		t.Fatal("blocked output copy was not a pending cmd.Wait")
	default:
	}
	if err := lease.HoldUntil(done); err != nil {
		t.Fatal(err)
	}
	lease.End(Unknown)
	other := testCoordinator(t, root, state)
	if next, err := other.TryAcquire(t.Context(), request); !errors.Is(err, ErrBusy) {
		if next != nil {
			next.End(NotExecuted)
		}
		t.Fatalf("logical return released physical ownership: %v", err)
	}
	disjoint, err := other.TryAcquire(t.Context(), intent("disjoint"))
	if err != nil {
		t.Fatalf("unrelated file blocked: %v", err)
	}
	disjoint.End(NotExecuted)
	unblock()
	select {
	case <-released:
	case <-time.After(10 * time.Second):
		t.Fatal("completed root wait did not release the lease")
	}
	next, err := other.TryAcquire(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	defer next.End(NotExecuted)
	lease.End(NotExecuted)
	if probe, err := c.TryAcquire(t.Context(), request); !errors.Is(err, ErrBusy) {
		if probe != nil {
			probe.End(NotExecuted)
		}
		t.Fatalf("old cleanup released a new transaction: %v", err)
	}
}
