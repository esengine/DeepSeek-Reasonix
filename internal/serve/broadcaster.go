package serve

import (
	"encoding/json"
	"sync"

	"reasonix/internal/event"
	"reasonix/internal/eventwire"
)

// Broadcaster is the event.Sink the controller emits to in server mode. It
// marshals each event once and fans it out to every connected SSE subscriber.
// A slow subscriber's buffer is allowed to drop rather than back-pressure the
// agent goroutine — a browser that can't keep up loses intermediate frames, not
// the whole session (it can refetch /history).
type Broadcaster struct {
	mu   sync.Mutex
	subs map[chan []byte]struct{}
}

// NewBroadcaster returns an empty Broadcaster ready to accept subscribers.
func NewBroadcaster() *Broadcaster {
	return &Broadcaster{subs: map[chan []byte]struct{}{}}
}

// Emit marshals the event to JSON and delivers it to every subscriber. Drops to
// a subscriber whose buffer is full rather than blocking. A marshal failure is
// dropped silently — one bad event shouldn't stall the stream.
func (b *Broadcaster) Emit(e event.Event) {
	data, err := json.Marshal(eventwire.ToWire(e))
	if err != nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs {
		select {
		case ch <- data:
		default: // subscriber is behind; drop this frame for it
		}
	}
}

// EmitTo delivers an event only to the supplied subscriber. It is used for
// connection-local recovery frames, such as replaying a prompt to a browser
// that attached after the original event was emitted. Normal runtime events
// should continue to use Emit so every subscriber receives them.
func (b *Broadcaster) EmitTo(target <-chan []byte, e event.Event) {
	data, err := json.Marshal(eventwire.ToWire(e))
	if err != nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs {
		if (<-chan []byte)(ch) != target {
			continue
		}
		select {
		case ch <- data:
		default: // subscriber is behind; drop this frame rather than blocking.
		}
		return
	}
}

// Subscribe registers a new SSE client and returns its channel plus an
// unsubscribe func the handler must call (defer) when the client disconnects.
func (b *Broadcaster) Subscribe() (<-chan []byte, func()) {
	ch := make(chan []byte, 64)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	return ch, func() {
		b.mu.Lock()
		if _, ok := b.subs[ch]; ok {
			delete(b.subs, ch)
			close(ch)
		}
		b.mu.Unlock()
	}
}

// Subscribers reports the current connection count (for diagnostics/tests).
func (b *Broadcaster) Subscribers() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.subs)
}
