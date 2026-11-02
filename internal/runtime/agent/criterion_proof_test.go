package agent

import (
	"reasonix/internal/state/sessionstore"
	"testing"

	"reasonix/internal/runtime/plancontract"
)

func criterionPlan() plancontract.Plan {
	return plancontract.Plan{
		Objective: "fix the retry race",
		Steps: []plancontract.Step{{
			Title: "fix it",
			Acceptance: []plancontract.Criterion{
				{Text: "retries no longer double-charge"},
				{Text: "existing payment tests keep passing", Regression: true},
			},
		}},
	}.Normalize()
}

func TestAcceptanceCriterionIDsReachTheToolCall(t *testing.T) {
	a := New(nil, nil, sessionstore.NewSession(""), Options{}, nil)
	if ids := a.acceptanceCriterionIDs(); len(ids) != 0 {
		t.Fatalf("no plan must yield no ids, got %v", ids)
	}
	plan := criterionPlan()
	a.SetPlanContract(&plan)
	ids := a.acceptanceCriterionIDs()
	if len(ids) != 2 {
		t.Fatalf("ids = %v, want both criteria of the approved plan", ids)
	}
}
