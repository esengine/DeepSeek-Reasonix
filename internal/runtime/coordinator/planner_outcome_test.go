package coordinator

import (
	"testing"

	"reasonix/internal/runtime/plancontract"
)

func TestPlannerOutcomeReadsApprovalFromTheFieldWhenStructured(t *testing.T) {
	// Approval is a field. Prose that sounds like a request for one — the exact
	// phrase the retired fallback matched — must not gate anything.
	prose := "The plan is ready. Waiting for approval before I continue."
	structured := plannerOutcome{
		text: prose,
		exit: plannerExitPlan,
		plan: plancontract.Plan{Objective: "o", Steps: []plancontract.Step{{Title: "do it"}}},
	}
	if structured.requestsApproval() {
		t.Fatal("a structured plan must gate on requires_approval, not on its rendered prose")
	}
	structured.plan.RequiresApproval = true
	if !structured.requestsApproval() {
		t.Fatal("requires_approval must gate execution")
	}
	if (plannerOutcome{text: prose}).requestsApproval() {
		t.Fatal("prose must not request approval: the host reads the field, not the words")
	}
}
