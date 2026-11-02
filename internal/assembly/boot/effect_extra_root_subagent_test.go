package boot

import (
	"sync"

	"reasonix/internal/contract/event"
)

type childReceiptSink struct {
	mu     sync.Mutex
	events []event.Event
	done   chan struct{}
	once   sync.Once
}

func (s *childReceiptSink) Emit(e event.Event) {
	s.mu.Lock()
	s.events = append(s.events, e)
	s.mu.Unlock()
	if e.Kind == event.TurnDone {
		s.once.Do(func() { close(s.done) })
	}
}
