package remote

import "sync"

// statusHub fans status events out to subscribers and remembers the last event
// so a late subscriber immediately learns the current state. Subscriber
// callbacks are invoked synchronously under no lock; they must not block or
// call back into the Client.
type statusHub struct {
	mu     sync.Mutex
	last   StatusEvent
	haveL  bool
	nextID int
	subs   map[int]func(StatusEvent)
}

func newStatusHub() *statusHub {
	return &statusHub{subs: map[int]func(StatusEvent){}}
}

// subscribe registers fn, replays the last event to it, and returns a cancel.
func (h *statusHub) subscribe(fn func(StatusEvent)) func() {
	h.mu.Lock()
	id := h.nextID
	h.nextID++
	h.subs[id] = fn
	last, have := h.last, h.haveL
	h.mu.Unlock()

	if have {
		fn(last)
	}
	return func() {
		h.mu.Lock()
		delete(h.subs, id)
		h.mu.Unlock()
	}
}

// publish records ev as the last event and delivers it to all subscribers.
func (h *statusHub) publish(ev StatusEvent) {
	h.mu.Lock()
	h.last = ev
	h.haveL = true
	fns := make([]func(StatusEvent), 0, len(h.subs))
	for _, fn := range h.subs {
		fns = append(fns, fn)
	}
	h.mu.Unlock()
	for _, fn := range fns {
		fn(ev)
	}
}

func (h *statusHub) current() StatusEvent {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.last
}
