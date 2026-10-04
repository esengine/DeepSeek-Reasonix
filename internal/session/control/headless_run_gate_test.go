package control

import (
	"context"
	"errors"
	"reasonix/internal/base/testenv"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/state/sessionstore"
)

// The headless Run holds the turn gate like every other turn: it waits out a
// turn already in flight, and while it runs no inbox or submitted turn starts
// on the same executor.
func TestHeadlessRunTakesTheTurnGate(t *testing.T) {
	sess := sessionstore.NewSession("sys")
	release := make(chan struct{})
	c := New(Options{Runner: blockingRunner{session: sess, release: release}, Sink: event.Discard})
	t.Cleanup(c.Close)

	inFlight := make(chan struct{})
	if got := c.runGuarded(func(context.Context) error { <-inFlight; return nil }); got != turnStarted {
		t.Fatalf("guarded admission = %v", got)
	}
	runDone := make(chan error, 1)
	go func() { runDone <- c.Run(context.Background(), "headless") }()
	time.Sleep(3 * syncTurnPoll)
	if n := len(sess.Snapshot()); n != 1 {
		t.Fatalf("Run reached the runner while another turn held the gate (%d messages)", n)
	}

	close(inFlight)
	deadline := time.After(testenv.Budget(t))
	for len(sess.Snapshot()) < 2 {
		select {
		case <-deadline:
			t.Fatal("Run never started after the gate opened")
		case <-time.After(5 * time.Millisecond):
		}
	}
	if got := c.runGuarded(func(context.Context) error { return nil }); got != turnDroppedRunning {
		t.Fatalf("a guarded turn was admitted during Run: %v", got)
	}
	close(release)
	if err := <-runDone; err != nil {
		t.Fatalf("Run = %v", err)
	}
}

func TestHeadlessRunStopsWaitingWhenItsCallerDoes(t *testing.T) {
	c := New(Options{Runner: blockingRunner{session: sessionstore.NewSession("sys"), release: make(chan struct{})}, Sink: event.Discard})
	t.Cleanup(c.Close)
	inFlight := make(chan struct{})
	defer close(inFlight)
	c.runGuarded(func(context.Context) error { <-inFlight; return nil })
	ctx, cancel := context.WithTimeout(context.Background(), 5*syncTurnPoll)
	defer cancel()
	if err := c.Run(ctx, "headless"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run = %v, want the caller's deadline", err)
	}
}
