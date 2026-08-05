package control

import (
	"context"
	"errors"
	"sync"
	"testing"

	"reasonix/internal/event"
)

type completionCountingSink struct {
	mu          sync.Mutex
	completions int
}

func (*completionCountingSink) Emit(event.Event) {}

func (s *completionCountingSink) RecordTurnCompletion() {
	s.mu.Lock()
	s.completions++
	s.mu.Unlock()
}

func (s *completionCountingSink) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.completions
}

type noOpTurnRunner struct{}

func (noOpTurnRunner) Run(context.Context, string) error { return nil }

type gatedTurnRunner struct {
	started chan struct{}
	release chan struct{}
}

func (r *gatedTurnRunner) Run(ctx context.Context, _ string) error {
	close(r.started)
	select {
	case <-r.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestSynchronousControllerRunsRecordCompletion(t *testing.T) {
	sink := &completionCountingSink{}
	c := New(Options{Runner: noOpTurnRunner{}, Sink: sink})

	if err := c.Run(context.Background(), "headless"); err != nil {
		t.Fatal(err)
	}
	if err := c.RunTurn(context.Background(), "transport"); err != nil {
		t.Fatal(err)
	}
	if got := sink.count(); got != 2 {
		t.Fatalf("completion count = %d, want 2", got)
	}
}

func TestRejectedRunTurnDoesNotRecordCompletion(t *testing.T) {
	runner := &gatedTurnRunner{started: make(chan struct{}), release: make(chan struct{})}
	sink := &completionCountingSink{}
	c := New(Options{Runner: runner, Sink: sink})
	done := make(chan error, 1)
	go func() { done <- c.RunTurn(context.Background(), "first") }()
	<-runner.started

	if err := c.RunTurn(context.Background(), "second"); !errors.Is(err, ErrTurnRunning) {
		t.Fatalf("second RunTurn error = %v, want ErrTurnRunning", err)
	}
	if got := sink.count(); got != 0 {
		t.Fatalf("rejected turn recorded completion: %d", got)
	}
	close(runner.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if got := sink.count(); got != 1 {
		t.Fatalf("completion count = %d, want 1", got)
	}
}
