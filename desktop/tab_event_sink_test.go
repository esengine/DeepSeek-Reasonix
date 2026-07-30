package main

import (
	"context"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/event"
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
