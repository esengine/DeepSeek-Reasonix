package event

import (
	"reasonix/internal/base/nilutil"
	"reasonix/internal/contract/hostaudit"
)

// AuditForwarder forwards every optional sink capability to Inner. Embed it in
// a wrapper that only passes host-side signals through, so a channel added here
// reaches every embedder without editing them. Hand-written forwarding lost
// capabilities at multiple wrappers even while their owning tests stayed green.
type AuditForwarder struct{ Inner Sink }

func (f AuditForwarder) RecordReadinessAudit(a hostaudit.ReadinessAudit) {
	RecordReadinessAudit(f.Inner, a)
}

func (f AuditForwarder) RecordTurnCompletion() { RecordTurnCompletion(f.Inner) }

func (f AuditForwarder) RecordContractShadow(a ContractShadowAudit) {
	RecordContractShadow(f.Inner, a)
}

func (f AuditForwarder) RecordEvidenceBundle(a EvidenceBundleAudit) {
	RecordEvidenceBundle(f.Inner, a)
}

func (f AuditForwarder) RecordCompletionReport(a CompletionReportAudit) {
	RecordCompletionReport(f.Inner, a)
}

func (f AuditForwarder) RecordMemoryRecall(a MemoryRecallAudit) {
	RecordMemoryRecall(f.Inner, a)
}

func (f AuditForwarder) RecordOutcomeProgress(s hostaudit.OutcomeSample) {
	RecordOutcomeProgress(f.Inner, s)
}

func (f AuditForwarder) RecordProtocolRecovery(a ProtocolRecoveryAudit) {
	RecordProtocolRecovery(f.Inner, a)
}

func (f AuditForwarder) RecordDelegationAudit(a hostaudit.DelegationAudit) {
	RecordDelegationAudit(f.Inner, a)
}

func (f AuditForwarder) RecordWorkspaceMutation(m WorkspaceMutation) {
	RecordWorkspaceMutation(f.Inner, m)
}

func (f AuditForwarder) RecordRunBudget(s RunBudgetSample) {
	RecordRunBudget(f.Inner, s)
}

func (f AuditForwarder) RecordProjectCheckProbe(p ProjectCheckProbe) {
	RecordProjectCheckProbe(f.Inner, p)
}

func (f AuditForwarder) RecordSubagentHandoff(a SubagentHandoffAudit) {
	RecordSubagentHandoff(f.Inner, a)
}

func (f AuditForwarder) RecordVerificationContractDrift(d VerificationContractDrift) {
	RecordVerificationContractDrift(f.Inner, d)
}

// DelegationAuditSink receives one receipt per completed sub-agent run.
type DelegationAuditSink interface {
	RecordDelegationAudit(a hostaudit.DelegationAudit)
}

// RecordDelegationAudit forwards a delegation receipt to sinks that opt in.
func RecordDelegationAudit(s Sink, a hostaudit.DelegationAudit) {
	if nilutil.IsNil(s) {
		return
	}
	if ds, ok := s.(DelegationAuditSink); ok {
		ds.RecordDelegationAudit(a)
	}
}
