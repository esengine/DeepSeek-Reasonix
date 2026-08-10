package event

import (
	"reasonix/internal/evidence"
	"reasonix/internal/nilutil"
)

// AuditForwarder forwards every optional sink capability to Inner. Embed it in
// a wrapper that only passes audits through, so a channel added here reaches
// every embedder without editing them. Hand-written forwarding lost three
// audits at three wrappers, silently and with the tests green, because nine
// capabilities had to be repeated at every layer to stay whole.
type AuditForwarder struct{ Inner Sink }

func (f AuditForwarder) RecordReadinessAudit(a evidence.ReadinessAudit) {
	RecordReadinessAudit(f.Inner, a)
}

func (f AuditForwarder) RecordTurnCompletion() { RecordTurnCompletion(f.Inner) }

func (f AuditForwarder) RecordContractShadow(a ContractShadowAudit) {
	RecordContractShadow(f.Inner, a)
}

func (f AuditForwarder) RecordCompletionReport(a CompletionReportAudit) {
	RecordCompletionReport(f.Inner, a)
}

func (f AuditForwarder) RecordMemoryRecall(a MemoryRecallAudit) {
	RecordMemoryRecall(f.Inner, a)
}

func (f AuditForwarder) RecordDelegationAdmission(a DelegationAdmissionAudit) {
	RecordDelegationAdmission(f.Inner, a)
}

func (f AuditForwarder) RecordOutcomeProgress(s evidence.OutcomeSample) {
	RecordOutcomeProgress(f.Inner, s)
}

func (f AuditForwarder) RecordProtocolRecovery(a ProtocolRecoveryAudit) {
	RecordProtocolRecovery(f.Inner, a)
}

func (f AuditForwarder) RecordDelegationAudit(a evidence.DelegationAudit) {
	RecordDelegationAudit(f.Inner, a)
}

// DelegationAuditSink receives one receipt per completed sub-agent run.
type DelegationAuditSink interface {
	RecordDelegationAudit(a evidence.DelegationAudit)
}

// RecordDelegationAudit forwards a delegation receipt to sinks that opt in.
func RecordDelegationAudit(s Sink, a evidence.DelegationAudit) {
	if nilutil.IsNil(s) {
		return
	}
	if ds, ok := s.(DelegationAuditSink); ok {
		ds.RecordDelegationAudit(a)
	}
}
