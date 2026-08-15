package main

import (
	"context"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/eventwire"
)

type closeTrackingSink struct {
	closed atomic.Bool
}

func (s *closeTrackingSink) Emit(event.Event) {}

func (s *closeTrackingSink) Close() {
	s.closed.Store(true)
}

type blockingCloseTrackingSink struct {
	closeTrackingSink
	entered chan struct{}
	release chan struct{}
}

func (s *blockingCloseTrackingSink) Emit(event.Event) {
	close(s.entered)
	<-s.release
}

func TestTabEventSinkSetBotSinkClosesPreviousSink(t *testing.T) {
	sink := &tabEventSink{}
	first := &closeTrackingSink{}
	second := &closeTrackingSink{}

	sink.SetBotSink(first)
	if first.closed.Load() {
		t.Fatal("newly attached sink was closed")
	}

	sink.SetBotSink(second)
	if !first.closed.Load() {
		t.Fatal("previous sink was not closed when replaced")
	}
	if second.closed.Load() {
		t.Fatal("replacement sink was closed too early")
	}

	sink.SetBotSink(nil)
	if !second.closed.Load() {
		t.Fatal("second sink was not closed when cleared")
	}
}

func TestTabEventSinkOldTurnDoneDoesNotClearReplacement(t *testing.T) {
	sink := &tabEventSink{}
	old := &blockingCloseTrackingSink{
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	replacement := &closeTrackingSink{}

	if !sink.tryBeginTurn() {
		t.Fatal("failed to reserve initial turn")
	}
	sink.SetBotSink(old)
	done := make(chan struct{})
	go func() {
		sink.Emit(event.Event{Kind: event.TurnDone})
		close(done)
	}()
	select {
	case <-old.entered:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("old forwarder did not receive TurnDone")
	}

	sink.SetBotSink(replacement)
	if sink.tryBeginTurn() {
		t.Fatal("new turn admitted before old TurnDone completed")
	}
	close(old.release)
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("TurnDone did not finish")
	}

	if replacement.closed.Load() {
		t.Fatal("old TurnDone cleared the replacement forwarder")
	}
	got, _ := sink.botSinkSnapshot()
	if got != replacement {
		t.Fatalf("attached forwarder = %T, want replacement", got)
	}
	if !sink.tryBeginTurn() {
		t.Fatal("next turn was not admitted after TurnDone completed")
	}
	sink.cancelTurnStart()
	sink.SetBotSink(nil)
}

func TestTabEventSinkDoesNotBlockOnRuntimeEventsEmit(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	delivered := make(chan string, 2)
	var calls atomic.Int32

	sink := &tabEventSink{tabID: "tab", ctx: context.Background()}
	sink.runtimeEvents.emit = func(_ context.Context, name string, payload ...any) {
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

	wrapped := event.Sync(sink)
	wrapped.Emit(event.Event{Kind: event.Text, Text: "one"})

	select {
	case <-entered:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("first runtime emit did not start")
	}

	done := make(chan struct{})
	go func() {
		wrapped.Emit(event.Event{Kind: event.Text, Text: "two"})
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
	allDelivered := make(chan struct{}, 1)
	var calls atomic.Int32
	var runtimeCalls atomic.Int32
	var legacyCalls atomic.Int32

	app := &App{ctx: context.Background()}
	app.runtimeEvents.emit = func(_ context.Context, name string, payload ...any) {
		switch name {
		case "project-tree:runtime-changed":
			runtimeCalls.Add(1)
			if len(payload) != 1 {
				t.Errorf("runtime payload count = %d, want 1", len(payload))
			} else if event, ok := payload[0].(ProjectTreeRuntimeSnapshot); !ok || event.Topics == nil || event.Revision == 0 {
				t.Errorf("runtime payload = %#v, want a versioned snapshot with [] topics", payload[0])
			}
		case "project-tree:changed":
			legacyCalls.Add(1)
			if len(payload) != 0 {
				t.Errorf("legacy payload count = %d, want 0", len(payload))
			}
		default:
			t.Errorf("event name = %q, want project-tree:runtime-changed or project-tree:changed", name)
		}
		if runtimeCalls.Load() >= 2 && legacyCalls.Load() >= 2 {
			select {
			case allDelivered <- struct{}{}:
			default:
			}
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
	select {
	case <-allDelivered:
		return
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("runtime emit calls = %d (runtime=%d legacy=%d), want two broadcasts on both contracts", calls.Load(), runtimeCalls.Load(), legacyCalls.Load())
	}
}

func TestAsyncRuntimeEmitterDrainsBacklogInOrder(t *testing.T) {
	const backlog = 256

	entered := make(chan struct{})
	release := make(chan struct{})
	delivered := make(chan string, backlog)
	var calls atomic.Int32

	emitter := &asyncRuntimeEmitter{}
	emitter.emit = func(_ context.Context, _ string, payload ...any) {
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
	for i := range backlog {
		emitter.Emit(ctx, "agent:event", strconv.Itoa(i))
	}

	select {
	case <-entered:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("first runtime emit did not start")
	}
	close(release)

	for i := range backlog {
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

// TestAsyncRuntimeEmitterDropsStreamDeltasUnderPressure verifies that when the
// queue is saturated the emitter drops stream deltas (recoverable via the
// later message event) but never drops structural events.
func TestAsyncRuntimeEmitterDropsStreamDeltasUnderPressure(t *testing.T) {
	emitter := &asyncRuntimeEmitter{}
	var mu sync.Mutex
	delivered := []string{}

	ctx := context.Background()
	// A stalled emit (simulated by blocking the first call) plus a full queue
	// of stream deltas: the next text/reasoning delta must be dropped, while
	// structural events are always accepted.
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	emitter.emit = func(_ context.Context, _ string, payload ...any) {
		once.Do(func() {
			close(entered)
			<-release
		})
		var kind string
		if len(payload) > 0 {
			kind = wireEventKindOf(payload[0])
		}
		mu.Lock()
		delivered = append(delivered, kind)
		mu.Unlock()
	}

	for i := 0; i < asyncEmitterMaxQueue+1; i++ {
		emitter.Emit(ctx, "agent:event", wireEventTab{Event: eventwire.Event{Kind: "text"}, TabID: "t"})
	}
	select {
	case <-entered:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("first emit did not start")
	}

	// Queue is saturated: a stream delta is dropped.
	emitter.Emit(ctx, "agent:event", wireEventTab{Event: eventwire.Event{Kind: "text"}, TabID: "t"})
	if got := emitter.droppedStreamEvents.Load(); got != 1 {
		t.Fatalf("droppedStreamEvents = %d, want 1", got)
	}

	// A structural event is kept even under saturation.
	emitter.Emit(ctx, "agent:event", wireEventTab{Event: eventwire.Event{Kind: "turn_done"}, TabID: "t"})

	close(release)
	// The queue is drained asynchronously; poll until the structural event
	// arrives (it sits behind thousands of queued stream deltas).
	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		foundDone := false
		for _, d := range delivered {
			if d == "turn_done" {
				foundDone = true
			}
		}
		mu.Unlock()
		if foundDone {
			break
		}
		if time.Now().After(deadline) {
			mu.Lock()
			defer mu.Unlock()
			t.Fatalf("structural turn_done event was dropped under saturation; delivered=%v", delivered)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
