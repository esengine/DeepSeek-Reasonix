package serve

import (
	"bytes"
	"encoding/json"
	"sync"

	"reasonix/internal/event"
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

// encPool reuses bytes.Buffer instances across Emit calls. Each event
// streamed from a long agent turn otherwise allocates a fresh buffer that
// the GC has to walk and free on the way out — a steady drip of small
// allocations that bumps the pacer noticeably when a turn streams hundreds
// of text deltas. The pool is sized for the steady state of one in-flight
// emit; concurrent emits get their own buffer and the pool just shaves the
// single-emit case.
var encPool = sync.Pool{
	New: func() any {
		// 4 KiB is enough for the common text_delta + tool_dispatch frame
		// (both well under 1 KiB in practice). Bigger frames (a tool
		// result with a full file body) grow the buffer past the initial
		// capacity; sync.Pool returns the same instance on Get, so the
		// growth amortization is what matters, not the initial size.
		b := &bytes.Buffer{}
		b.Grow(4096)
		return b
	},
}

// Emit marshals the event to JSON and delivers it to every subscriber. Drops to
// a subscriber whose buffer is full rather than blocking. A marshal failure is
// dropped silently — one bad event shouldn't stall the stream.
//
// Buffer reuse: a fresh *bytes.Buffer is pulled from encPool, the event is
// marshaled into it, and the resulting []byte is cloned (one copy per
// subscriber — they each get their own slice so the pool can return the
// buffer to the pool before the slowest subscriber has consumed the frame).
// The buffer is always returned to the pool on the way out, including the
// marshal-failure path.
func (b *Broadcaster) Emit(e event.Event) {
	buf := encPool.Get().(*bytes.Buffer)
	buf.Reset()
	// json.Encoder is a stricter writer than json.Marshal — it rejects
	// non-JSON-safe types in map keys and adds a trailing newline. We use
	// the encoder here because (a) it streams into the buffer with no
	// intermediate allocation, and (b) its contract is identical to
	// json.Marshal for the wireEvent shape (plain struct, all string keys,
	// no NaN/Inf floats).
	enc := json.NewEncoder(buf)
	enc.SetEscapeHTML(false) // matches the conn.Encoder contract for the ACP wire
	if err := enc.Encode(toWire(e)); err != nil {
		encPool.Put(buf)
		return
	}
	// json.Encoder appends a trailing '\n' so the output is line-delimited
	// (one JSON object per line). The SSE handler emits each frame with
	// "data: <payload>\n\n" framing, so the trailing newline is absorbed
	// into the separator; trimming would re-allocate, so we keep it.
	data := buf.Bytes()

	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs {
		// Copy per subscriber: the buffer is going back to the pool as soon
		// as Emit returns, so any subscriber that hasn't drained its
		// channel yet would see the buffer reused mid-frame. A single
		// bytes.Clone is cheaper than per-subscriber sync.Pool churn.
		select {
		case ch <- bytes.Clone(data):
		default: // subscriber is behind; drop this frame for it
		}
	}
	encPool.Put(buf)
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
