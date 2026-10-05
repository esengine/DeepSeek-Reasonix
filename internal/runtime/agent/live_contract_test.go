package agent

import (
	"reasonix/internal/state/sessionstore"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/taskcontract"
	"reasonix/internal/safety/evidence"
)

func liveContractAgent(t *testing.T, sink event.Sink) *Agent {
	t.Helper()
	a := New(nil, tool.NewRegistry(), sessionstore.NewSession(""), Options{}, sink)
	a.resetTurnEvidence()
	a.turn.turnInput = "make the cache key model-aware"
	a.SetPlanContract(new(contractPlan()))
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
		ToolName: "bash", Command: "go test ./internal/contract/provider/", Success: true,
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
	a.task.ledger.Record(evidence.Receipt{ToolName: "edit_file", Mutation: true, Write: true, Success: true, Paths: []string{"internal/contract/provider/cache.go"}})
	a.task.ledger.Record(evidence.Receipt{ToolName: "bash", Command: "go test ./internal/contract/provider/", Success: true})

	live := contractShadowAudit(a.LiveContract())
	replay := contractShadowAudit(buildShadowContract(a.turn.turnInput, a.task.ledger.Receipts(), a.PlanContract()))
	if live != replay {
		t.Fatalf("live view %+v disagrees with the end-of-turn replay %+v", live, replay)
	}
}

func TestLiveContractIsNilWithoutALedger(t *testing.T) {
	a := New(nil, tool.NewRegistry(), sessionstore.NewSession(""), Options{}, event.Discard)
	a.task.ledger = nil
	if a.LiveContract() != nil {
		t.Fatal("no ledger means no contract to answer with")
	}
}
