package usage

import (
	"sync"
	"time"

	"reasonix/internal/event"
)

// EnrichSink injects ProviderName, ModelName, LatencyMS, and SessionID into
// every event.Usage before forwarding. It is thread-safe.
type EnrichSink struct {
	Inner        event.Sink
	ProviderName string
	ModelName    string
	SessionID    func() string

	mu        sync.Mutex
	turnStart time.Time
}

func (s *EnrichSink) Emit(e event.Event) {
	switch e.Kind {
	case event.TurnStarted:
		s.mu.Lock()
		s.turnStart = time.Now()
		s.mu.Unlock()
	case event.TurnDone:
		s.mu.Lock()
		s.turnStart = time.Time{}
		s.mu.Unlock()
	}
	if e.Kind == event.Usage && e.Usage != nil {
		if e.ProviderName == "" {
			e.ProviderName = s.ProviderName
		}
		if e.ModelName == "" {
			e.ModelName = s.ModelName
		}
		if s.SessionID != nil {
			if sid := s.SessionID(); sid != "" {
				e.SessionID = sid
			}
		}
		s.mu.Lock()
		if !s.turnStart.IsZero() {
			e.LatencyMS = time.Since(s.turnStart).Milliseconds()
		}
		s.mu.Unlock()
	}
	s.Inner.Emit(e)
}
