package event

import (
	"reasonix/internal/contract/hostaudit"
	"sync"

	"reasonix/internal/base/nilutil"
)

// Sync wraps a Sink so concurrent Emit calls are serialized. The base Sink
// contract assumes serial emission — the agent's run loop emits one event at a
// time. Background jobs (internal/tools/jobs) emit from their own goroutines, which can
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

func (s *syncSink) CaptureUsageScope() any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return CaptureUsageScope(s.inner)
}

func (s *syncSink) Emit(e Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inner.Emit(e)
}

func (s *syncSink) RecordDelegationAudit(a hostaudit.DelegationAudit) {
	s.mu.Lock()
	defer s.mu.Unlock()
	RecordDelegationAudit(s.inner, a)
}

func (s *syncSink) RecordTurnCompletion() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ts, ok := s.inner.(TurnCompletionSink); ok {
		ts.RecordTurnCompletion()
	}
}

func (s *syncSink) RecordMemoryRecall(a MemoryRecallAudit) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if mr, ok := s.inner.(MemoryRecallSink); ok {
		mr.RecordMemoryRecall(a)
	}
}

func (s *syncSink) RecordWorkspaceMutation(m WorkspaceMutation) {
	s.mu.Lock()
	defer s.mu.Unlock()
	RecordWorkspaceMutation(s.inner, m)
}

func (s *syncSink) RecordRunBudget(sample RunBudgetSample) {
	s.mu.Lock()
	defer s.mu.Unlock()
	RecordRunBudget(s.inner, sample)
}
