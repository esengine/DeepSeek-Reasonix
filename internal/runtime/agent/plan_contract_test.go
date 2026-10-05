package agent

import (
	"slices"
	"testing"

	"reasonix/internal/runtime/plancontract"
	"reasonix/internal/runtime/taskcontract"
	"reasonix/internal/safety/evidence"
)

func contractPlan() plancontract.Plan {
	return plancontract.Plan{
		Objective: "make the cache key model-aware",
		Steps: []plancontract.Step{
			{
				ID: "p1", Title: "thread the model ref through",
				VerifiedFiles:  []string{"internal/contract/provider/cache.go"},
				CandidateFiles: []string{"internal/assembly/boot/boot.go"},
				Risks:          []string{"warm caches invalidate once"},
				Acceptance: []plancontract.Criterion{
					{Text: "two model refs never share an entry"},
					{Text: "existing hits keep hitting", Regression: true},
					{Text: "the hit rate is logged", Optional: true},
				},
				Verification: []plancontract.Verification{{Command: "go test ./internal/contract/provider/"}},
			},
		},
	}.Normalize()
}

func TestPlanFactsSeparatesCriteriaByKind(t *testing.T) {
	plan := contractPlan()
	facts := planFacts(plan)
	texts := func(cs []taskcontract.PlanCriterion) []string {
		out := make([]string, 0, len(cs))
		for _, c := range cs {
			if c.ID == "" {
				t.Errorf("criterion %q lost the identity a proof must cite", c.Text)
			}
			out = append(out, c.Text)
		}
		return out
	}
	if !slices.Equal(texts(facts.AcceptanceCriteria), []string{"two model refs never share an entry"}) {
		t.Errorf("acceptance = %v", facts.AcceptanceCriteria)
	}
	if !slices.Equal(texts(facts.Regressions), []string{"existing hits keep hitting"}) {
		t.Errorf("regressions = %v", facts.Regressions)
	}
	if !slices.Equal(texts(facts.Optional), []string{"the hit rate is logged"}) {
		t.Errorf("optional = %v", facts.Optional)
	}
	// The id the plan assigned is the id the contract must carry.
	if got, want := facts.AcceptanceCriteria[0].ID, plan.Steps[0].Acceptance[0].ID; got != want {
		t.Errorf("criterion id = %q, want the plan's %q", got, want)
	}
	if !slices.Equal(facts.Verifications, []string{"go test ./internal/contract/provider/"}) {
		t.Errorf("verifications = %v", facts.Verifications)
	}
	if !facts.Risky {
		t.Error("a step carrying risks must mark the plan risky")
	}
	// Scope is where work is expected, so an inferred path belongs in it.
	if !slices.Equal(facts.Touchpoints, []string{"internal/contract/provider/cache.go", "internal/assembly/boot/boot.go"}) {
		t.Errorf("touchpoints = %v", facts.Touchpoints)
	}
}

// The contract's requirements are the plan's acceptance criteria, not a
// restatement of the step titles the todo list happens to carry.
func TestShadowContractPrefersPlanCriteriaOverTodoTitles(t *testing.T) {
	receipts := []evidence.Receipt{
		{ToolName: "todo_write", Success: true, Todos: []evidence.TodoItem{
			{Content: "thread the model ref through", Status: "completed", StepID: "p1"},
		}},
	}
	c := buildShadowContract("fix the cache key", receipts, new(contractPlan()))

	texts := make([]string, 0, len(c.Requirements))
	for _, req := range c.Requirements {
		texts = append(texts, req.Text)
	}
	if slices.Contains(texts, "thread the model ref through") {
		t.Fatalf("a todo title became a requirement alongside the plan's criteria: %v", texts)
	}
	if !slices.Contains(texts, "two model refs never share an entry") {
		t.Fatalf("requirements = %v, want the plan's acceptance criteria", texts)
	}
	if len(c.Checks) == 0 {
		t.Fatal("the plan's verification command did not become a check")
	}
}

// An optional criterion is recorded but must never hold completion open.
func TestOptionalCriterionDoesNotBlockCompletion(t *testing.T) {
	c := taskcontract.FromPlan("o", taskcontract.PlanFacts{
		AcceptanceCriteria: []taskcontract.PlanCriterion{{Text: "required one"}},
		Optional:           []taskcontract.PlanCriterion{{Text: "nice to have"}},
	})
	for _, req := range c.Requirements {
		if req.Text == "required one" {
			c.Resolve(req.ID, taskcontract.Satisfied)
		}
		if req.Text == "nice to have" && req.Required {
			t.Fatal("an optional criterion must not be required")
		}
	}
	if !c.Complete() {
		t.Fatalf("optional work left the contract incomplete: %s", c.Summary())
	}
}

func TestShadowContractFallsBackToTodosWithoutAPlan(t *testing.T) {
	receipts := []evidence.Receipt{
		{ToolName: "todo_write", Success: true, Todos: []evidence.TodoItem{
			{Content: "fix add()", Status: "completed"},
		}},
	}
	c := buildShadowContract("fix the add bug", receipts, nil)
	found := false
	for _, req := range c.Requirements {
		if req.Text == "fix add()" {
			found = true
		}
	}
	if !found {
		t.Fatal("without a plan the todo list must still stand in as the requirement set")
	}
}
