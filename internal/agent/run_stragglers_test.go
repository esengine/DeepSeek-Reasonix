package agent

import (
	"context"
	"testing"
	"time"
)

// TestStragglerDrainWaitsForAbandonedGoroutines pins the turn boundary: the
// next turn must not zero per-turn state while a batch goroutine still reads it.
func TestStragglerDrainWaitsForAbandonedGoroutines(t *testing.T) {
	var s runStragglers
	s.enter()
	drained := make(chan struct{})
	go func() {
		s.drain(context.Background(), time.Second)
		close(drained)
	}()
	select {
	case <-drained:
		t.Fatal("drain returned while a batch goroutine was still live")
	case <-time.After(20 * time.Millisecond):
	}
	s.leave()
	select {
	case <-drained:
	case <-time.After(time.Second):
		t.Fatal("drain did not return after the goroutine left")
	}
}
