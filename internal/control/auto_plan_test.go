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
		// Original low-risk questions
		{"what does this function do?", false},
		{"why did the test fail", false},
		{"解释一下这段代码", false},
		{reasoningLanguageBlock("zh") + "\n\nwhat does this function do?", false},
		{reasoningLanguageBlock("en") + "\n\n" + PlanModeMarker + "\n\nfix the bug", false},
		{reasoningLanguageBlock("en") + "\n\nfix the bug", true},
		{"fix the bug", true},
		{"add a login button", true},
		{"implement the new caching layer across the backend", true},
		// New English low-risk prefixes
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
		{"does it work?", false},
		{"did the test pass?", false},
		{"should I use mutex here?", false},
		{"would this approach work?", false},
		{"list all the endpoints", false},
		{"summarize the changes", false},
		{"compare these two approaches", false},
		{"what's the status?", false},
		{"what's 2+2?", false},
		// New Chinese low-risk prefixes
		{"介绍一下这个项目", false},
		{"说一下这个函数的作用", false},
		{"帮我看一下这个报错", false},
		{"是什么意思", false},
		{"有没有现成的方案", false},
		{"能不能这样做", false},
		{"请问这个怎么用", false},
		// Complex intent overrides — these should still plan even with low-risk prefix
		{"how do I implement a new caching layer", true},
		{"what's the best way to refactor this module", true},
		{"explain how to migrate from v1 to v2", true},
		// Work requests still plan
		{"create a new file", true},
		{"update the config", true},
		{"delete this function", true},
	}
	for _, c := range cases {
		if got := TaskWarrantsPlanner(c.input); got != c.want {
			t.Errorf("TaskWarrantsPlanner(%q) = %v, want %v", c.input, got, c.want)
		}
	}
}

// mockClassifier is a test-only AutoPlanClassifier that returns a fixed answer.
type mockClassifier struct {
	needsPlan bool
	err       error
}

func (m *mockClassifier) NeedsPlan(ctx context.Context, input string, score int) (bool, string, error) {
	if m.err != nil {
		return false, "", m.err
	}
	return m.needsPlan, "mock", nil
}

func TestNewPlannerGateNilClassifierFallback(t *testing.T) {
	gate := NewPlannerGate(nil)
	if gate == nil {
		t.Fatal("NewPlannerGate(nil) should not return nil")
	}
	// Should behave exactly like TaskWarrantsPlanner
	if gate("what is this?") != false {
		t.Error("nil classifier should fall back to TaskWarrantsPlanner (low-risk → false)")
	}
	if gate("fix the bug") != true {
		t.Error("nil classifier should fall back to TaskWarrantsPlanner (work → true)")
	}
}

func TestNewPlannerGateWithClassifier(t *testing.T) {
	// Classifier says "no plan needed" → gate should return false for borderline
	gate := NewPlannerGate(&mockClassifier{needsPlan: false})
	// "fix the bug" is a work request (score ≤ 2, borderline) → classifier says no
	if gate("fix the bug") != false {
		t.Error("classifier said no plan, gate should return false")
	}

	// Classifier says "plan needed" → gate should return true
	gatePlan := NewPlannerGate(&mockClassifier{needsPlan: true})
	if gatePlan("fix the bug") != true {
		t.Error("classifier said plan, gate should return true")
	}
}

func TestNewPlannerGateLowRiskSkipsClassifier(t *testing.T) {
	// Low-risk questions should skip the classifier entirely
	called := false
	gate := NewPlannerGate(&mockClassifier{
		needsPlan: true, // would say "plan" if called
		err:       nil,
	})
	_ = called
	// "what is this?" is low-risk → TaskWarrantsPlanner returns false → classifier not called
	if gate("what is this?") != false {
		t.Error("low-risk question should skip classifier and return false")
	}
}
