package event

import (
	"testing"

	"reasonix/internal/evidence"
)

// capabilityRecorder accepts every optional sink capability and names the ones
// it received.
type capabilityRecorder struct {
	got map[string]bool
}

func newCapabilityRecorder() *capabilityRecorder {
	return &capabilityRecorder{got: map[string]bool{}}
}

func (c *capabilityRecorder) mark(name string)      { c.got[name] = true }
func (c *capabilityRecorder) Emit(Event)            { c.mark("emit") }
func (c *capabilityRecorder) RecordTurnCompletion() { c.mark("turn_completion") }

func (c *capabilityRecorder) RecordReadinessAudit(evidence.ReadinessAudit) {
	c.mark("readiness_audit")
}
func (c *capabilityRecorder) RecordContractShadow(ContractShadowAudit) { c.mark("contract_shadow") }
func (c *capabilityRecorder) RecordCompletionReport(CompletionReportAudit) {
	c.mark("completion_report")
}
func (c *capabilityRecorder) RecordMemoryRecall(MemoryRecallAudit) { c.mark("memory_recall") }
func (c *capabilityRecorder) RecordDelegationAdmission(DelegationAdmissionAudit) {
	c.mark("delegation_admission")
}
func (c *capabilityRecorder) RecordOutcomeProgress(evidence.OutcomeSample) {
	c.mark("outcome_progress")
}
func (c *capabilityRecorder) RecordProtocolRecovery(ProtocolRecoveryAudit) {
	c.mark("protocol_recovery")
}
func (c *capabilityRecorder) RecordDelegationAudit(evidence.DelegationAudit) {
	c.mark("delegation_audit")
}
func (c *capabilityRecorder) RecordWorkspaceMutation(WorkspaceMutation) {
	c.mark("workspace_mutation")
}
func (c *capabilityRecorder) RecordRunBudget(RunBudgetSample) { c.mark("run_budget") }

// A wrapper that drops an optional capability silently truncates every recorder
// below it: the trajectory and stats recorders sit under the quoting sink, so
// this one dropping them meant real runs recorded no audits at all while every
// unit test — which wires recorders directly — stayed green.
func TestCostQuoteSinkPreservesEveryAuditCapability(t *testing.T) {
	inner := newCapabilityRecorder()
	// Sink, not *CostQuoteSink: the host reaches these channels through the
	// package dispatchers, which type-assert and silently no-op on a wrapper
	// that lost the capability. Calling the methods directly instead would turn
	// this guard into a compile error that only fires when they vanish outright.
	var s Sink = NewCostQuoteSink(inner, nil)

	s.Emit(Event{Kind: Notice})
	RecordTurnCompletion(s)
	RecordReadinessAudit(s, evidence.ReadinessAudit{})
	RecordContractShadow(s, ContractShadowAudit{})
	RecordCompletionReport(s, CompletionReportAudit{})
	RecordMemoryRecall(s, MemoryRecallAudit{})
	RecordDelegationAdmission(s, DelegationAdmissionAudit{})
	RecordOutcomeProgress(s, evidence.OutcomeSample{Round: 1})
	RecordProtocolRecovery(s, ProtocolRecoveryAudit{})
	RecordDelegationAudit(s, evidence.DelegationAudit{})
	RecordWorkspaceMutation(s, WorkspaceMutation{})
	RecordRunBudget(s, RunBudgetSample{})

	for _, want := range []string{
		"emit", "turn_completion", "readiness_audit", "contract_shadow",
		"completion_report", "memory_recall", "delegation_admission",
		"outcome_progress", "protocol_recovery", "delegation_audit",
		"workspace_mutation", "run_budget",
	} {
		if !inner.got[want] {
			t.Errorf("quoting sink swallowed %s; everything recorded below it loses that channel", want)
		}
	}
}
