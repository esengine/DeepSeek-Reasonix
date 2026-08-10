package control

import (
	"reasonix/internal/event"
	"reasonix/internal/evidence"
)

// inboxEventSink observes Steer and unapplied-steer events so durable inbox
// state tracks consumption without frontends matching on text.
type inboxEventSink struct {
	inner event.Sink
	c     *Controller
}

func (s *inboxEventSink) Emit(e event.Event) {
	if s == nil {
		return
	}
	if s.inner != nil {
		s.inner.Emit(e)
	}
	if s.c == nil {
		return
	}
	switch e.Kind {
	case event.Steer:
		if e.ItemID != "" {
			s.c.onInboxSteerConsumed(e.ItemID)
		}
	case event.Notice:
		if e.Code == event.NoticeCodeUnappliedSteer && e.ItemID != "" {
			s.c.onInboxUnappliedSteer(e.ItemID)
		}
	}
}

// Forward optional sink capabilities so wrapping does not strip accounting.

func (s *inboxEventSink) RecordTurnCompletion() {
	if s == nil {
		return
	}
	event.RecordTurnCompletion(s.inner)
}

func (s *inboxEventSink) RecordReadinessAudit(a evidence.ReadinessAudit) {
	if s == nil {
		return
	}
	event.RecordReadinessAudit(s.inner, a)
}

func (s *inboxEventSink) RecordContractShadow(a event.ContractShadowAudit) {
	if s == nil {
		return
	}
	if rs, ok := s.inner.(interface {
		RecordContractShadow(event.ContractShadowAudit)
	}); ok {
		rs.RecordContractShadow(a)
	}
}

func (s *inboxEventSink) RecordCompletionReport(a event.CompletionReportAudit) {
	if s == nil {
		return
	}
	if rs, ok := s.inner.(interface {
		RecordCompletionReport(event.CompletionReportAudit)
	}); ok {
		rs.RecordCompletionReport(a)
	}
}

func (s *inboxEventSink) RecordOutcomeProgress(sample evidence.OutcomeSample) {
	if s == nil {
		return
	}
	if rs, ok := s.inner.(interface{ RecordOutcomeProgress(evidence.OutcomeSample) }); ok {
		rs.RecordOutcomeProgress(sample)
	}
}

func (s *inboxEventSink) RecordDelegationAdmission(a event.DelegationAdmissionAudit) {
	if s == nil {
		return
	}
	if rs, ok := s.inner.(interface {
		RecordDelegationAdmission(event.DelegationAdmissionAudit)
	}); ok {
		rs.RecordDelegationAdmission(a)
	}
}

func (s *inboxEventSink) RecordMemoryRecall(a event.MemoryRecallAudit) {
	if s == nil {
		return
	}
	if rs, ok := s.inner.(interface{ RecordMemoryRecall(event.MemoryRecallAudit) }); ok {
		rs.RecordMemoryRecall(a)
	}
}

func (s *inboxEventSink) RecordProtocolRecovery(a event.ProtocolRecoveryAudit) {
	if s == nil {
		return
	}
	event.RecordProtocolRecovery(s.inner, a)
}
