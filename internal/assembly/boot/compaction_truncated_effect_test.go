package boot

import (
	"sync"

	"reasonix/internal/contract/event"
)

type collectSink struct {
	mu sync.Mutex
	ev []event.Event
}

func (s *collectSink) Emit(e event.Event) { s.mu.Lock(); s.ev = append(s.ev, e); s.mu.Unlock() }
