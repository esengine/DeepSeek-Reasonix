package serve

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/eventwire"
)

// read drains ch until want is seen or the deadline passes, the way a real SSE
// handler reads: blocking on the channel rather than sampling its length.
func read(t *testing.T, ch <-chan Frame, want string) (seen int) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case f, ok := <-ch:
			if !ok {
				t.Fatalf("stream closed before %q arrived (after %d frames)", want, seen)
			}
			seen++
			if strings.Contains(string(f.Data), want) {
				return seen
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %q (saw %d frames)", want, seen)
		}
	}
}

func TestBroadcasterFanOut(t *testing.T) {
	b := NewBroadcaster()
	a, ca := b.Subscribe()
	d, cd := b.Subscribe()
	defer ca()
	defer cd()

	if got := b.Subscribers(); got != 2 {
		t.Fatalf("subscribers = %d, want 2", got)
	}

	b.Emit(event.Event{Kind: event.Text, Text: "hi"})

	for i, ch := range []<-chan Frame{a, d} {
		var w eventwire.Event
		if err := json.Unmarshal((<-ch).Data, &w); err != nil {
			t.Fatalf("subscriber %d: %v", i, err)
		}
		if w.Kind != "text" || w.Text != "hi" {
			t.Errorf("subscriber %d got %+v", i, w)
		}
	}
}

func TestBroadcasterEmitsRetryingJSON(t *testing.T) {
	b := NewBroadcaster()
	ch, cancel := b.Subscribe()
	defer cancel()

	b.Emit(event.Event{Kind: event.Retrying, RetryAttempt: 3, RetryMax: 10})

	s := string((<-ch).Data)
	for _, want := range []string{`"kind":"retrying"`, `"retryAttempt":3`, `"retryMax":10`} {
		if !strings.Contains(s, want) {
			t.Fatalf("retrying broadcast JSON = %s, want it to contain %s", s, want)
		}
	}
}

func TestBroadcasterUnsubscribe(t *testing.T) {
	b := NewBroadcaster()
	_, cancel := b.Subscribe()
	if b.Subscribers() != 1 {
		t.Fatalf("want 1 subscriber")
	}
	cancel()
	if b.Subscribers() != 0 {
		t.Fatalf("unsubscribe should drop to 0, got %d", b.Subscribers())
	}
	// Emitting with no subscribers must not panic.
	b.Emit(event.Event{Kind: event.TurnDone})
}

// Droppable frames are shed rather than queued without bound: a client that
// fell behind must not have to read ten thousand stale deltas to reach the
// frame that says the turn is over.
func TestBroadcasterShedsDroppableFrames(t *testing.T) {
	b := NewBroadcaster()
	ch, cancel := b.Subscribe()
	defer cancel()
	for range 10_000 {
		b.Emit(event.Event{Kind: event.Text, Text: "x"})
	}
	b.Emit(event.Event{Kind: event.TurnDone})
	if n := read(t, ch, `"kind":"turn_done"`); n > 1000 {
		t.Errorf("client read %d frames before reaching the turn boundary", n)
	}
}

// A backlog of droppable frames must not cost the subscriber the prompt or the
// turn boundary behind it: both are states nothing later restates, and a
// frontend that misses one waits on a turn that already ended.
func TestBroadcasterKeepsPromptsBehindABacklog(t *testing.T) {
	b := NewBroadcaster()
	ch, cancel := b.Subscribe()
	defer cancel()

	for range 200 {
		b.Emit(event.Event{Kind: event.Text, Text: "x"})
	}
	b.Emit(event.Event{Kind: event.ApprovalRequest, Approval: event.Approval{ID: "a1", Tool: "bash"}})
	b.Emit(event.Event{Kind: event.TurnDone})

	read(t, ch, `"kind":"approval_request"`)
	read(t, ch, `"kind":"turn_done"`)
}

// The frames that survive keep provider order. A queue that shed droppable
// frames while letting the rest overtake them would put a result before the
// dispatch it answers.
func TestBroadcasterKeepsOrderAcrossADrop(t *testing.T) {
	b := NewBroadcaster()
	ch, cancel := b.Subscribe()
	defer cancel()

	for range 500 {
		b.Emit(event.Event{Kind: event.Text, Text: "x"})
	}
	for _, id := range []string{"t1", "t2", "t3"} {
		b.Emit(event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: id, Name: "bash"}})
	}
	read(t, ch, `"id":"t1"`)
	read(t, ch, `"id":"t2"`)
	read(t, ch, `"id":"t3"`)
}

// Emitting must never wait on a client that has stopped reading — the property
// the whole queue exists to preserve.
func TestBroadcasterEmitDoesNotBlockOnAStalledClient(t *testing.T) {
	b := NewBroadcaster()
	_, cancel := b.Subscribe()
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 10_000 {
			b.Emit(event.Event{Kind: event.Text, Text: "x"})
			b.Emit(event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: "t", Name: "bash"}})
		}
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Emit blocked on a subscriber that never read")
	}
}
