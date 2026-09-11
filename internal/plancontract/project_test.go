package plancontract

import (
	"testing"

	"reasonix/internal/evidence"
)

func TestProjectTodosBuildsTheTwoLevelList(t *testing.T) {
	todos := ProjectTodos(Plan{Objective: "o", Steps: []Step{
		{ID: "p1", Title: "phase one"},
		{ID: "a", ParentID: "p1", Title: "one-a"},
		{ID: "b", ParentID: "p1", Title: "one-b"},
		{ID: "p2", Title: "phase two"},
	}})

	want := []evidence.TodoItem{
		{Content: "phase one", Status: "pending", Level: 0, StepID: "p1"},
		{Content: "one-a", Status: "in_progress", Level: 1, StepID: "a"},
		{Content: "one-b", Status: "pending", Level: 1, StepID: "b"},
		{Content: "phase two", Status: "pending", Level: 0, StepID: "p2"},
	}
	if len(todos) != len(want) {
		t.Fatalf("todos = %+v, want %d items", todos, len(want))
	}
	for i := range want {
		if todos[i] != want[i] {
			t.Errorf("todo %d = %+v, want %+v", i, todos[i], want[i])
		}
	}
}

func TestProjectTodosAlwaysSatisfiesTheSerialContract(t *testing.T) {
	plans := map[string]Plan{
		"flat":            {Objective: "o", Steps: []Step{{Title: "one"}, {Title: "two"}}},
		"single phase":    {Objective: "o", Steps: []Step{{ID: "p", Title: "phase"}, {ParentID: "p", Title: "child"}}},
		"orphan first":    {Objective: "o", Steps: []Step{{ID: "kid", ParentID: "ghost", Title: "kid"}, {ID: "p", Title: "phase"}}},
		"deeply nested":   {Objective: "o", Steps: []Step{{ID: "p", Title: "phase"}, {ID: "c", ParentID: "p", Title: "child"}, {ID: "g", ParentID: "c", Title: "grandchild"}}},
		"cyclic siblings": {Objective: "o", Steps: []Step{{ID: "a", Title: "a", DependsOn: []string{"b"}}, {ID: "b", Title: "b", DependsOn: []string{"a"}}}},
	}
	for name, plan := range plans {
		t.Run(name, func(t *testing.T) {
			todos := ProjectTodos(plan)
			if len(todos) != len(plan.Steps) {
				t.Fatalf("projected %d todos from %d steps: %+v", len(todos), len(plan.Steps), todos)
			}
			if err := evidence.ValidateSerialTodos(todos); err != nil {
				t.Fatalf("projection violates the serial contract: %v (%+v)", err, todos)
			}
		})
	}
}

func TestProjectTodosCarriesStableStepIdentity(t *testing.T) {
	// The plan already knows each step's identity; dropping it at the projection
	// boundary is what forces completion attribution back onto title and index.
	p := Plan{Objective: "o", Steps: []Step{
		{Title: "change the DB"},
		{Title: "change the API"},
		{Title: "change the API"}, // deliberately identical wording
	}}
	todos := ProjectTodos(p)
	ordered := p.Normalize().Ordered()
	seen := make(map[string]bool, len(todos))
	for i, todo := range todos {
		if todo.StepID == "" {
			t.Fatalf("todo %d %q lost its step id", i, todo.Content)
		}
		if todo.StepID != ordered[i].ID {
			t.Errorf("todo %d step id = %q, want the plan's %q", i, todo.StepID, ordered[i].ID)
		}
		if seen[todo.StepID] {
			t.Fatalf("todo %d reuses step id %q; identically worded steps must stay distinct", i, todo.StepID)
		}
		seen[todo.StepID] = true
	}
}

func TestProjectTodosReturnsNilWithoutSteps(t *testing.T) {
	if todos := ProjectTodos(Plan{Objective: "o"}); todos != nil {
		t.Fatalf("todos = %+v, want nil", todos)
	}
	if todos := ProjectTodos(Plan{Objective: "o", Steps: []Step{{Title: "  "}}}); todos != nil {
		t.Fatalf("todos = %+v, want nil for a plan whose only step is blank", todos)
	}
}

func TestProjectTodosMatchesTheRenderedOrder(t *testing.T) {
	p := Plan{Objective: "o", Steps: []Step{
		{ID: "p", Title: "phase"},
		{ID: "late", ParentID: "p", Title: "late", DependsOn: []string{"early"}},
		{ID: "early", ParentID: "p", Title: "early"},
	}}
	todos := ProjectTodos(p)
	ordered := p.Normalize().Ordered()
	for i := range ordered {
		if todos[i].Content != ordered[i].Title {
			t.Fatalf("todo %d = %q, want %q — the seeded list must match the approved one", i, todos[i].Content, ordered[i].Title)
		}
	}
}
