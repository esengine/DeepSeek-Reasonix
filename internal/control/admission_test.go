package control

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/event"
)

// holdFinishingWindow returns a sink that blocks inside the FIRST TurnDone
// delivery until release is closed, holding the controller's finishing window
// deterministically open so tests can place submits inside it. Later
// TurnDones pass through unblocked.
func holdFinishingWindow(release <-chan struct{}, entered chan<- struct{}, events chan<- event.Event) event.Sink {
	var first int32
	return event.FuncSink(func(e event.Event) {
		if e.Kind == event.TurnDone && atomic.AddInt32(&first, 1) == 1 {
			entered <- struct{}{}
			<-release
		}
		if events != nil {
			select {
			case events <- e:
			default:
			}
		}
	})
}

// TestParkedTurnsRunFIFO pins ordering: several submits landing inside one
// finishing window run in arrival order, one per window close, none lost.
func TestParkedTurnsRunFIFO(t *testing.T) {
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	c := New(Options{Sink: holdFinishingWindow(release, entered, nil)})

	c.runGuarded(func(context.Context) error { return nil })
	<-entered

	var order []int
	ran := make(chan int, 3)
	for i := 1; i <= 3; i++ {
		i := i
		if got := c.runGuarded(func(context.Context) error {
			ran <- i
			return nil
		}); got != turnParked {
			t.Fatalf("submit %d admission = %v, want turnParked", i, got)
		}
	}
	close(release)

	deadline := time.After(30 * time.Second)
	for len(order) < 3 {
		select {
		case i := <-ran:
			order = append(order, i)
		case <-deadline:
			t.Fatalf("parked turns did not all run; got order %v", order)
		}
	}
	if order[0] != 1 || order[1] != 2 || order[2] != 3 {
		t.Fatalf("parked turns ran out of order: %v", order)
	}
}

// TestSubmitDuringRotationEmitsNotice pins the rotating posture: the input's
// intended session is ambiguous while the executor session is being swapped,
// so the submit is refused with a user-visible notice instead of silently
// dropped (the caller should resend against the session they can now see).
func TestSubmitDuringRotationEmitsNotice(t *testing.T) {
	events := make(chan event.Event, 8)
	c := New(Options{Sink: event.FuncSink(func(e event.Event) {
		select {
		case events <- e:
		default:
		}
	})})

	c.mu.Lock()
	c.rotating = true
	c.mu.Unlock()

	bodyRan := make(chan struct{}, 1)
	if got := c.runGuarded(func(context.Context) error {
		bodyRan <- struct{}{}
		return nil
	}); got != turnDroppedRotating {
		t.Fatalf("admission during rotation = %v, want turnDroppedRotating", got)
	}

	select {
	case e := <-events:
		if e.Kind != event.Notice || e.Level != event.LevelWarn || !strings.Contains(e.Text, "resend") {
			t.Fatalf("event = %+v, want a warn notice asking to resend", e)
		}
	case <-time.After(time.Second):
		t.Fatal("no notice emitted for a rotation-dropped submit")
	}
	select {
	case <-bodyRan:
		t.Fatal("rotation-dropped body must not run")
	case <-time.After(50 * time.Millisecond):
	}

	c.mu.Lock()
	c.rotating = false
	c.mu.Unlock()
}

// TestSubmitWhileRunningStaysSilentNoOp pins the running posture: unchanged
// from the historical contract — frontends own the steer/queue UX, internal
// opportunistic callers rely on the quiet no-op.
func TestSubmitWhileRunningStaysSilentNoOp(t *testing.T) {
	block := make(chan struct{})
	var notices int32
	c := New(Options{Sink: event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice {
			atomic.AddInt32(&notices, 1)
		}
	})})

	started := make(chan struct{})
	c.runGuarded(func(context.Context) error {
		close(started)
		<-block
		return nil
	})
	<-started

	if got := c.runGuarded(func(context.Context) error { return nil }); got != turnDroppedRunning {
		t.Fatalf("admission while running = %v, want turnDroppedRunning", got)
	}
	if n := atomic.LoadInt32(&notices); n != 0 {
		t.Fatalf("running drop should stay silent, got %d notices", n)
	}
	close(block)
	waitIdleAdmission(t, c)
}

