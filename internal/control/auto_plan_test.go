package control

import (
	"context"
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

func TestNewPlannerGateUsesDeterministicTaskPolicy(t *testing.T) {
	gate := NewPlannerGate()
	if gate == nil {
		t.Fatal("NewPlannerGate returned nil")
	}
	if got := gate(context.Background(), "what is this?"); got {
		t.Error("planner gate should skip low-risk questions")
	}
	if got := gate(context.Background(), "fix the bug"); !got {
		t.Error("planner gate should plan work requests")
	}
}
