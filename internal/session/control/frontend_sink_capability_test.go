package control

import (
	"reasonix/internal/contract/hostaudit"
	"testing"

	"reasonix/internal/contract/event"
)

// Enabling extensions must never sever the audit channels: every capability
// the trajectory recorder implements has to pass through frontendEventSink.
func TestFrontendEventSinkForwardsAuditCapabilities(t *testing.T) {
	inner := &capabilityProbeSink{Sink: event.Discard}
	s := newFrontendEventSink(inner, nil)
	event.RecordOutcomeProgress(s, hostaudit.OutcomeSample{Round: 1})
	event.RecordMemoryRecall(s, event.MemoryRecallAudit{Suppressed: "probe"})
	event.RecordContractShadow(s, event.ContractShadowAudit{Verdict: "probe"})
	event.RecordCompletionReport(s, event.CompletionReportAudit{Verdict: "probe"})
	if inner.outcome != 1 || inner.recall != 1 || inner.contract != 1 || inner.completion != 1 {
		t.Fatalf("audits dropped by frontendEventSink: %+v", inner)
	}
}

type capabilityProbeSink struct {
	event.Sink
	outcome, recall, contract, completion int
}

func (p *capabilityProbeSink) RecordOutcomeProgress(hostaudit.OutcomeSample)  { p.outcome++ }
func (p *capabilityProbeSink) RecordMemoryRecall(event.MemoryRecallAudit)     { p.recall++ }
func (p *capabilityProbeSink) RecordContractShadow(event.ContractShadowAudit) { p.contract++ }
func (p *capabilityProbeSink) RecordCompletionReport(event.CompletionReportAudit) {
	p.completion++
}
