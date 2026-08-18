package event

import (
	"reasonix/internal/evidence"
	"reasonix/internal/nilutil"
)

// AuditForwarder forwards every optional sink capability to Inner. Embed it in
// a wrapper that only passes host-side signals through, so a channel added here
// reaches every embedder without editing them. Hand-written forwarding lost
// capabilities at multiple wrappers even while their owning tests stayed green.
type AuditForwarder struct{ Inner Sink }

// OptionalSinkCapabilities is the complete host-side capability contract that
// event decorators must preserve. Keeping the method set in one interface lets
// wrappers use compile-time assertions instead of maintaining partial lists in
// tests that silently drift when a new capability is added.
type OptionalSinkCapabilities interface {
	DelegationAuditSink
	ReadinessAuditSink
	AnchorSafetyAuditSink
	ContractShadowAuditSink
	CompletionReportAuditSink
	MemoryRecallSink
	DelegationAdmissionSink
	OutcomeProgressSink
	ProtocolRecoveryAuditSink
	TurnCompletionSink
	WorkspaceMutationSink
	RunBudgetSink
}

var _ OptionalSinkCapabilities = AuditForwarder{}

func (f AuditForwarder) RecordReadinessAudit(a evidence.ReadinessAudit) {
	RecordReadinessAudit(f.Inner, a)
}

func (f AuditForwarder) RecordAnchorSafetyAudit(a AnchorSafetyAudit) {
	RecordAnchorSafetyAudit(f.Inner, a)
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

func (f AuditForwarder) RecordWorkspaceMutation(m WorkspaceMutation) {
	RecordWorkspaceMutation(f.Inner, m)
}

func (f AuditForwarder) RecordRunBudget(s RunBudgetSample) {
	RecordRunBudget(f.Inner, s)
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
