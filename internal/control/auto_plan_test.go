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
		// Synthetic goal-loop messages should NOT trigger the planner.
		{activeGoalBlock("execute plan: fix the parser", GoalResearchAuto) + "\n\n" + goalContinueTurn, false},
		{activeGoalBlock("execute plan: fix the parser", GoalResearchAuto) + "\n\n" + goalSelfCheckTurn, false},
		// Normal user input inside an active-goal block should still trigger.
		{activeGoalBlock("implement the new caching layer", GoalResearchAuto) + "\n\nimplement the new caching layer across the backend", true},
	}
	for _, c := range cases {
		if got := TaskWarrantsPlanner(c.input); got != c.want {
			t.Errorf("TaskWarrantsPlanner(%q) = %v, want %v", c.input, got, c.want)
		}
	}
}
