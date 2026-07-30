package control

import (
	"encoding/json"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/evidence"
)

func TestParsePlanTodos(t *testing.T) {
	tests := []struct {
		name string
		plan string
		want []seedTodo
	}{
		{
			name: "bulleted list",
			plan: "Here's the plan:\n- Add the parser\n- Wire it up\n- Add tests",
			want: []seedTodo{
				{Content: "Add the parser", Status: "in_progress"},
				{Content: "Wire it up", Status: "pending"},
				{Content: "Add tests", Status: "pending"},
			},
		},
		{
			name: "numbered list with both . and ) delimiters",
			plan: "1. First step\n2) Second step",
			want: []seedTodo{
				{Content: "First step", Status: "in_progress"},
				{Content: "Second step", Status: "pending"},
			},
		},
		{
			name: "strips inline markdown and checkbox syntax",
			plan: "- [ ] **Add** the `parser`\n* Plain item",
			want: []seedTodo{
				{Content: "Add the parser", Status: "in_progress"},
				{Content: "Plain item", Status: "pending"},
			},
		},
		{
			name: "prose without list items yields nothing (the model's todo_write covers it)",
			plan: "总结：这是一个简单的三步骤测试——创建文件 → 编辑文件 → 删除文件。",
			want: nil,
		},
		{
			name: "numbered list embedded in a longer plan",
			plan: "My understanding:\n1. Create the file\n2. Write content\n3. Delete it\n\nReady when you are.",
			want: []seedTodo{
				{Content: "Create the file", Status: "in_progress"},
				{Content: "Write content", Status: "pending"},
				{Content: "Delete it", Status: "pending"},
			},
		},
		{
			name: "a digit run that isn't a list item is ignored",
			plan: "Version 2 is the target.\n- Real item",
			want: []seedTodo{{Content: "Real item", Status: "in_progress"}},
		},
		{
			name: "two-level plan: phases at level 0, indented sub-steps at level 1",
			plan: "1. Add the loader\n   - parse the TOML\n   - validate fields\n2. Wire it up\n  - call from boot",
			want: []seedTodo{
				{Content: "Add the loader", Status: "pending", Level: 0},
				{Content: "parse the TOML", Status: "in_progress", Level: 1},
				{Content: "validate fields", Status: "pending", Level: 1},
				{Content: "Wire it up", Status: "pending", Level: 0},
				{Content: "call from boot", Status: "pending", Level: 1},
			},
		},
		{
			name: "tab-indented sub-step is level 1",
			plan: "- Phase one\n\t- nested by tab",
			want: []seedTodo{
				{Content: "Phase one", Status: "pending", Level: 0},
				{Content: "nested by tab", Status: "in_progress", Level: 1},
			},
		},
		{
			// The real model wrote phases as numbered ### headings with indented
			// bullet sub-steps, and a leading "## Plan" title — the shape a live
			// run surfaced that flat list-only parsing collapsed to all level 1.
			name: "numbered headings are phases; title is ignored; bullets are sub-steps",
			plan: "## Plan: add a flag\n\n### 1. Define the field\n   - add Verbose bool\n   - document it\n\n### 2. Wire it up\n   - read from config",
			want: []seedTodo{
				{Content: "Define the field", Status: "pending", Level: 0},
				{Content: "add Verbose bool", Status: "in_progress", Level: 1},
				{Content: "document it", Status: "pending", Level: 1},
				{Content: "Wire it up", Status: "pending", Level: 0},
				{Content: "read from config", Status: "pending", Level: 1},
			},
		},
		{
			name: "leading indented item is promoted instead of seeding an orphan sub-step",
			plan: "  - recover configuration\n- restart normally",
			want: []seedTodo{
				{Content: "recover configuration", Status: "in_progress", Level: 0},
				{Content: "restart normally", Status: "pending", Level: 0},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parsePlanTodos(tc.plan)
			if len(got) != len(tc.want) {
				t.Fatalf("got %d todos, want %d: %+v", len(got), len(tc.want), got)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("todo %d = %+v, want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestPlanTodosJSONAlwaysSatisfiesSerialContract(t *testing.T) {
	plans := []string{
		"1. Flat first\n2. Flat second",
		"1. Phase one\n   - child one\n   - child two\n2. Phase two\n   - child three",
		"   - accidentally indented first\n   - nested child\n2. Next phase",
	}
	for _, plan := range plans {
		args := PlanTodosJSON(plan)
		if args == "" {
			t.Fatalf("PlanTodosJSON(%q) returned no seed", plan)
		}
		var payload struct {
			Todos []evidence.TodoItem `json:"todos"`
		}
		if err := json.Unmarshal([]byte(args), &payload); err != nil {
			t.Fatalf("PlanTodosJSON(%q): %v", plan, err)
		}
		if err := evidence.ValidateSerialTodos(payload.Todos); err != nil {
			t.Fatalf("PlanTodosJSON(%q) emitted invalid state: %v\n%s", plan, err, args)
		}
	}
}

func TestParsePlanTodosCapsAtTwenty(t *testing.T) {
	plan := ""
	for i := 0; i < 30; i++ {
		plan += "- item\n"
	}
	if got := parsePlanTodos(plan); len(got) != 20 {
		t.Fatalf("got %d todos, want cap of 20", len(got))
	}
}

func TestSeedPlanTodosSeedsAgentState(t *testing.T) {
	var events []event.Event
	sink := event.FuncSink(func(e event.Event) { events = append(events, e) })
	executor := &agent.Agent{}
	c := &Controller{
		sink:     sink,
		executor: executor,
	}
	plan := "1. Add the parser\n2. Wire it up\n3. Add tests"
	args := c.seedPlanTodos(plan)
	if args == "" {
		t.Fatal("seedPlanTodos returned empty args for a valid plan")
	}
	var todoDispatches, todoResults int
	for _, e := range events {
		if e.Kind == event.ToolDispatch && e.Tool.Name == "todo_write" {
			todoDispatches++
		}
		if e.Kind == event.ToolResult && e.Tool.Name == "todo_write" {
			todoResults++
		}
	}
	if todoDispatches != 1 || todoResults != 1 {
		t.Fatalf("plan-seed events: %d dispatches, %d results; want 1,1", todoDispatches, todoResults)
	}
	if got := executor.CanonicalTodoState(); len(got) != 3 || got[0].Content != "Add the parser" || got[0].Status != "in_progress" {
		t.Fatalf("seedPlanTodos did not seed canonical todo state: %+v", got)
	}
}

func TestSeedPlanTodosEmptyPlanNoOp(t *testing.T) {
	c := &Controller{
		sink:     event.Discard,
		executor: &agent.Agent{},
	}
	args := c.seedPlanTodos("no list items here")
	if args != "" {
		t.Fatalf("empty plan returned args = %q, want empty", args)
	}
}

func TestCompletePlanTodosMirrorsAgentState(t *testing.T) {
	var events []event.Event
	sink := event.FuncSink(func(e event.Event) { events = append(events, e) })
	executor := &agent.Agent{}
	c := &Controller{
		sink:     sink,
		executor: executor,
	}

	args := c.seedPlanTodos("1. Add the parser\n2. Wire it up")
	c.completePlanTodos(args)

	got := executor.CanonicalTodoState()
	if len(got) != 2 {
		t.Fatalf("canonical todo count = %d, want 2: %+v", len(got), got)
	}
	for i, todo := range got {
		if todo.Status != "completed" {
			t.Fatalf("canonical todo %d status = %q, want completed: %+v", i, todo.Status, got)
		}
	}

	var completedResults int
	for _, e := range events {
		if e.Kind == event.ToolResult && e.Tool.Name == "todo_write" && e.Tool.Output == "approved plan finished" {
			completedResults++
		}
	}
	if completedResults != 1 {
		t.Fatalf("completed plan UI events = %d, want 1", completedResults)
	}
}
