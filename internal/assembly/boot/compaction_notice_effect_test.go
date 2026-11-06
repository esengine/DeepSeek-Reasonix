package boot

import (
	"sync"

	"reasonix/internal/contract/event"
)

type noticeSink struct {
	mu sync.Mutex
	ev []event.Event
}

func (s *noticeSink) Emit(e event.Event) { s.mu.Lock(); s.ev = append(s.ev, e); s.mu.Unlock() }

func (s *noticeSink) notice(code string) (event.Event, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.ev {
		if e.Kind == event.Notice && e.Code == code {
			return e, true
		}
	}
	return event.Event{}, false
}