// TestCloseDiscardsParkedTurns pins teardown: a turn parked in the finishing
// window must not start against a controller that has been closed.
func TestCloseDiscardsParkedTurns(t *testing.T) {
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	c := New(Options{Sink: holdFinishingWindow(release, entered, nil)})

	c.runGuarded(func(context.Context) error { return nil })
	<-entered

	parkedRan := make(chan struct{}, 1)
	if got := c.runGuarded(func(context.Context) error {
		parkedRan <- struct{}{}
		return nil
	}); got != turnParked {
		t.Fatalf("admission = %v, want turnParked", got)
	}

	c.Close()
	close(release)

	select {
	case <-parkedRan:
		t.Fatal("parked turn ran after Close")
	case <-time.After(200 * time.Millisecond):
	}
}

// TestCloseSealsAdmissionDuringFinishingWindow pins the terminal-state
// ordering the first review round flagged: Close clears the parked queue, but
// a submit arriving AFTER that — while the old turn's TurnDone delivery is
// still in flight — must be rejected outright, not parked and started against
// freed resources when the window closes.
func TestCloseSealsAdmissionDuringFinishingWindow(t *testing.T) {
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	c := New(Options{Sink: holdFinishingWindow(release, entered, nil)})

	c.runGuarded(func(context.Context) error { return nil })
	<-entered // finishing window is now held open

	c.Close() // seals admission; parked queue is empty at this instant

	lateRan := make(chan struct{}, 1)
	if got := c.runGuarded(func(context.Context) error {
		lateRan <- struct{}{}
		return nil
	}); got != turnDroppedClosed {
		t.Fatalf("submit after Close during finishing window = %v, want turnDroppedClosed", got)
	}

	close(release) // window closes; the drain must start nothing
	select {
	case <-lateRan:
		t.Fatal("submit accepted after Close ran when finishing window closed")
	case <-time.After(200 * time.Millisecond):
	}
}

// TestRunTurnRefusedDuringFinishingWindow pins the synchronous gate: RunTurn
// must not start inside the previous turn's TurnDone delivery window — that
// would recreate the completion/transport crosstalk the window prevents.
func TestRunTurnRefusedDuringFinishingWindow(t *testing.T) {
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	c := New(Options{Sink: holdFinishingWindow(release, entered, nil)})

	c.runGuarded(func(context.Context) error { return nil })
	<-entered // finishing window is now held open

	errCh := make(chan error, 1)
	go func() { errCh <- c.RunTurn(context.Background(), "sync input") }()
	select {
	case err := <-errCh:
		if err != ErrTurnRunning {
			t.Fatalf("RunTurn during finishing window = %v, want ErrTurnRunning", err)
		}
	case <-time.After(time.Second):
		t.Fatal("RunTurn did not return promptly during the finishing window")
	}
	close(release)
	waitIdleAdmission(t, c)
}

// TestRunTurnRefusedAfterClose pins the terminal state for the synchronous
// entry point too.
func TestRunTurnRefusedAfterClose(t *testing.T) {
	c := New(Options{})
	c.Close()
	if err := c.RunTurn(context.Background(), "late"); err != ErrTurnRunning {
		t.Fatalf("RunTurn after Close = %v, want ErrTurnRunning", err)
	}
}

// waitIdleAdmission polls the running||finishing admission gate; a test that
// submits or asserts idle right after TurnDone must wait the finishing window
// out (TurnDone is emitted inside it).
func waitIdleAdmission(t *testing.T, c *Controller) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for c.Running() {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for the controller to return to idle")
		}
		time.Sleep(time.Millisecond)
	}
}
