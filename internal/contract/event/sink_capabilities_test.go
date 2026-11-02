package event

import (
	"reasonix/internal/contract/hostaudit"
	"reasonix/internal/contract/pricing"
	"reflect"
	"testing"
	"time"
)

// capturingSink implements every optional capability so a chain test can prove
// a signal reached the far end rather than dying at some wrapper.
type capturingSink struct {
	events   []Event
	recorded []string
}

func (s *capturingSink) Emit(e Event) { s.events = append(s.events, e) }

func (s *capturingSink) QuoteUsage(e Event) *pricing.CostQuote {
	s.recorded = append(s.recorded, "UsageQuote")
	return EnsureCostQuote(e, nil)
}

func (s *capturingSink) RecordDelegationAudit(hostaudit.DelegationAudit) {
	s.recorded = append(s.recorded, "DelegationAudit")
}
func (s *capturingSink) RecordTurnCompletion() {
	s.recorded = append(s.recorded, "TurnCompletion")
}
func (s *capturingSink) RecordMemoryRecall(MemoryRecallAudit) {
	s.recorded = append(s.recorded, "MemoryRecall")
}
func (s *capturingSink) RecordWorkspaceMutation(WorkspaceMutation) {
	s.recorded = append(s.recorded, "WorkspaceMutation")
}
func (s *capturingSink) RecordRunBudget(RunBudgetSample) {
	s.recorded = append(s.recorded, "RunBudget")
}

// AuditForwarder is why a new capability does not have to be repeated at every
// layer, so it must itself be complete. It reads the registry rather than a
// list kept beside it: a hand-written list passes for every capability it has
// never heard of, which is how a channel comes to be registered and forwarded
// by nothing.
func TestAuditForwarderCoversEveryCapability(t *testing.T) {
	fwd := reflect.TypeFor[AuditForwarder]()
	for name, want := range capabilityContracts {
		if !fwd.Implements(want) {
			t.Errorf("AuditForwarder does not forward %s; every embedder loses it", name)
		}
	}
}

// TestSessionChainDeliversAudits mirrors boot's assembly order
// (Coalesce → Sync → CostQuote → recorder) end to end.
func TestSessionChainDeliversAudits(t *testing.T) {
	recorder := &capturingSink{}
	chain := Coalesce(Sync(NewCostQuoteSink(recorder, nil)), 5*time.Millisecond)

	RecordMemoryRecall(chain, MemoryRecallAudit{})

	if len(recorder.recorded) != 1 || recorder.recorded[0] != "MemoryRecall" {
		t.Fatalf("memory recall never reached the recorder: %v", recorder.recorded)
	}
}

func (s *capturingSink) CaptureUsageScope() any {
	s.recorded = append(s.recorded, "UsageScope")
	return s
}
