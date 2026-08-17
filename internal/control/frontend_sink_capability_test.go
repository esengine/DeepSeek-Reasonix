package control

import (
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/evidence"
)

// Enabling extensions must never sever the audit channels: every capability
// the trajectory recorder implements has to pass through frontendEventSink.
func TestFrontendEventSinkForwardsAuditCapabilities(t *testing.T) {
	inner := &capabilityProbeSink{Sink: event.Discard}
	s := newFrontendEventSink(inner, nil)
	event.RecordOutcomeProgress(s, evidence.OutcomeSample{Round: 1})
	event.RecordMemoryRecall(s, event.MemoryRecallAudit{Suppressed: "probe"})
	event.RecordDelegationAdmission(s, event.DelegationAdmissionAudit{Tool: "probe"})
	event.RecordContractShadow(s, event.ContractShadowAudit{Verdict: "probe"})
	event.RecordCompletionReport(s, event.CompletionReportAudit{Verdict: "probe"})
	event.RecordWorkspaceMutation(s, event.WorkspaceMutation{ToolName: "write_file"})
	event.RecordRunBudget(s, event.RunBudgetSample{Currency: "USD"})
	if inner.outcome != 1 || inner.recall != 1 || inner.delegation != 1 || inner.contract != 1 || inner.completion != 1 || inner.workspace != 1 || inner.runBudget != 1 {
		t.Fatalf("audits dropped by frontendEventSink: %+v", inner)
	}
}

func TestInboxEventSinkForwardsMissingCapabilities(t *testing.T) {
	inner := &capabilityProbeSink{Sink: event.Discard}
	s := &inboxEventSink{inner: inner}
	event.RecordDelegationAudit(s, evidence.DelegationAudit{})
	event.RecordWorkspaceMutation(s, event.WorkspaceMutation{ToolName: "write_file"})
	event.RecordRunBudget(s, event.RunBudgetSample{Currency: "USD"})
	if inner.delegationAudit != 1 || inner.workspace != 1 || inner.runBudget != 1 {
		t.Fatalf("host capabilities dropped by inboxEventSink: %+v", inner)
	}
}

type capabilityProbeSink struct {
	event.Sink
	outcome, recall, delegation, delegationAudit, contract, completion, workspace, runBudget int
}

func (p *capabilityProbeSink) RecordOutcomeProgress(evidence.OutcomeSample) { p.outcome++ }
func (p *capabilityProbeSink) RecordMemoryRecall(event.MemoryRecallAudit)   { p.recall++ }
func (p *capabilityProbeSink) RecordDelegationAdmission(event.DelegationAdmissionAudit) {
	p.delegation++
}
func (p *capabilityProbeSink) RecordContractShadow(event.ContractShadowAudit) { p.contract++ }
func (p *capabilityProbeSink) RecordCompletionReport(event.CompletionReportAudit) {
	p.completion++
}
func (p *capabilityProbeSink) RecordDelegationAudit(evidence.DelegationAudit) {
	p.delegationAudit++
}
func (p *capabilityProbeSink) RecordWorkspaceMutation(event.WorkspaceMutation) { p.workspace++ }
func (p *capabilityProbeSink) RecordRunBudget(event.RunBudgetSample)           { p.runBudget++ }
