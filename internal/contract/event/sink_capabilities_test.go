package event

import (
	"reasonix/internal/contract/hostaudit"
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

func (s *capturingSink) RecordDelegationAudit(hostaudit.DelegationAudit) {
	s.recorded = append(s.recorded, "DelegationAudit")
}
func (s *capturingSink) RecordReadinessAudit(hostaudit.ReadinessAudit) {
	s.recorded = append(s.recorded, "ReadinessAudit")
}
func (s *capturingSink) RecordTurnCompletion() {
	s.recorded = append(s.recorded, "TurnCompletion")
}
func (s *capturingSink) RecordProtocolRecovery(ProtocolRecoveryAudit) {
	s.recorded = append(s.recorded, "ProtocolRecovery")
}
func (s *capturingSink) RecordContractShadow(ContractShadowAudit) {
	s.recorded = append(s.recorded, "ContractShadow")
}
func (s *capturingSink) RecordCompletionReport(CompletionReportAudit) {
	s.recorded = append(s.recorded, "CompletionReport")
}
func (s *capturingSink) RecordEvidenceBundle(EvidenceBundleAudit) {
	s.recorded = append(s.recorded, "EvidenceBundle")
}
func (s *capturingSink) RecordMemoryRecall(MemoryRecallAudit) {
	s.recorded = append(s.recorded, "MemoryRecall")
}
func (s *capturingSink) RecordOutcomeProgress(hostaudit.OutcomeSample) {
	s.recorded = append(s.recorded, "OutcomeProgress")
}
func (s *capturingSink) RecordWorkspaceMutation(WorkspaceMutation) {
	s.recorded = append(s.recorded, "WorkspaceMutation")
}
func (s *capturingSink) RecordRunBudget(RunBudgetSample) {
	s.recorded = append(s.recorded, "RunBudget")
}
func (s *capturingSink) RecordProjectCheckProbe(ProjectCheckProbe) {
	s.recorded = append(s.recorded, "ProjectCheckProbe")
}
func (s *capturingSink) RecordSubagentHandoff(SubagentHandoffAudit) {
	s.recorded = append(s.recorded, "SubagentHandoff")
}
func (s *capturingSink) RecordVerificationContractDrift(VerificationContractDrift) {
	s.recorded = append(s.recorded, "VerificationDrift")
}

// recordAll drives every optional capability once through s.
func recordAll(s Sink) {
	RecordDelegationAudit(s, hostaudit.DelegationAudit{})
	RecordReadinessAudit(s, hostaudit.ReadinessAudit{})
	RecordTurnCompletion(s)
	RecordProtocolRecovery(s, ProtocolRecoveryAudit{})
	RecordContractShadow(s, ContractShadowAudit{})
	RecordCompletionReport(s, CompletionReportAudit{})
	RecordEvidenceBundle(s, EvidenceBundleAudit{})
	RecordMemoryRecall(s, MemoryRecallAudit{})
	RecordOutcomeProgress(s, hostaudit.OutcomeSample{})
	RecordWorkspaceMutation(s, WorkspaceMutation{})
	RecordRunBudget(s, RunBudgetSample{})
	RecordProjectCheckProbe(s, ProjectCheckProbe{})
	RecordSubagentHandoff(s, SubagentHandoffAudit{})
	RecordVerificationContractDrift(s, VerificationContractDrift{})
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

func TestCapturingSinkCoversEveryCapability(t *testing.T) {
	if missing := MissingCapabilities(&capturingSink{}); len(missing) > 0 {
		t.Fatalf("test fixture is stale — implement %v so chain tests stay honest", missing)
	}
}

// TestWrappersForwardEveryCapability is the guard the real chain lacked:
// CostQuoteSink forwarded Emit only, so every audit channel died one link
// before the trajectory recorder with nothing failing.
func TestWrappersForwardEveryCapability(t *testing.T) {
	inner := &capturingSink{}
	wrappers := map[string]Sink{
		"Sync":          Sync(inner),
		"Coalesce":      Coalesce(inner, DefaultStreamDeltaWindow),
		"CostQuoteSink": NewCostQuoteSink(inner, nil),
	}
	for name, w := range wrappers {
		t.Run(name, func(t *testing.T) {
			if missing := MissingCapabilities(w); len(missing) > 0 {
				t.Fatalf("%s drops %v — embed AuditForwarder or forward them by hand", name, missing)
			}
			inner.recorded = nil
			recordAll(w)
			if got, want := len(inner.recorded), len(CapabilityNames()); got != want {
				t.Fatalf("%s delivered %d of %d capabilities: %v", name, got, want, inner.recorded)
			}
		})
	}
}

// TestSessionChainDeliversAudits mirrors boot's assembly order
// (Coalesce → Sync → CostQuote → recorder) end to end.
func TestSessionChainDeliversAudits(t *testing.T) {
	recorder := &capturingSink{}
	chain := Coalesce(Sync(NewCostQuoteSink(recorder, nil)), 5*time.Millisecond)

	RecordProtocolRecovery(chain, ProtocolRecoveryAudit{Kind: ProtocolRecoveryMissingReasoningRetryAttempted})

	if len(recorder.recorded) != 1 || recorder.recorded[0] != "ProtocolRecovery" {
		t.Fatalf("protocol recovery never reached the recorder: %v", recorder.recorded)
	}
}
