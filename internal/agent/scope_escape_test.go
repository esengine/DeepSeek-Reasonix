package agent

import (
	"encoding/json"
	"testing"

	"reasonix/internal/plancontract"
	"reasonix/internal/tool"
)

func scopedAgent(t *testing.T, plan *plancontract.Plan) *Agent {
	t.Helper()
	a := New(nil, tool.NewRegistry(), NewSession(""), Options{}, nil)
	a.SetPlanContract(plan)
	return a
}

func writeArgs(path string) json.RawMessage {
	return json.RawMessage(`{"path":"` + path + `","content":"x"}`)
}

// The question the user actually approved an answer to: is this write outside
// the plan they agreed on. The existing rule only asks whether a retry drifts
// from the call that failed, which says nothing about the plan.
func TestMutationOutsideThePlanEscapes(t *testing.T) {
	plan := plancontract.Plan{Objective: "o", Steps: []plancontract.Step{{
		Title:         "fix the retry race",
		VerifiedFiles: []string{"internal/pay/retry.go"},
	}}}.Normalize()
	a := scopedAgent(t, &plan)

	if a.mutationEscapesPlan("write_file", writeArgs("internal/billing/invoice.go")) != true {
		t.Fatal("a write the plan never named must read as leaving the approved scope")
	}
}

// A plan naming a file implies its directory is in play, which is how the test
// file beside it stays in scope.
func TestMutationBesideAPlannedFileStaysInScope(t *testing.T) {
	plan := plancontract.Plan{Objective: "o", Steps: []plancontract.Step{{
		Title:         "fix the retry race",
		VerifiedFiles: []string{"internal/pay/retry.go"},
	}}}.Normalize()
	a := scopedAgent(t, &plan)

	for _, path := range []string{"internal/pay/retry.go", "internal/pay/retry_test.go"} {
		if a.mutationEscapesPlan("write_file", writeArgs(path)) {
			t.Errorf("%s is inside the planned surface", path)
		}
	}
}

// A candidate path was inferred rather than read, but the plan still named it as
// where the work is expected — scope is an expectation, not a claim of fact.
func TestCandidatePathsCountAsScope(t *testing.T) {
	plan := plancontract.Plan{Objective: "o", Steps: []plancontract.Step{{
		Title:          "fix it",
		CandidateFiles: []string{"internal/boot/boot.go"},
	}}}.Normalize()
	a := scopedAgent(t, &plan)

	if a.mutationEscapesPlan("write_file", writeArgs("internal/boot/boot.go")) {
		t.Fatal("a candidate path is inside the planned surface")
	}
}

// Silence is not a claim that everything is out of bounds.
func TestNoPlanAndNoTouchpointsNeverEscape(t *testing.T) {
	if scopedAgent(t, nil).mutationEscapesPlan("write_file", writeArgs("anything.go")) {
		t.Error("an unplanned turn has no approved surface to leave")
	}
	silent := plancontract.Plan{Objective: "o", Steps: []plancontract.Step{{Title: "do it"}}}.Normalize()
	if scopedAgent(t, &silent).mutationEscapesPlan("write_file", writeArgs("anything.go")) {
		t.Error("a plan that named no touchpoints says nothing about scope")
	}
}
