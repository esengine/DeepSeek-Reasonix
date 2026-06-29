package main

import (
	"context"
	"sync"
	"testing"

	"reasonix/internal/event"
)

// All tabEventSink context mutations go through the locked setContext /
// clearContext accessors (no bare s.ctx = ... writes that data-race the
// s.context() reads in emitRuntimeEvent). After clearContext the sink stops
// emitting — emitRuntimeEvent sees a nil ctx and no-ops — and the queued
// emitter is drained, so a detached/backgrounded session can't flush stale
// events onto the now-rebound tab (#5352: stale "AI 不断输出" on the visible
// session after rapid session switching).
func TestTabEventSinkClearContextStopsEmission(t *testing.T) {
	var mu sync.Mutex
	var emitted int
	s := &tabEventSink{tabID: "t"}
	s.runtimeEvents.emit = func(context.Context, string, ...interface{}) {
		mu.Lock()
		emitted++
		mu.Unlock()
	}

	s.setContext(context.Background())
	if s.context() == nil {
		t.Fatal("setContext did not install the context")
	}

	s.clearContext()
	if s.context() != nil {
		t.Fatal("clearContext did not clear the context")
	}

	// An emit after clearContext must not reach the runtime bridge.
	s.emitRuntimeEvent(eventChannel, toWireTab(event.Event{}, s.tabID))

	mu.Lock()
	defer mu.Unlock()
	if emitted != 0 {
		t.Fatalf("sink emitted %d events after clearContext, want 0", emitted)
	}
}
