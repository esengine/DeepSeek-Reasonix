package main

import (
	"context"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/event"
)

func TestTabEventSinkDoesNotBlockOnRuntimeEventsEmit(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	delivered := make(chan string, 2)
	var calls atomic.Int32

	sink := &tabEventSink{tabID: "tab", ctx: context.Background()}
	sink.runtimeEvents.emit = func(_ context.Context, name string, payload ...interface{}) {
		if name != eventChannel {
			t.Errorf("event name = %q, want %q", name, eventChannel)
		}
		if len(payload) != 1 {
			t.Errorf("payload count = %d, want 1", len(payload))
			return
		}
		wire, ok := payload[0].(wireEventTab)
		if !ok {
			t.Errorf("payload type = %T, want wireEventTab", payload[0])
			return
		}
		delivered <- wire.Text
		if calls.Add(1) == 1 {
			close(entered)
			<-release
		}
	}

	// Use Notice events (non-coalescable) so they go through the async emitter
	// immediately. Text/Reasoning events are buffered by the coalescer and
	// would not reach runtime.EventsEmit until a flush trigger arrives.
	wrapped := event.Sync(sink)
	wrapped.Emit(event.Event{Kind: event.Notice, Text: "one"})

	select {
	case <-entered:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("first runtime emit did not start")
	}

	done := make(chan struct{})
	go func() {
		wrapped.Emit(event.Event{Kind: event.Notice, Text: "two"})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("second event blocked behind runtime EventsEmit")
	}

	close(release)
	if got := <-delivered; got != "one" {
		t.Fatalf("first delivered event = %q, want one", got)
	}
	select {
	case got := <-delivered:
		if got != "two" {
			t.Fatalf("second delivered event = %q, want two", got)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("second queued event was not delivered")
	}
}

func TestEmitProjectTreeChangedDoesNotBlockOnRuntimeEventsEmit(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32

	app := &App{ctx: context.Background()}
	app.runtimeEvents.emit = func(_ context.Context, name string, payload ...interface{}) {
		if name != "project-tree:changed" {
			t.Errorf("event name = %q, want project-tree:changed", name)
		}
		if len(payload) != 0 {
			t.Errorf("payload count = %d, want 0", len(payload))
		}
		if calls.Add(1) == 1 {
			close(entered)
			<-release
		}
	}

	app.emitProjectTreeChanged()
	select {
	case <-entered:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("first project tree runtime emit did not start")
	}

	done := make(chan struct{})
	go func() {
		app.emitProjectTreeChanged()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("project tree event blocked behind runtime EventsEmit")
	}

	close(release)
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if calls.Load() >= 2 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("runtime emit calls = %d, want at least 2", calls.Load())
}

// TestTabEventSinkCoalescesTextDeltas verifies that consecutive Text/Reasoning
// deltas are buffered and coalesced into fewer events before hitting the Wails
// bridge. A non-Text/Reasoning event flushes the buffer.
func TestTabEventSinkCoalescesTextDeltas(t *testing.T) {
	var mu sync.Mutex
	var delivered []string
	sink := &tabEventSink{tabID: "tab", ctx: context.Background()}
	sink.runtimeEvents.emit = func(_ context.Context, _ string, payload ...interface{}) {
		wire, ok := payload[0].(wireEventTab)
		if !ok {
			return
		}
		mu.Lock()
		delivered = append(delivered, wire.Kind+":"+wire.Text)
		mu.Unlock()
	}

	// Send 5 Text deltas + 3 Reasoning deltas + 1 flush trigger (Notice).
	sink.Emit(event.Event{Kind: event.Text, Text: "H"})
	sink.Emit(event.Event{Kind: event.Text, Text: "i"})
	sink.Emit(event.Event{Kind: event.Text, Text: "!"})
	sink.Emit(event.Event{Kind: event.Reasoning, Text: "th"})
	sink.Emit(event.Event{Kind: event.Reasoning, Text: "ink"})
	sink.Emit(event.Event{Kind: event.Reasoning, Text: "ing"})
	// No events should have been delivered yet (all buffered).
	mu.Lock()
	if len(delivered) != 0 {
		t.Fatalf("expected 0 delivered before flush, got %d: %v", len(delivered), delivered)
	}
	mu.Unlock()

	// Flush trigger: Notice is non-coalescable, so it flushes the buffer.
	sink.Emit(event.Event{Kind: event.Notice, Text: "done"})

	// Wait for the async emitter to drain.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(delivered)
		mu.Unlock()
		if n >= 3 { // coalesced Text + coalesced Reasoning + Notice
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	// Expect: 1 merged Text ("Hi!"), 1 merged Reasoning ("thinking"), 1 Notice ("done").
	if len(delivered) != 3 {
		t.Fatalf("expected 3 delivered events (coalesced), got %d: %v", len(delivered), delivered)
	}
	if delivered[0] != "text:Hi!" {
		t.Fatalf("delivered[0] = %q, want text:Hi!", delivered[0])
	}
	if delivered[1] != "reasoning:thinking" {
		t.Fatalf("delivered[1] = %q, want reasoning:thinking", delivered[1])
	}
	if delivered[2] != "notice:done" {
		t.Fatalf("delivered[2] = %q, want notice:done", delivered[2])
	}
}

func TestAsyncRuntimeEmitterDrainsBacklogInOrder(t *testing.T) {
	const backlog = 256

	entered := make(chan struct{})
	release := make(chan struct{})
	delivered := make(chan string, backlog)
	var calls atomic.Int32

	emitter := &asyncRuntimeEmitter{}
	emitter.emit = func(_ context.Context, _ string, payload ...interface{}) {
		if len(payload) != 1 {
			t.Errorf("payload count = %d, want 1", len(payload))
			return
		}
		value, ok := payload[0].(string)
		if !ok {
			t.Errorf("payload type = %T, want string", payload[0])
			return
		}
		delivered <- value
		if calls.Add(1) == 1 {
			close(entered)
			<-release
		}
	}

	ctx := context.Background()
	for i := 0; i < backlog; i++ {
		emitter.Emit(ctx, "agent:event", strconv.Itoa(i))
	}

	select {
	case <-entered:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("first runtime emit did not start")
	}
	close(release)

	for i := 0; i < backlog; i++ {
		select {
		case got := <-delivered:
			if want := strconv.Itoa(i); got != want {
				t.Fatalf("delivered[%d] = %q, want %q", i, got, want)
			}
		case <-time.After(500 * time.Millisecond):
			t.Fatalf("timed out waiting for delivered event %d", i)
		}
	}
}
