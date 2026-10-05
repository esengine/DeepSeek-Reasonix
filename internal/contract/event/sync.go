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

func (s *syncSink) RecordReadinessAudit(a hostaudit.ReadinessAudit) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if rs, ok := s.inner.(ReadinessAuditSink); ok {
		rs.RecordReadinessAudit(a)
	}
}

func (s *syncSink) RecordProjectCheckProbe(p ProjectCheckProbe) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ps, ok := s.inner.(ProjectCheckProbeSink); ok {
		ps.RecordProjectCheckProbe(p)
	}
}

func (s *syncSink) RecordSubagentHandoff(a SubagentHandoffAudit) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if hs, ok := s.inner.(SubagentHandoffSink); ok {
		hs.RecordSubagentHandoff(a)
	}
}

func (s *syncSink) RecordVerificationContractDrift(d VerificationContractDrift) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ds, ok := s.inner.(VerificationContractDriftSink); ok {
		ds.RecordVerificationContractDrift(d)
	}
}

func (s *syncSink) RecordTurnCompletion() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ts, ok := s.inner.(TurnCompletionSink); ok {
		ts.RecordTurnCompletion()
	}
}

func (s *syncSink) RecordProtocolRecovery(a ProtocolRecoveryAudit) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if rs, ok := s.inner.(ProtocolRecoveryAuditSink); ok {
		rs.RecordProtocolRecovery(a)
	}
}

func (s *syncSink) RecordContractShadow(a ContractShadowAudit) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if rs, ok := s.inner.(ContractShadowAuditSink); ok {
		rs.RecordContractShadow(a)
	}
}

func (s *syncSink) RecordCompletionReport(a CompletionReportAudit) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if rs, ok := s.inner.(CompletionReportAuditSink); ok {
		rs.RecordCompletionReport(a)
	}
}

func (s *syncSink) RecordEvidenceBundle(a EvidenceBundleAudit) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if es, ok := s.inner.(EvidenceBundleAuditSink); ok {
		es.RecordEvidenceBundle(a)
	}
}

func (s *syncSink) RecordOutcomeProgress(sample hostaudit.OutcomeSample) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if op, ok := s.inner.(OutcomeProgressSink); ok {
		op.RecordOutcomeProgress(sample)
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
