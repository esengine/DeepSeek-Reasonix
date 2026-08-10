package agent

import (
	"context"
	"slices"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/plancontract"
	"reasonix/internal/provider"
	"reasonix/internal/taskcontract"
)

func contractPlan() plancontract.Plan {
	return plancontract.Plan{
		Objective: "make the cache key model-aware",
		Steps: []plancontract.Step{
			{
				ID: "p1", Title: "thread the model ref through",
				VerifiedFiles:  []string{"internal/provider/cache.go"},
				CandidateFiles: []string{"internal/boot/boot.go"},
				Risks:          []string{"warm caches invalidate once"},
				Acceptance: []plancontract.Criterion{
					{Text: "two model refs never share an entry"},
					{Text: "existing hits keep hitting", Regression: true},
					{Text: "the hit rate is logged", Optional: true},
				},
				Verification: []plancontract.Verification{{Command: "go test ./internal/provider/"}},
			},
		},
	}.Normalize()
}

func TestPlanFactsSeparatesCriteriaByKind(t *testing.T) {
	facts := planFacts(contractPlan())
	if !slices.Equal(facts.AcceptanceCriteria, []string{"two model refs never share an entry"}) {
		t.Errorf("acceptance = %v", facts.AcceptanceCriteria)
	}
	if !slices.Equal(facts.Regressions, []string{"existing hits keep hitting"}) {
		t.Errorf("regressions = %v", facts.Regressions)
	}
	if !slices.Equal(facts.Optional, []string{"the hit rate is logged"}) {
		t.Errorf("optional = %v", facts.Optional)
	}
	if !slices.Equal(facts.Verifications, []string{"go test ./internal/provider/"}) {
		t.Errorf("verifications = %v", facts.Verifications)
	}
	if !facts.Risky {
		t.Error("a step carrying risks must mark the plan risky")
	}
	// Scope is where work is expected, so an inferred path belongs in it.
	if !slices.Equal(facts.Touchpoints, []string{"internal/provider/cache.go", "internal/boot/boot.go"}) {
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
	c := buildShadowContract("fix the cache key", receipts, ptr(contractPlan()))

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
		AcceptanceCriteria: []string{"required one"},
		Optional:           []string{"nice to have"},
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

// A turn must never inherit the previous turn's plan: the executor-only route
// runs with no contract at all.
func TestCoordinatorClearsThePlanContractEachTurn(t *testing.T) {
	planner := &mockProvider{name: "planner", streams: submitPlanCall(e2ePlanArgs)}
	exec := &mockProvider{name: "executor", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "Done."},
		{Type: provider.ChunkDone},
	}}
	coord, executor := submitPlanCoordinator(t, planner, exec, event.Discard)

	if err := coord.Run(context.Background(), "fix the cache key"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if executor.planContractSnapshot() == nil {
		t.Fatal("the approved plan never reached the executor's contract")
	}

	coord.plannerPolicy = func(context.Context, string) PlannerDecision {
		return PlannerDecision{Route: PlannerRouteExecutorOnly, Reason: "test"}
	}
	if err := coord.Run(context.Background(), "just answer me"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if executor.planContractSnapshot() != nil {
		t.Fatal("an executor-only turn inherited the previous turn's plan")
	}
}

func ptr[T any](v T) *T { return &v }
