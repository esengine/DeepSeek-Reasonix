package control

import "testing"

func TestTaskWarrantsPlanner(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"", false},
		{"   ", false},
		{"/init", false},
		{"what does this function do?", false}, // low-risk question → executor only
		{"why did the test fail", false},
		{"解释一下这段代码", false},
		{reasoningLanguageBlock("zh") + "\n\nwhat does this function do?", false},
		{reasoningLanguageBlock("en") + "\n\n" + PlanModeMarker + "\n\nfix the bug", false},
		{reasoningLanguageBlock("en") + "\n\nfix the bug", true},
		{"fix the bug", true},        // terse, but a work request → still planned
		{"add a login button", true}, // ditto
		{"implement the new caching layer across the backend", true},
		// Goal synthetic messages — should skip planner:
		{goalContinueTurn, false},
		{goalSelfCheckTurn, false},
		{"No tool calls in recent turns. Either make progress with tools or signal [goal:blocked:<reason>].", false},
		{"Goal signaled complete but issues remain:\n- the following tasks are still incomplete:\n  - Fix login (in_progress)\nFix or use todo_write/complete_step to mark done, then [goal:complete] again.", false},
	}
	for _, c := range cases {
		if got := TaskWarrantsPlanner(c.input); got != c.want {
			t.Errorf("TaskWarrantsPlanner(%q) = %v, want %v", c.input, got, c.want)
		}
	}
}

func TestAutoPlanScore_skipsSyntheticMessages(t *testing.T) {
	syntheticInputs := []string{
		"Plan approved — plan mode is off",
		"Host final-answer readiness check failed. Before giving a final answer, address the missing host-observable receipts: missing evidence.",
		"You are already in the executor phase. The planner's read-only limitations do not apply to you.",
		"The previous assistant response was interrupted while a tool call was streaming. Continue the same task now.",
		"The previous assistant response was interrupted during streaming. Continue the same task from immediately after the partial assistant message above.",
		"The previous assistant response was interrupted during streaming before visible answer text was completed. Continue the same task now.",
		"The previous assistant response finished without any visible answer text. Continue the same task now and provide a concise visible answer.",
		goalContinueTurn,
		goalSelfCheckTurn,
		"No tool calls in recent turns. Either make progress with tools or signal [goal:blocked:<reason>].",
		"Goal signaled complete but issues remain:\n- the following tasks are still incomplete:\n  - Fix login (in_progress)\nFix or use todo_write/complete_step to mark done, then [goal:complete] again.",
	}
	for _, input := range syntheticInputs {
		if got := autoPlanScore(input); got != 0 {
			t.Errorf("autoPlanScore(%q) = %d, want 0 (synthetic messages should score 0)", input, got)
		}
	}
}

func TestAutoPlanScore_normalInputs(t *testing.T) {
	cases := []struct {
		input string
		want  int
	}{
		{"", 0},
		{"   ", 0},
		{"/init", 0},
		{"what does this function do?", 0}, // low-risk
		{"fix the bug", 0},                 // no trigger patterns
		// just "implement" alone → complexIntentTerms match
		{"implement", 1},
		// just "across" → multiSurfaceTerms match
		{"across", 1},
	}
	for _, c := range cases {
		if got := autoPlanScore(c.input); got != c.want {
			t.Errorf("autoPlanScore(%q) = %d, want %d", c.input, got, c.want)
		}
	}
}
