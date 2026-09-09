package control

import (
	"context"
	"testing"
	"time"

	"reasonix/internal/event"
)

// Stop must reach the turn context before the cancelling status crosses the
// synchronous event barrier; a stalled sink cannot be allowed to keep the
// provider stream or a tool process alive.
func TestCancelSignalsTurnBeforeStatusBarrier(t *testing.T) {
	releaseStatus := make(chan struct{})
	statusEntered := make(chan struct{}, 1)
	c := New(Options{Sink: event.FuncSink(func(e event.Event) {
		if e.Kind == event.TurnStatusChanged && e.Status == event.TurnCancelling {
			statusEntered <- struct{}{}
			<-releaseStatus
		}
	})})
	t.Cleanup(c.Close)

	turnCtxDone := make(chan struct{})
	releaseTurn := make(chan struct{})
	started := make(chan struct{})
	c.runGuarded(func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		close(turnCtxDone)
		// Hold the turn open so TurnDone cannot race ahead of the cancelling
		// status; the assertion is about ordering inside Cancel itself.
		<-releaseTurn
		return ctx.Err()
	})
	<-started
	defer close(releaseTurn)

	cancelReturned := make(chan struct{})
	go func() {
		c.Cancel()
		close(cancelReturned)
	}()
	select {
	case <-turnCtxDone:
	case <-time.After(5 * time.Second):
		close(releaseStatus)
		t.Fatal("turn context was not cancelled before the status barrier")
	}
	select {
	case <-statusEntered:
	case <-time.After(5 * time.Second):
		close(releaseStatus)
		t.Fatal("cancel never emitted the cancelling status")
	}
	select {
	case <-cancelReturned:
		close(releaseStatus)
		t.Fatal("Cancel returned before the status barrier drained")
	default:
	}
	close(releaseStatus)
	select {
	case <-cancelReturned:
	case <-time.After(5 * time.Second):
		t.Fatal("Cancel did not return after the barrier was released")
	}
}

// A cancelling status stamped for a turn that already terminated must not turn
// the next admitted turn into a permanently "cancelling" one.
func TestStaleCancellingStatusDoesNotStickToNextTurn(t *testing.T) {
	dir := t.TempDir()
	done := make(chan event.Event, 4)
	c := New(Options{SessionDir: dir, SessionPath: dir + "/session.jsonl", Sink: event.FuncSink(func(e event.Event) {
		if e.Kind == event.TurnDone {
			done <- e
		}
	})})
	t.Cleanup(c.Close)

	c.runGuarded(func(context.Context) error { return nil })
	first := waitTurnDoneEvent(t, done)
	if first.TurnID == "" {
		t.Fatal("first turn has no ledger id")
	}

	started := make(chan struct{})
	c.runGuarded(func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	})
	<-started
	c.emitTurnStatus(event.TurnCancelling, first.TurnID)
	if st := c.RuntimeStatus(); st.Status == event.TurnCancelling || st.CancelRequested {
		t.Fatalf("stale cancelling status leaked into the next turn: %+v", st)
	}
	c.Cancel()
	if second := waitTurnDoneEvent(t, done); second.Status != event.TurnInterrupted {
		t.Fatalf("second turn terminal = %q, want interrupted", second.Status)
	}
}
