package control

import (
	"context"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/runtimepolicy"
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
		{"好的", false},
		{"what does this function do?", false},
		{"解释一下这段代码", false},
		{"fix the bug", false},
		{"复杂重构认证迁移", false},
		{"直接修改 parser.go", false},
		{"先规划这个认证迁移，不要执行", true},
		{"plan first then implement the cache", true},
		{"give me a plan only", true},
		{"fix auth and wait for my approval", true},
	}
	for _, c := range cases {
		if got := TaskWarrantsPlanner(c.input); got != c.want {
			t.Errorf("TaskWarrantsPlanner(%q) = %v, want %v", c.input, got, c.want)
		}
	}
}

func TestNewPlannerGateIsExplicitOnly(t *testing.T) {
	gate := NewPlannerGate()
	if gate == nil {
		t.Fatal("NewPlannerGate returned nil")
	}
	if got := gate(context.Background(), "what is this?"); got {
		t.Error("planner gate should skip questions")
	}
	if got := gate(context.Background(), "fix the bug"); got {
		t.Error("ordinary work must stay executor-only")
	}
	if got := gate(context.Background(), "先规划再执行认证迁移"); !got {
		t.Error("explicit plan-then-execute must call planner")
	}
}

func TestDecidePlannerRouteExplicitOnly(t *testing.T) {
	cases := []struct {
		name   string
		input  string
		meta   plannerTurnMetadata
		route  agent.PlannerRoute
		reason string
	}{
		{
			name:   "explicit plan mode bypasses dual planner",
			input:  "fix the bug",
			meta:   plannerTurnMetadata{ExplicitPlanMode: true},
			route:  agent.PlannerRouteExecutorOnly,
			reason: plannerReasonExplicitPlanMode,
		},
		{
			name:   "trusted synthetic turn bypasses dual planner",
			input:  "perform a brand new implementation",
			meta:   plannerTurnMetadata{Synthetic: true},
			route:  agent.PlannerRouteExecutorOnly,
			reason: plannerReasonSynthetic,
		},
		{
			name:   "user asks for plan only",
			input:  "先规划这个认证迁移，不要执行",
			route:  agent.PlannerRoutePlanOnly,
			reason: plannerReasonUserPlanOnly,
		},
		{
			name:   "user asks for plan then execute",
			input:  "先规划再执行这个缓存改造",
			route:  agent.PlannerRoutePlanAndExecute,
			reason: plannerReasonUserPlanAndExecute,
		},
		{
			name:   "user asks for approval",
			input:  "plan the migration and wait for my approval",
			route:  agent.PlannerRoutePlanForApproval,
			reason: plannerReasonUserPlanApproval,
		},
		{
			name:   "direct request stays executor only",
			input:  "直接修改 parser.go",
			route:  agent.PlannerRouteExecutorOnly,
			reason: plannerReasonUserDirect,
		},
		{
			name:   "ordinary long multi-file request stays executor only",
			input:  "refactor the parser across reader.py and writer.py and add tests",
			route:  agent.PlannerRouteExecutorOnly,
			reason: plannerReasonDefault,
		},
		{
			name:   "auth wording does not auto plan",
			input:  "修复登录超时",
			route:  agent.PlannerRouteExecutorOnly,
			reason: plannerReasonDefault,
		},
		{
			name:   "explicit goal start plans once",
			input:  "fix the crash",
			meta:   plannerTurnMetadata{ExplicitGoalStart: true},
			route:  agent.PlannerRoutePlanAndExecute,
			reason: plannerReasonGoalStart,
		},
		{
			name:   "context dependent fix stays with executor",
			input:  "fix it",
			meta:   plannerTurnMetadata{HasConversationContext: true},
			route:  agent.PlannerRouteExecutorOnly,
			reason: plannerReasonContextContinuation,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := withPlannerTurnMetadata(context.Background(), tc.meta)
			got := DecidePlannerRoute(ctx, tc.input)
			if got.Route != tc.route || got.Reason != tc.reason {
				t.Fatalf("decision = %+v, want route=%s reason=%s", got, tc.route, tc.reason)
			}
		})
	}
}

func TestPlannerPolicyUsesPristineMetadataInsteadOfInjectedContext(t *testing.T) {
	ctx := withPlannerTurnMetadata(context.Background(), plannerTurnMetadata{
		UserText: "fix typo in README",
	})
	input := activeGoalBlock("migrate authentication across the backend") +
		"\n\n<capability-route>\nhigh risk migration\n</capability-route>\n\nfix typo in README"
	got := DecidePlannerRoute(ctx, input)
	if got.Route != agent.PlannerRouteExecutorOnly {
		t.Fatalf("decision used injected context instead of pristine user text: %+v", got)
	}
}

func TestStandardTodoExecutionIntentRejectsNegativeShortReplies(t *testing.T) {
	constraints := runtimepolicy.Constraints{}
	for _, text := range []string{"no", "n"} {
		if standardTodoExecutionExpected(text, false, 2, false, constraints) {
			t.Fatalf("negative reply %q must not arm Standard Todo continuation", text)
		}
	}
	for _, text := range []string{"continue", "开始", "2", "按这个改"} {
		if !standardTodoExecutionExpected(text, false, 2, false, constraints) {
			t.Fatalf("execution reply %q should arm Standard Todo continuation with context", text)
		}
	}
}
