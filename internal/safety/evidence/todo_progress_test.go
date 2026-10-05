package evidence

import "testing"

func todos(items ...TodoItem) []TodoItem { return items }

func step(id, content, status string) TodoItem {
	return TodoItem{StepID: id, Content: content, Status: status}
}

// The classification reads the list's structure. Every case below is a shape a
// real turn produced; the rewrite cases are the ones the 15.6-hour session
// spent twelve hours on.
func TestClassifyTodoTransition(t *testing.T) {
	for _, tc := range []struct {
		name   string
		before []TodoItem
		after  []TodoItem
		want   TodoTransition
	}{
		{"first list is a plan",
			nil,
			todos(step("s1", "add parser", "in_progress")),
			TodoReplan},
		{"same list, same words",
			todos(step("s1", "add parser", "in_progress"), step("s2", "add tests", "pending")),
			todos(step("s1", "add parser", "in_progress"), step("s2", "add tests", "pending")),
			TodoUnchanged},
		{"restated more precisely, execution unmoved",
			todos(step("s1", "check login expiry", "in_progress"), step("s2", "add tests", "pending")),
			todos(step("s1", "verify the login expiry path", "in_progress"), step("s2", "add tests", "pending")),
			TodoRewrite},
		{"a step completed and the next became current",
			todos(step("s1", "add parser", "in_progress"), step("s2", "add tests", "pending")),
			todos(step("s1", "add parser", "completed"), step("s2", "add tests", "in_progress")),
			TodoAdvance},
		{"the last step completed",
			todos(step("s1", "add parser", "completed"), step("s2", "add tests", "in_progress")),
			todos(step("s1", "add parser", "completed"), step("s2", "add tests", "completed")),
			TodoTerminal},
		{"a step was added",
			todos(step("s1", "add parser", "in_progress")),
			todos(step("s1", "add parser", "in_progress"), step("s2", "add tests", "pending")),
			TodoReplan},
		{"a step was dropped",
			todos(step("s1", "add parser", "in_progress"), step("s2", "add tests", "pending")),
			todos(step("s1", "add parser", "in_progress")),
			TodoReplan},
		{"completed work was reopened",
			todos(step("s1", "add parser", "completed"), step("s2", "add tests", "in_progress")),
			todos(step("s1", "add parser", "in_progress"), step("s2", "add tests", "pending")),
			TodoRegression},
		{"lists without ids match on text",
			todos(step("", "add parser", "in_progress")),
			todos(step("", "add parser", "completed")),
			TodoTerminal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyTodoTransition(tc.before, tc.after); got != tc.want {
				t.Errorf("ClassifyTodoTransition = %q, want %q", got, tc.want)
			}
		})
	}
}

// Rewriting the plan is how a stalled turn would renew a progress lease it has
// not earned, so replan must never read as advancement.
func TestOnlyExecutionMovementAdvances(t *testing.T) {
	for _, tr := range []TodoTransition{TodoUnchanged, TodoRewrite, TodoReplan, TodoRegression} {
		if tr.Advances() {
			t.Errorf("%q reports as advancement", tr)
		}
	}
	for _, tr := range []TodoTransition{TodoAdvance, TodoTerminal} {
		if !tr.Advances() {
			t.Errorf("%q does not report as advancement", tr)
		}
	}
}
