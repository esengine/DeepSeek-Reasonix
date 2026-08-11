package agent

import (
	"strings"
	"testing"

	"reasonix/internal/evidence"
	"reasonix/internal/plancontract"
	"reasonix/internal/tool"
)

func rejectionAgent(t *testing.T, plan *plancontract.Plan) *Agent {
	t.Helper()
	a := New(nil, tool.NewRegistry(), NewSession(""), Options{}, nil)
	a.resetTurnEvidence()
	a.turnInput = "fix the retry race"
	a.SetPlanContract(plan)
	return a
}

// The gate has to name what is missing. A criterion nobody proved is exactly
// what "pretending to be done" looks like from the host's side.
func TestUnprovenPlanCriterionBlocksTheFinalAnswer(t *testing.T) {
	plan := criterionPlan()
	a := rejectionAgent(t, &plan)
	a.evidence.Record(evidence.Receipt{ToolName: "edit_file", Mutation: true, Write: true, Success: true, Paths: []string{"pay.go"}})

	outstanding := a.outstandingPlanCriteria()
	if len(outstanding) == 0 {
		t.Fatal("a plan whose criteria have no evidence must have something outstanding")
	}
	joined := strings.Join(outstanding, "; ")
	if !strings.Contains(joined, "retries no longer double-charge") {
		t.Fatalf("outstanding = %v, want the criterion named", outstanding)
	}
}

// Once every criterion carries fresh proof the gate must let go, or the model
// can never finish.
func TestProvenPlanCriteriaLeaveNothingOutstanding(t *testing.T) {
	plan := criterionPlan()
	a := rejectionAgent(t, &plan)
	for _, c := range plan.Steps[0].Acceptance {
		a.evidence.Record(completeStepReceipt(t, c.ID, "manual", ""))
	}
	if outstanding := a.outstandingPlanCriteria(); len(outstanding) != 0 {
		t.Fatalf("outstanding = %v, want none once every criterion is proven", outstanding)
	}
}

// Freshness has to survive the trip into the gate: a proof from before the last
// mutation is not a proof of the current code.
func TestStaleProofBecomesOutstandingAgain(t *testing.T) {
	plan := criterionPlan()
	a := rejectionAgent(t, &plan)
	for _, c := range plan.Steps[0].Acceptance {
		a.evidence.Record(completeStepReceipt(t, c.ID, "verification", "go test ./..."))
	}
	if outstanding := a.outstandingPlanCriteria(); len(outstanding) != 0 {
		t.Fatalf("outstanding = %v, want none while the proofs are fresh", outstanding)
	}

	a.evidence.Record(evidence.Receipt{ToolName: "edit_file", Mutation: true, Write: true, Success: true, Paths: []string{"pay.go"}})
	outstanding := a.outstandingPlanCriteria()
	if len(outstanding) == 0 {
		t.Fatal("a mutation after the proofs must reopen them")
	}
	if !strings.Contains(strings.Join(outstanding, "; "), "stale") {
		t.Fatalf("outstanding = %v, want the staleness said out loud", outstanding)
	}
}

// An unplanned turn must not inherit a contract it never agreed to.
func TestUnplannedTurnHasNoOutstandingCriteria(t *testing.T) {
	a := rejectionAgent(t, nil)
	a.evidence.Record(evidence.Receipt{ToolName: "edit_file", Mutation: true, Write: true, Success: true, Paths: []string{"pay.go"}})
	if outstanding := a.outstandingPlanCriteria(); len(outstanding) != 0 {
		t.Fatalf("outstanding = %v, want none without an approved plan", outstanding)
	}
}
