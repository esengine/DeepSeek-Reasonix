package event

import (
	"sync"

	"reasonix/internal/evidence"
	"reasonix/internal/nilutil"
)

// Sync wraps a Sink so concurrent Emit calls are serialized. The base Sink
// contract assumes serial emission — the agent's run loop emits one event at a
// time. Background jobs (internal/jobs) emit from their own goroutines, which can
// overlap a running turn's emission; wrapping the session sink once in Sync keeps
// the serial-Emit invariant every sink relies on (an SSE writer, a webview
// EventsEmit, a TUI channel) without each having to lock. A nil sink yields
// Discard.
func Sync(s Sink) Sink {
	if nilutil.IsNil(s) {
		return Discard
	}
	return &syncSink{inner: s}
}

type syncSink struct {
	mu    sync.Mutex
	inner Sink
}

func (s *syncSink) Emit(e Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inner.Emit(e)
}

func (s *syncSink) RecordReadinessAudit(a evidence.ReadinessAudit) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if rs, ok := s.inner.(ReadinessAuditSink); ok {
		rs.RecordReadinessAudit(a)
	}
}
