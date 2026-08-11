package agent

import (
	"encoding/json"
	"testing"

	"reasonix/internal/evidence"
	"reasonix/internal/plancontract"
	"reasonix/internal/taskcontract"
)

func completeStepReceipt(t *testing.T, criterionID, kind, command string) evidence.Receipt {
	t.Helper()
	args, err := json.Marshal(map[string]any{
		"step":   "do it",
		"result": "done",
		"evidence": []map[string]any{
			{"kind": kind, "summary": "proved it", "command": command, "criterion_id": criterionID},
		},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return evidence.Receipt{ToolName: "complete_step", Success: true, Step: "do it", Args: args}
}

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

func requirementByID(c *taskcontract.Contract, id string) (taskcontract.Requirement, bool) {
	for _, req := range c.Requirements {
		if req.ID == id {
			return req, true
		}
	}
	return taskcontract.Requirement{}, false
}

// A command succeeding is not a criterion being met. The citation is what binds
// the proof to the claim, and without it the criterion stays unproven.
func TestCitedCriterionBecomesSatisfied(t *testing.T) {
	plan := criterionPlan()
	first := plan.Steps[0].Acceptance[0].ID

	uncited := buildShadowContract("fix it", []evidence.Receipt{
		{ToolName: "bash", Command: "go test ./...", Success: true},
	}, &plan)
	if req, ok := requirementByID(uncited, first); !ok || req.Status == taskcontract.Satisfied {
		t.Fatalf("an uncited passing command must not satisfy a criterion: %+v", req)
	}

	cited := buildShadowContract("fix it", []evidence.Receipt{
		{ToolName: "bash", Command: "go test ./...", Success: true},
		completeStepReceipt(t, first, "verification", "go test ./..."),
	}, &plan)
	req, ok := requirementByID(cited, first)
	if !ok || req.Status != taskcontract.Satisfied {
		t.Fatalf("the cited criterion was not satisfied: %+v", req)
	}
	if len(req.Evidence) == 0 {
		t.Fatal("a satisfied criterion must carry the proof that satisfied it")
	}
}

// Freshness is the point: a criterion proven by a test run stops counting once
// the code changes underneath it.
func TestVerifiedCriterionGoesStaleAfterALaterMutation(t *testing.T) {
	plan := criterionPlan()
	id := plan.Steps[0].Acceptance[0].ID

	c := buildShadowContract("fix it", []evidence.Receipt{
		{ToolName: "bash", Command: "go test ./...", Success: true},
		completeStepReceipt(t, id, "verification", "go test ./..."),
		{ToolName: "edit_file", Mutation: true, Write: true, Success: true, Paths: []string{"pay.go"}},
	}, &plan)

	req, ok := requirementByID(c, id)
	if !ok || req.Status != taskcontract.Stale {
		t.Fatalf("status = %v, want Stale after a mutation landed on top of the proof:\n%s", req.Status, c.Graph())
	}
}

// Mutation-kind evidence records that a change happened, which a later change
// cannot un-happen; whether the fix still holds is verification's job.
func TestMutationProofDoesNotGoStale(t *testing.T) {
	plan := criterionPlan()
	id := plan.Steps[0].Acceptance[0].ID

	c := buildShadowContract("fix it", []evidence.Receipt{
		completeStepReceipt(t, id, "diff", ""),
		{ToolName: "edit_file", Mutation: true, Write: true, Success: true, Paths: []string{"other.go"}},
	}, &plan)

	req, _ := requirementByID(c, id)
	if req.Status != taskcontract.Satisfied {
		t.Fatalf("status = %v, want Satisfied; mutation evidence must not stale", req.Status)
	}
}

func TestUnknownCriterionCitationIsIgnoredByTheReplay(t *testing.T) {
	plan := criterionPlan()
	c := buildShadowContract("fix it", []evidence.Receipt{
		completeStepReceipt(t, "c99", "verification", "go test ./..."),
	}, &plan)
	for _, req := range c.Requirements {
		if req.Status == taskcontract.Satisfied {
			t.Fatalf("a bogus citation satisfied %q", req.ID)
		}
	}
}

func TestAcceptanceCriterionIDsReachTheToolCall(t *testing.T) {
	a := New(nil, nil, NewSession(""), Options{}, nil)
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
