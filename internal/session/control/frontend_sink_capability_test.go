package control

import (
	"testing"

	"reasonix/internal/contract/event"
)

// Enabling extensions must never sever the audit channels: every capability
// the trajectory recorder implements has to pass through frontendEventSink.
func TestFrontendEventSinkForwardsAuditCapabilities(t *testing.T) {
	inner := &capabilityProbeSink{Sink: event.Discard}
	s := newFrontendEventSink(inner, nil)
	event.RecordMemoryRecall(s, event.MemoryRecallAudit{Suppressed: "probe"})
	if inner.recall != 1 {
		t.Fatalf("audits dropped by frontendEventSink: %+v", inner)
	}
}

type capabilityProbeSink struct {
	event.Sink
	recall int
}

func (p *capabilityProbeSink) RecordMemoryRecall(event.MemoryRecallAudit) { p.recall++ }
