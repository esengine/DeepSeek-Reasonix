package control

import (
	"context"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/event"
)

type approvalBlockingRunner struct {
	c *Controller
}

func (r *approvalBlockingRunner) Run(ctx context.Context, _ string) error {
	_, _, err := gateApprover{c: r.c}.Approve(ctx, "bash", "go test ./...", nil)
	return err
}

type askBlockingRunner struct {
	c *Controller
}

func (r *askBlockingRunner) Run(ctx context.Context, _ string) error {
	_, err := r.c.Ask(ctx, []event.AskQuestion{{
		ID:      "choice",
		Prompt:  "Pick one",
		Options: []event.AskOption{{Label: "A"}, {Label: "B"}},
	}})
	return err
}

func TestCancelClearsPendingApprovalRuntimeStatus(t *testing.T) {
	approvals := make(chan event.Approval, 1)
	done := make(chan event.Event, 1)
	c := New(Options{Sink: event.FuncSink(func(e event.Event) {
		switch e.Kind {
		case event.ApprovalRequest:
			approvals <- e.Approval
		case event.TurnDone:
			done <- e
		}
	})})
	runner := &approvalBlockingRunner{c: c}
	c.runner = runner

	c.Send("needs approval")
	select {
	case <-approvals:
	case <-time.After(30 * time.Second):
		t.Fatal("timed out waiting for approval request")
	}
	if st := c.RuntimeStatus(); !st.Running || !st.PendingPrompt || !st.Cancellable || st.CancelRequested {
		t.Fatalf("status before cancel = %+v, want running pending cancellable", st)
	}

	c.Cancel()
	c.Cancel()
	assertCancelClearedPendingRuntimeStatus(t, c.RuntimeStatus())
	waitTurnDoneEvent(t, done)
	// TurnDone is emitted inside the finishing window; Running() (and the
	// RuntimeStatus it feeds) stays true until finishGuardedTurn's deferred
	// clear runs. Wait for the gate to reopen before asserting idle.
	waitIdle(t, c)
	if st := c.RuntimeStatus(); st.Running || st.PendingPrompt || st.Cancellable || st.CancelRequested {
		t.Fatalf("status after turn done = %+v, want idle", st)
	}
}

func TestCancelClearsPendingAskRuntimeStatus(t *testing.T) {
	asks := make(chan event.Ask, 1)
	done := make(chan event.Event, 1)
	c := New(Options{Sink: event.FuncSink(func(e event.Event) {
		switch e.Kind {
		case event.AskRequest:
			asks <- e.Ask
		case event.TurnDone:
			done <- e
		}
	})})
	runner := &askBlockingRunner{c: c}
	c.runner = runner

	c.Send("ask user")
	select {
	case <-asks:
	case <-time.After(30 * time.Second):
		t.Fatal("timed out waiting for ask request")
	}
	if st := c.RuntimeStatus(); !st.Running || !st.PendingPrompt || !st.Cancellable || st.CancelRequested {
		t.Fatalf("status before cancel = %+v, want running pending cancellable", st)
	}

	c.Cancel()
	assertCancelClearedPendingRuntimeStatus(t, c.RuntimeStatus())
	waitTurnDoneEvent(t, done)
	// TurnDone is emitted inside the finishing window; Running() (and the
	// RuntimeStatus it feeds) stays true until finishGuardedTurn's deferred
	// clear runs. Wait for the gate to reopen before asserting idle.
	waitIdle(t, c)
	if st := c.RuntimeStatus(); st.Running || st.PendingPrompt || st.Cancellable || st.CancelRequested {
		t.Fatalf("status after turn done = %+v, want idle", st)
	}
}

func TestTryCancelIsStrictAndIdempotent(t *testing.T) {
	c := New(Options{Runner: appendingRunner{session: agent.NewSession("")}, Sink: event.Discard})
	if got := c.TryCancel(); got != CancelNotActive {
		t.Fatalf("idle TryCancel = %q", got)
	}

	started := make(chan struct{})
	release := make(chan struct{})
	c.runGuarded(func(ctx context.Context) error {
		close(started)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-release:
			return nil
		}
	})
	<-started
	if got := c.TryCancel(); got != CancelRequestedNow {
		t.Fatalf("first TryCancel = %q", got)
	}
	if got := c.TryCancel(); got != CancelAlreadyRequested {
		t.Fatalf("second TryCancel = %q", got)
	}
	close(release)
	waitIdleAdmission(t, c)
	if got := c.TryCancel(); got != CancelNotActive {
		t.Fatalf("finished TryCancel = %q", got)
	}
}

func assertCancelClearedPendingRuntimeStatus(t *testing.T, st RuntimeStatus) {
	t.Helper()
	if st.PendingPrompt {
		t.Fatalf("status immediately after cancel = %+v, want pending prompt cleared", st)
	}
	if st.Running {
		if !st.Cancellable || !st.CancelRequested {
			t.Fatalf("status immediately after cancel = %+v, want running cancelling without pending prompt", st)
		}
		return
	}
	if st.Cancellable || st.CancelRequested {
		t.Fatalf("status immediately after cancel = %+v, want idle when turn already completed", st)
	}
}

func waitTurnDoneEvent(t *testing.T, done <-chan event.Event) {
	t.Helper()
	select {
	case e := <-done:
		if e.Kind != event.TurnDone {
			t.Fatalf("event = %v, want TurnDone", e.Kind)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("timed out waiting for turn_done")
	}
}
