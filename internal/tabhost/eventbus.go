package tabhost

import (
	"encoding/json"
	"sync"

	"reasonix/internal/event"
	"reasonix/internal/eventwire"
)

// WireEvent is eventwire.Event plus tab routing fields (desktop wireEventTab).
type WireEvent struct {
	eventwire.Event
	TabID        string `json:"tabId"`
	RuntimeEpoch string `json:"runtimeEpoch,omitempty"`
}

// EventBus fans stamped wire events to subscribers (SSE / host adapters).
type EventBus struct {
	mu   sync.Mutex
	subs map[chan []byte]struct{}
}

// NewEventBus builds an empty bus.
func NewEventBus() *EventBus {
	return &EventBus{subs: make(map[chan []byte]struct{})}
}

// Subscribe registers a buffered subscriber. Caller must Unsubscribe.
func (b *EventBus) Subscribe() (<-chan []byte, func()) {
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

// PublishJSON broadcasts a pre-marshaled payload; drops if a subscriber is full.
func (b *EventBus) PublishJSON(data []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs {
		select {
		case ch <- data:
		default:
			// drop — same class of tradeoff as serve.Broadcaster
		}
	}
}

// PublishEvent stamps tab metadata and publishes.
func (b *EventBus) PublishEvent(tabID, runtimeEpoch string, e event.Event) error {
	w := WireEvent{
		Event:        eventwire.ToWire(e),
		TabID:        tabID,
		RuntimeEpoch: runtimeEpoch,
	}
	data, err := json.Marshal(w)
	if err != nil {
		return err
	}
	b.PublishJSON(data)
	return nil
}

// tabSink implements event.Sink and stamps tabId onto the bus.
type tabSink struct {
	bus          *EventBus
	tabID        string
	runtimeEpoch string
	mu           sync.RWMutex
}

func (s *tabSink) Emit(e event.Event) {
	s.mu.RLock()
	id, epoch, bus := s.tabID, s.runtimeEpoch, s.bus
	s.mu.RUnlock()
	if bus == nil || id == "" {
		return
	}
	_ = bus.PublishEvent(id, epoch, e)
}

func (s *tabSink) setEpoch(epoch string) {
	s.mu.Lock()
	s.runtimeEpoch = epoch
	s.mu.Unlock()
}
