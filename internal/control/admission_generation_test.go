package control

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/extension"
)

func TestAdmitGuardedTurnRejectsDrainingGeneration(t *testing.T) {
	// Publish generation 2 so gen 1 is stale for admission.
	owner := extension.NewRuntimeOwner()
	owner.Gate.Publish(2)

	var notices atomic.Int32
	var c *Controller
	c = New(Options{
		Sink: event.FuncSink(func(ev event.Event) {
			if ev.Kind == event.Notice {
				_ = c.RuntimeGeneration() // must not re-enter while Controller.mu is held
				notices.Add(1)
			}
		}),
		RuntimeGeneration: 1,
		RuntimeOwner:      owner,
	})
	// Ensure we don't leak a controller without Close.
	t.Cleanup(func() { c.Close() })

	result := make(chan admissionResult, 1)
	go func() {
		result <- c.runGuarded(func(context.Context) error {
			t.Error("body must not run on a draining generation")
			return nil
		})
	}()
	select {
	case got := <-result:
		if got != turnDroppedDraining {
			t.Fatalf("admission = %v, want turnDroppedDraining", got)
		}
	case <-time.After(time.Second):
		t.Fatal("drain notice deadlocked while re-entering the controller")
	}
	if notices.Load() == 0 {
		t.Fatal("expected drain notice")
	}
	if err := c.RunTurn(context.Background(), "blocked"); !errors.Is(err, ErrRuntimeDraining) {
		t.Fatalf("RunTurn error = %v, want ErrRuntimeDraining", err)
	}
	if extension.DefaultLifecycleMetrics.AdmissionRejected.Load() == 0 {
		t.Fatal("expected AdmissionRejected metric")
	}
}

func TestAdmitGuardedTurnAllowsPublishedGeneration(t *testing.T) {
	owner := extension.NewRuntimeOwner()
	owner.Gate.Publish(9)
	c := New(Options{RuntimeGeneration: 9, RuntimeOwner: owner, Sink: event.Discard})
	t.Cleanup(func() { c.Close() })
	done := make(chan struct{})
	got := c.runGuarded(func(context.Context) error {
		close(done)
		return nil
	})
	if got != turnStarted {
		t.Fatalf("admission = %v, want turnStarted", got)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("turn body did not run")
	}
}

type runtimeOwnerRunner struct {
	owner *extension.RuntimeOwner
}

func (r *runtimeOwnerRunner) Run(ctx context.Context, _ string) error {
	r.owner = extension.RuntimeOwnerFromContext(ctx)
	return nil
}

func TestRunTurnBindsRuntimeOwnerToRunnerContext(t *testing.T) {
	owner := extension.NewRuntimeOwner()
	owner.Gate.Publish(4)
	runner := &runtimeOwnerRunner{}
	c := New(Options{Runner: runner, RuntimeGeneration: 4, RuntimeOwner: owner, Sink: event.Discard})
	t.Cleanup(c.Close)

	if err := c.RunTurn(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}
	if runner.owner != owner {
		t.Fatal("turn context did not carry the controller runtime owner")
	}
}
