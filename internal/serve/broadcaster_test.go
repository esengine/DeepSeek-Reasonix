package serve

import (
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/eventwire"
)

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

	for i, ch := range []<-chan []byte{a, d} {
		var w eventwire.Event
		if err := json.Unmarshal(<-ch, &w); err != nil {
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

	s := string(<-ch)
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

func TestBroadcasterDropsSlowSubscriber(t *testing.T) {
	b := NewBroadcaster()
	ch, cancel := b.Subscribe()
	defer cancel()
	// Overfill far past the 64-slot buffer without reading; Emit must not block.
	for i := 0; i < 1000; i++ {
		b.Emit(event.Event{Kind: event.Text, Text: "x"})
	}
	if len(ch) == 0 {
		t.Error("expected some buffered frames")
	}
}
