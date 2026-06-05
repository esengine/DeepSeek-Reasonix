package control

import "testing"

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
				{Content: "Add the loader", Status: "in_progress", Level: 0},
				{Content: "parse the TOML", Status: "pending", Level: 1},
				{Content: "validate fields", Status: "pending", Level: 1},
				{Content: "Wire it up", Status: "pending", Level: 0},
				{Content: "call from boot", Status: "pending", Level: 1},
			},
		},
		{
			name: "tab-indented sub-step is level 1",
			plan: "- Phase one\n\t- nested by tab",
			want: []seedTodo{
				{Content: "Phase one", Status: "in_progress", Level: 0},
				{Content: "nested by tab", Status: "pending", Level: 1},
			},
		},
		{
			// The real model wrote phases as numbered ### headings with indented
			// bullet sub-steps, and a leading "## Plan" title — the shape a live
			// run surfaced that flat list-only parsing collapsed to all level 1.
			name: "numbered headings are phases; title is ignored; bullets are sub-steps",
			plan: "## Plan: add a flag\n\n### 1. Define the field\n   - add Verbose bool\n   - document it\n\n### 2. Wire it up\n   - read from config",
			want: []seedTodo{
				{Content: "Define the field", Status: "in_progress", Level: 0},
				{Content: "add Verbose bool", Status: "pending", Level: 1},
				{Content: "document it", Status: "pending", Level: 1},
				{Content: "Wire it up", Status: "pending", Level: 0},
				{Content: "read from config", Status: "pending", Level: 1},
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

func TestParsePlanTodosCapsAtTwenty(t *testing.T) {
	plan := ""
	for i := 0; i < 30; i++ {
		plan += "- item\n"
	}
	if got := parsePlanTodos(plan); len(got) != 20 {
		t.Fatalf("got %d todos, want cap of 20", len(got))
	}
}
