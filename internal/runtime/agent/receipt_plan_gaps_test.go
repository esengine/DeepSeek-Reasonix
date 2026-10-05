package agent

import (
	"slices"
	"strings"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/runtime/completion"
	"reasonix/internal/runtime/taskcontract"
	"reasonix/internal/state/trustedstate"
)

func unprovenDetails(r *event.CompletionReceipt) []string {
	var out []string
	for _, g := range r.Gaps {
		if g.Kind == "unproven_criterion" {
			out = append(out, g.Detail)
		}
	}
	return out
}

// The receipt answers plan criteria from the sealed verdict: the check its step
// names, run after the latest change. A criterion no command checks is listed
// as such rather than left to a citation.
func TestReceiptAnswersPlanCriteriaFromTheSealedVerdict(t *testing.T) {
	plan := verifiedPlan()
	for _, tc := range []struct {
		ran        string
		wantFailed bool
	}{{"go test ./parser/", false}, {"go vet ./...", true}} {
		a, _ := contractAgent(t, writeThenRun(tc.ran), "", trustedstate.Open(t.TempDir(), nil))
		a.SetPlanContract(&plan)
		_ = a.Run(deliveryGoalContext("goal-1", "fix"), "fix")
		details := unprovenDetails(a.CompletionReceipt())
		failed := slices.ContainsFunc(details, func(d string) bool {
			return strings.Contains(d, "quoted fields parse") && strings.Contains(d, "not passed since the last change: go test ./parser/")
		})
		if failed != tc.wantFailed {
			t.Fatalf("after %q the receipt lists %v, want the step check failing = %v", tc.ran, details, tc.wantFailed)
		}
		if !slices.ContainsFunc(details, func(d string) bool { return strings.Contains(d, "the README says so (no command checks it)") }) {
			t.Fatalf("after %q the criterion no command checks is missing from %v", tc.ran, details)
		}
		if slices.ContainsFunc(details, func(d string) bool { return strings.Contains(d, "timing is logged") }) {
			t.Fatal("an optional criterion was listed as unproven")
		}
	}
}

// Mechanism given a state: the replayed report is set by hand to the one a
// complete_step citation produces — every criterion satisfied, no gap. The
// receipt still lists the criterion its step's check did not prove.
func TestACitationNoLongerProvesAPlanCriterionOnTheReceipt(t *testing.T) {
	plan := verifiedPlan()
	a, _ := contractAgent(t, writeThenRun("go vet ./..."), "", trustedstate.Open(t.TempDir(), nil))
	a.SetPlanContract(&plan)
	_ = a.Run(deliveryGoalContext("goal-1", "fix"), "fix")
	cited := *a.turn.completion
	for i := range cited.Criteria {
		cited.Criteria[i].Status = taskcontract.Satisfied
	}
	cited.Gaps = slices.DeleteFunc(cited.Gaps, func(g completion.Gap) bool { return g.Kind == completion.GapUnprovenCriterion })
	a.turn.completion = &cited
	if details := unprovenDetails(a.CompletionReceipt()); !slices.ContainsFunc(details, func(d string) bool { return strings.Contains(d, "quoted fields parse") }) {
		t.Fatalf("receipt lists %v, want the cited but unproven criterion", details)
	}
}

// Without Trusted Host State there is no sealed verdict, and the receipt is
// the report's as before.
func TestReceiptWithoutASealedVerdictIsTheReports(t *testing.T) {
	plan := verifiedPlan()
	a, _ := contractAgent(t, writeThenRun("go test ./parser/"), "", trustedstate.Open(t.TempDir(), nil))
	a.svc.evidenceSeal = nil
	a.SetPlanContract(&plan)
	_ = a.Run(deliveryGoalContext("goal-1", "fix"), "fix")
	want := completionReceipt(*a.turn.completion)
	if got := a.CompletionReceipt(); !slices.Equal(unprovenDetails(got), unprovenDetails(want)) {
		t.Fatalf("receipt gaps %v, want the report's own %v", unprovenDetails(got), unprovenDetails(want))
	}
}
