package control

import (
	"context"
	"errors"
	"testing"
)

func TestTaskWarrantsPlanner(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"", false},
		{"   ", false},
		{"/init", false},
		{"1", false},
		{"2.", false},
		{"A", false},
		{"好的", false},
		{"继续", false},
		{"选 1", false},
		{"what does this function do?", false}, // low-risk question → executor only
		{"why did the test fail", false},
		{"解释一下这段代码", false},
		{reasoningLanguageBlock("zh") + "\n\nwhat does this function do?", false},
		{reasoningLanguageBlock("en") + "\n\n" + PlanModeMarker + "\n\nfix the bug", false},
		{reasoningLanguageBlock("en") + "\n\nfix the bug", true},
		{"fix the bug", true},        // terse, but a work request → still planned
		{"add a login button", true}, // ditto
		{"执行修复", true},
		{"开始迁移", true},
		{"继续重构", true},
		{"continue fixing tests", true},
		{"implement the new caching layer across the backend", true},
		{"who wrote this file?", false},
		{"where is the config file?", false},
		{"when does this run?", false},
		{"which file has the error?", false},
		{"explain this code", false},
		{"describe the architecture", false},
		{"tell me about this function", false},
		{"is this safe?", false},
		{"are we done?", false},
		{"can you help?", false},
		{"can you fix the failing tests across the backend", true},
		{"could you update the README", true},
		{"should we remove the stale config option", true},
		{"would you add a regression test", true},
		{"do you fix flaky tests here", true},
		{"does it work?", false},
		{"did the test pass?", false},
		{"should I use mutex here?", false},
		{"would this approach work?", false},
		{"list all the endpoints", false},
		{"summarize the changes", false},
		{"compare these two approaches", false},
		{"what's the status?", false},
		{"介绍一下这个项目", false},
		{"说一下这个函数的作用", false},
		{"帮我看一下这个报错", false},
		{"是什么意思", false},
		{"有没有现成的方案", false},
		{"能不能这样做", false},
		{"请问这个怎么用", false},
		{"how do I implement a new caching layer", true},
		{"what's the best way to refactor this module", true},
		{"explain how to migrate from v1 to v2", true},
		{goalContinueTurn, false},
		{goalSelfCheckTurn, false},
		{"No tool calls in recent turns. Either make progress with tools or signal [goal:blocked:<reason>].", false},
		{"Goal signaled complete but issues remain:\n- the following tasks are still incomplete:\n  - Fix login (in_progress)\nFix or use todo_write/complete_step to mark done, then [goal:complete] again.", false},
		{activeGoalBlock("execute plan: fix the parser", GoalResearchAuto) + "\n\n" + goalContinueTurn, false},
		{activeGoalBlock("execute plan: fix the parser", GoalResearchAuto) + "\n\n" + goalSelfCheckTurn, false},
		{activeGoalBlock("implement the new caching layer", GoalResearchAuto) + "\n\nimplement the new caching layer across the backend", true},
	}
	for _, c := range cases {
		if got := TaskWarrantsPlanner(c.input); got != c.want {
			t.Errorf("TaskWarrantsPlanner(%q) = %v, want %v", c.input, got, c.want)
		}
	}
}

func TestAutoPlanScoreSkipsSyntheticMessages(t *testing.T) {
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
			t.Errorf("autoPlanScore(%q) = %d, want 0", input, got)
		}
	}
}

type mockAutoPlanClassifier struct {
	needsPlan bool
	err       error
}

func (m *mockAutoPlanClassifier) NeedsPlan(context.Context, string, int) (bool, string, error) {
	if m.err != nil {
		return false, "", m.err
	}
	return m.needsPlan, "mock", nil
}

func TestNewPlannerGateNilClassifierFallback(t *testing.T) {
	gate := NewPlannerGate(nil)
	if gate == nil {
		t.Fatal("NewPlannerGate(nil) returned nil")
	}
	if got := gate("what is this?"); got {
		t.Error("nil classifier gate should skip low-risk questions")
	}
	if got := gate("fix the bug"); !got {
		t.Error("nil classifier gate should plan work requests")
	}
}

func TestNewPlannerGateWithClassifier(t *testing.T) {
	gate := NewPlannerGate(&mockAutoPlanClassifier{needsPlan: false})
	if got := gate("fix the bug"); got {
		t.Error("classifier said no plan, gate should return false")
	}

	gate = NewPlannerGate(&mockAutoPlanClassifier{needsPlan: true})
	if got := gate("fix the bug"); !got {
		t.Error("classifier said plan, gate should return true")
	}
}

func TestNewPlannerGateClassifierFailureFallsBackToPlanning(t *testing.T) {
	gate := NewPlannerGate(&mockAutoPlanClassifier{err: errors.New("bad json")})
	if got := gate("fix the bug"); !got {
		t.Error("classifier failure should fall back to planning for work requests")
	}
}
