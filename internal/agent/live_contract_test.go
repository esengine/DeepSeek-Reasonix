package agent

import (
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/taskcontract"
	"reasonix/internal/tool"
)

type contractShadowSink struct {
	audits []event.ContractShadowAudit
}

func (s *contractShadowSink) Emit(event.Event) {}
func (s *contractShadowSink) RecordContractShadow(a event.ContractShadowAudit) {
	s.audits = append(s.audits, a)
}

func liveContractAgent(t *testing.T, sink event.Sink) *Agent {
	t.Helper()
	a := New(nil, tool.NewRegistry(), NewSession(""), Options{}, sink)
	a.resetTurnEvidence()
	a.turnInput = "make the cache key model-aware"
	a.SetPlanContract(ptr(contractPlan()))
	return a
}

// The contract has to be answerable while the turn is running, not only after
// it: a gate that asks "is anything still unproven" has nothing to ask today.
func TestLiveContractReflectsEvidenceAsItLands(t *testing.T) {
	a := liveContractAgent(t, event.Discard)

	before := a.LiveContract()
	if before == nil || before.Complete() {
		t.Fatalf("a plan with unproven criteria must not start complete: %+v", before)
	}
	startingChecks := 0
	for _, check := range before.Checks {
		if check.Status == taskcontract.Satisfied {
			startingChecks++
		}
	}

	a.task.ledger.Record(evidence.Receipt{
		ToolName: "bash", Command: "go test ./internal/provider/", Success: true,
	})
	after := a.LiveContract()
	satisfied := 0
	for _, check := range after.Checks {
		if check.Status == taskcontract.Satisfied {
			satisfied++
		}
	}
	if satisfied <= startingChecks {
		t.Fatalf("a passing verification did not satisfy the plan's check: %s", after.Graph())
	}
}

// One replay serves both views, so the live contract and the turn's record can
// never disagree about the same receipts.
func TestLiveContractMatchesTheEndOfTurnReplay(t *testing.T) {
	a := liveContractAgent(t, event.Discard)
	a.task.ledger.Record(evidence.Receipt{ToolName: "edit_file", Mutation: true, Write: true, Success: true, Paths: []string{"internal/provider/cache.go"}})
	a.task.ledger.Record(evidence.Receipt{ToolName: "bash", Command: "go test ./internal/provider/", Success: true})

	live := contractShadowAudit(a.LiveContract())
	replay := contractShadowAudit(buildShadowContract(a.turnInput, a.task.ledger.Receipts(), a.planContractSnapshot()))
	if live != replay {
		t.Fatalf("live view %+v disagrees with the end-of-turn replay %+v", live, replay)
	}
}

func TestContractRoundObservationRecordsATimeSeries(t *testing.T) {
	sink := &contractShadowSink{}
	a := liveContractAgent(t, sink)

	a.observeContractRound()
	a.task.ledger.Record(evidence.Receipt{ToolName: "bash", Command: "go test ./internal/provider/", Success: true})
	a.observeContractRound()

	if len(sink.audits) != 2 {
		t.Fatalf("recorded %d contract samples, want one per round", len(sink.audits))
	}
	if sink.audits[0].ChecksSatisfied >= sink.audits[1].ChecksSatisfied {
		t.Fatalf("the series does not show the check being satisfied: %+v", sink.audits)
	}
}

// A turn with nothing to prove must not spray empty samples into the trajectory.
func TestContractRoundObservationSkipsAnEmptyContract(t *testing.T) {
	sink := &contractShadowSink{}
	a := New(nil, tool.NewRegistry(), NewSession(""), Options{}, sink)
	a.resetTurnEvidence()
	a.turnInput = "what does this function do?"

	a.observeContractRound()
	if len(sink.audits) != 0 {
		t.Fatalf("recorded %d samples for a contract with nothing to prove", len(sink.audits))
	}
}

func TestLiveContractIsNilWithoutALedger(t *testing.T) {
	a := New(nil, tool.NewRegistry(), NewSession(""), Options{}, event.Discard)
	a.task.ledger = nil
	if a.LiveContract() != nil {
		t.Fatal("no ledger means no contract to answer with")
	}
}
