package remote

import (
	"bytes"
	"context"
	"sync/atomic"
	"testing"
	"time"
)

type hangingSession struct {
	release chan struct{}
	closed  atomic.Int32
}

func (s *hangingSession) Run(string) error { <-s.release; return nil }
func (s *hangingSession) Close() error {
	if s.closed.Add(1) == 1 {
		close(s.release)
	}
	return nil
}
func (s *hangingSession) capture(_, _ *bytes.Buffer) {}

func TestRunExecClosesSessionWhenContextEnds(t *testing.T) {
	s := &hangingSession{release: make(chan struct{})}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err := runExec(ctx, s, "sleep 100")
	if err == nil || ctx.Err() == nil {
		t.Fatalf("err = %v, want the context's", err)
	}
	if s.closed.Load() == 0 {
		t.Fatal("session left open after the context ended")
	}
}
