package control

import (
	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/sessioninbox"
)

// inboxEventSink observes unapplied-steer events and forwards optional inbox
// snapshot notifications without stripping the desktop sink capability.
type inboxEventSink struct {
	inner event.Sink
	c     *Controller
}

var _ event.OptionalSinkCapabilities = (*inboxEventSink)(nil)

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
	if e.Kind == event.Notice {
		if e.Code == event.NoticeCodeUnappliedSteer && e.ItemID != "" {
			s.c.onInboxUnappliedSteer(e.ItemID)
		}
	}
}

func notifyInboxChanged(sink event.Sink, snap sessioninbox.InboxSnapshot) {
	if target, ok := sink.(interface {
		InboxChanged(sessioninbox.InboxSnapshot)
	}); ok {
		target.InboxChanged(snap)
	}
}

func (s *inboxEventSink) InboxChanged(snap sessioninbox.InboxSnapshot) {
	if s == nil {
		return
	}
	notifyInboxChanged(s.inner, snap)
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

func (s *inboxEventSink) RecordAnchorSafetyAudit(a event.AnchorSafetyAudit) {
	if s == nil {
		return
	}
	event.RecordAnchorSafetyAudit(s.inner, a)
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

func (s *inboxEventSink) RecordDelegationAudit(a evidence.DelegationAudit) {
	if s == nil {
		return
	}
	event.RecordDelegationAudit(s.inner, a)
}

func (s *inboxEventSink) RecordWorkspaceMutation(m event.WorkspaceMutation) {
	if s == nil {
		return
	}
	event.RecordWorkspaceMutation(s.inner, m)
}

func (s *inboxEventSink) RecordRunBudget(sample event.RunBudgetSample) {
	if s == nil {
		return
	}
	event.RecordRunBudget(s.inner, sample)
}
