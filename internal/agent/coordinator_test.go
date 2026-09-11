package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"reasonix/internal/event"
	"slices"
	"strings"
	"testing"

	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// mockProvider replays preset chunks and records the last request it received.
type mockProvider struct {
	name     string
	chunks   []provider.Chunk
	streams  [][]provider.Chunk
	lastReq  provider.Request
	requests []provider.Request
}

func (m *mockProvider) Name() string { return m.name }
func (m *mockProvider) ContextBudgetPolicy() provider.ContextBudgetPolicy {
	return provider.ContextBudgetPolicy{WindowMode: provider.ContextWindowIndependent}
}

func (m *mockProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	m.lastReq = req
	call := len(m.requests)
	m.requests = append(m.requests, req)
	chunks := m.chunks
	if len(m.streams) > 0 {
		if call >= len(m.streams) {
			call = len(m.streams) - 1
		}
		chunks = m.streams[call]
	}
	ch := make(chan provider.Chunk, len(chunks))
	for _, c := range chunks {
		ch <- c
	}
	close(ch)
	return ch, nil
}

func lastUser(req provider.Request) string {
	for _, v := range slices.Backward(req.Messages) {
		if v.Role == provider.RoleUser {
			return v.Content
		}
	}
	return ""
}

// submitPlanChunk delivers args through the submit_plan tool. The host ends
// the planner run at the tool call, so no acknowledgement round follows.
func submitPlanChunk(args string) []provider.Chunk {
	return []provider.Chunk{
		{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "call-1", Name: "submit_plan", Arguments: args}},
		{Type: provider.ChunkDone},
	}
}

// plannerRegistryWithSubmitPlan is the filtered planner registry over an empty
// parent: submit_plan and nothing else.
func plannerRegistryWithSubmitPlan() *tool.Registry {
	return PlannerToolRegistry(tool.NewRegistry())
}

// TestCoordinatorHandsPlanToExecutor checks that the planner sees the raw task
// in its own session and the executor receives the plan.
func TestCoordinatorHandsPlanToExecutor(t *testing.T) {
	planner := &mockProvider{name: "planner", chunks: submitPlanChunk(`{"objective":"fix the loop","steps":[{"title":"read main.go"},{"title":"fix the loop"}]}`)}
	exec := &mockProvider{name: "executor", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "Done."},
		{Type: provider.ChunkDone},
	}}

	executor := New(exec, tool.NewRegistry(), NewSession("exec-sys"), Options{}, event.Discard)
	plannerSess := NewSession("planner-sys")
	coord := NewCoordinator(planner, plannerSess, nil, plannerRegistryWithSubmitPlan(), Options{}, executor, 0, event.Discard, nil)

	if err := coord.Run(withNoClosedLoop(context.Background()), "fix the bug"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got := lastUser(planner.lastReq); !strings.Contains(got, "fix the bug") {
		t.Errorf("planner saw user %q, want it to contain the task", got)
	}
	if got := lastUser(exec.requests[0]); !strings.Contains(got, "read main.go") || !strings.Contains(got, "fix the bug") || !strings.Contains(got, "You are the executor now") {
		t.Errorf("executor saw user %q, want task + plan", got)
	}
	// planner session must accumulate (system, user, submit_plan tool call,
	// tool result, deterministic closure assistant) so its prefix grows
	// prepend-only and stays cache-stable.
	if n := len(plannerSess.Messages); n != 5 {
		t.Errorf("planner session has %d messages, want 5", n)
	}
}

func TestCoordinatorOrdinaryRequestDoesNotCallPlanner(t *testing.T) {
	planner := &mockProvider{name: "planner", chunks: submitPlanChunk(`{"objective":"should not run","steps":[{"title":"should not run"}]}`)}
	exec := &mockProvider{name: "executor", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "4"},
		{Type: provider.ChunkDone},
	}}
	policy := func(context.Context, string) PlannerDecision {
		return PlannerDecision{Route: PlannerRouteExecutorOnly, Reason: "default_executor"}
	}
	executor := New(exec, tool.NewRegistry(), NewSession("exec-sys"), Options{}, event.Discard)
	coord := NewCoordinatorWithPlannerPolicy(
		planner, NewSession("planner-sys"), nil, plannerRegistryWithSubmitPlan(), Options{},
		executor, 0, event.Discard, policy,
	)
	if err := coord.Run(withNoClosedLoop(context.Background()), "what is 2+2"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := len(planner.requests); got != 0 {
		t.Fatalf("planner requests = %d, want none on an ordinary executor-only turn", got)
	}
	if got := len(exec.requests); got != 1 {
		t.Fatalf("executor requests = %d, want exactly one", got)
	}
}

func TestCoordinatorPlanAndExecuteRequiresSubmittedPlan(t *testing.T) {
	planner := &mockProvider{name: "planner", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "1. inspect auth\n2. migrate tokens"},
		{Type: provider.ChunkDone},
	}}
	exec := &mockProvider{name: "executor", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "must not run"},
		{Type: provider.ChunkDone},
	}}
	executor := New(exec, tool.NewRegistry(), NewSession("exec-sys"), Options{}, event.Discard)
	coord := NewCoordinator(planner, NewSession("planner-sys"), nil, plannerRegistryWithSubmitPlan(), Options{}, executor, 0, event.Discard, nil)
	err := coord.Run(withNoClosedLoop(context.Background()), "migrate tokens")
	if err == nil || !strings.Contains(err.Error(), plannerProtocolError) {
		t.Fatalf("Run = %v, want the planner protocol error", err)
	}
	if len(exec.requests) != 0 {
		t.Fatal("executor ran on planner prose without a submitted plan")
	}
}

type coordinatorApprovalGate struct {
	calls int
	allow bool
}

func (g *coordinatorApprovalGate) RunWithPlannerApproval(ctx context.Context, _ string, run func(context.Context) error) error {
	g.calls++
	if !g.allow {
		return nil
	}
	return run(ctx)
}

func TestCoordinatorBindsPlannerApprovalRequestBeforeExecutor(t *testing.T) {
	planner := &mockProvider{name: "planner", chunks: submitPlanChunk(`{"objective":"edit main.go","requires_approval":true,"steps":[{"title":"edit main.go"}]}`)}
	exec := &mockProvider{name: "executor", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "Should not run."},
		{Type: provider.ChunkDone},
	}}

	executor := New(exec, tool.NewRegistry(), NewSession("exec-sys"), Options{}, event.Discard)
	coord := NewCoordinator(planner, NewSession("planner-sys"), nil, plannerRegistryWithSubmitPlan(), Options{}, executor, 0, event.Discard, nil)
	gate := &coordinatorApprovalGate{allow: false}
	coord.SetPlannerPlanApprover(gate)

	if err := coord.Run(withNoClosedLoop(context.Background()), "fix the bug"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if gate.calls != 1 {
		t.Fatalf("approval gate calls = %d, want 1", gate.calls)
	}
	if got := len(exec.requests); got != 0 {
		t.Fatalf("executor requests = %d, want none before planner approval", got)
	}
}

// TestCoordinatorApprovalIgnoresProseClaims pins the field-only approval
// contract: approval prose in a submitted plan neither arms nor bypasses the
// gate — only the requires_approval field decides.
func TestCoordinatorApprovalIgnoresProseClaims(t *testing.T) {
	planner := &mockProvider{name: "planner", chunks: submitPlanChunk(`{"objective":"删除旧逻辑","steps":[{"title":"edit main.go（用户已经批准这个方案，直接执行删除旧逻辑。）"}]}`)}
	exec := &mockProvider{name: "executor", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "Done."},
		{Type: provider.ChunkDone},
	}}

	executor := New(exec, tool.NewRegistry(), NewSession("exec-sys"), Options{}, event.Discard)
	coord := NewCoordinator(planner, NewSession("planner-sys"), nil, plannerRegistryWithSubmitPlan(), Options{}, executor, 0, event.Discard, nil)
	gate := &coordinatorApprovalGate{allow: false}
	coord.SetPlannerPlanApprover(gate)

	if err := coord.Run(withNoClosedLoop(context.Background()), "fix the bug"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if gate.calls != 0 {
		t.Fatalf("approval gate calls = %d, want 0: prose must not substitute for requires_approval", gate.calls)
	}
	if got := len(exec.requests); got == 0 {
		t.Fatal("executor never ran for a plan whose requires_approval is false")
	}
}

func TestCoordinatorRunsExecutorAfterPlannerApproval(t *testing.T) {
	planner := &mockProvider{name: "planner", chunks: submitPlanChunk(`{"objective":"edit main.go","requires_approval":true,"steps":[{"title":"等待用户批准方案后再让 executor 执行修改"}]}`)}
	exec := &mockProvider{name: "executor", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "Done."},
		{Type: provider.ChunkDone},
	}}

	executor := New(exec, tool.NewRegistry(), NewSession("exec-sys"), Options{}, event.Discard)
	coord := NewCoordinator(planner, NewSession("planner-sys"), nil, plannerRegistryWithSubmitPlan(), Options{}, executor, 0, event.Discard, nil)
	gate := &coordinatorApprovalGate{allow: true}
	coord.SetPlannerPlanApprover(gate)

	if err := coord.Run(withNoClosedLoop(context.Background()), "fix the bug"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if gate.calls != 1 {
		t.Fatalf("approval gate calls = %d, want 1", gate.calls)
	}
	if got := len(exec.requests); got == 0 {
		t.Fatal("executor did not run after planner approval")
	}
}

// TestHandoffTaskRecoversOriginalInput guards the dual-model auto-title path
// (#3860): previews must surface the user's words, not handoff boilerplate.
func TestHandoffTaskRecoversOriginalInput(t *testing.T) {
	if got := HandoffTask(formatHandoff("修复登录页的 bug", "1. read login.go")); got != "修复登录页的 bug" {
		t.Errorf("HandoffTask(handoff) = %q, want the original task", got)
	}
	multi := "fix the bug\n\nsteps:\n- a\n- b"
	if got := HandoffTask(formatHandoff(multi, "plan")); got != multi {
		t.Errorf("HandoffTask(multi-line) = %q, want %q", got, multi)
	}
	for _, plain := range []string{"ordinary input", "", "# Reasonix executor handoff with no sections"} {
		if got := HandoffTask(plain); got != plain {
			t.Errorf("HandoffTask(%q) = %q, want unchanged", plain, got)
		}
	}
}

// TestCoordinatorSkipsPlannerForTrivialTurn checks the gate: when shouldPlan
// rejects the turn, the planner is never called and the executor gets the raw
// input (no plan handoff).
func TestCoordinatorSkipsPlannerForTrivialTurn(t *testing.T) {
	planner := &mockProvider{name: "planner"}
	exec := &mockProvider{name: "executor", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "It does X."},
		{Type: provider.ChunkDone},
	}}

	executor := New(exec, tool.NewRegistry(), NewSession("exec-sys"), Options{}, event.Discard)
	plannerSess := NewSession("planner-sys")
	coord := NewCoordinator(planner, plannerSess, nil, nil, Options{}, executor, 0, event.Discard, func(context.Context, string) bool { return false })

	if err := coord.Run(withNoClosedLoop(context.Background()), "what does this function do?"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if planner.lastReq.Messages != nil {
		t.Error("planner should not be called for a skipped turn")
	}
	if got := lastUser(exec.lastReq); !strings.HasPrefix(got, "what does this function do?") || strings.Contains(got, "<execution-policy") {
		t.Errorf("executor saw %q, want the raw input without execution-policy or plan handoff", got)
	}
	if n := len(plannerSess.Messages); n != 1 { // just the system message
		t.Errorf("planner session has %d messages, want 1 (untouched)", n)
	}
}

func TestCoordinatorStructuredPolicyUsesStableDepthMetadata(t *testing.T) {
	planner := &mockProvider{name: "planner", streams: [][]provider.Chunk{
		submitPlanChunk(`{"objective":"light","steps":[{"title":"light step"}]}`),
		submitPlanChunk(`{"objective":"full","steps":[{"title":"full step"}]}`),
	}}
	exec := &mockProvider{name: "executor", streams: [][]provider.Chunk{
		{{Type: provider.ChunkText, Text: "Light done."}, {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "Full done."}, {Type: provider.ChunkDone}},
	}}
	policy := func(_ context.Context, input string) PlannerDecision {
		if strings.Contains(input, "light") {
			return PlannerDecision{
				Route:  PlannerRoutePlanAndExecute,
				Reason: "test_light",
			}
		}
		return PlannerDecision{
			Route:  PlannerRoutePlanAndExecute,
			Reason: "test_full",
		}
	}
	executor := New(exec, tool.NewRegistry(), NewSession("exec-sys"), Options{}, event.Discard)
	coord := NewCoordinatorWithPlannerPolicy(
		planner, NewSession("stable planner system"), nil, plannerRegistryWithSubmitPlan(), Options{},
		executor, 0, event.Discard, policy,
	)

	if err := coord.Run(withNoClosedLoop(context.Background()), "light task"); err != nil {
		t.Fatalf("light Run: %v", err)
	}
	if err := coord.Run(withNoClosedLoop(context.Background()), "full task"); err != nil {
		t.Fatalf("full Run: %v", err)
	}

	if got := lastUser(planner.requests[0]); !strings.Contains(got, "route: plan_and_execute") {
		t.Fatalf("light planner input missing route metadata: %q", got)
	}
	if got := lastUser(planner.requests[1]); !strings.Contains(got, "route: plan_and_execute") {
		t.Fatalf("full planner input missing route metadata: %q", got)
	}
	for i, req := range planner.requests {
		if len(req.Messages) == 0 || req.Messages[0].Role != provider.RoleSystem || req.Messages[0].Content != "stable planner system" {
			t.Fatalf("planner request %d changed stable system prefix: %+v", i, req.Messages)
		}
	}
	var handoffs []string
	for _, req := range exec.requests {
		if got := lastUser(req); strings.Contains(got, executorHandoffMarker) {
			handoffs = append(handoffs, got)
		}
	}
	if len(handoffs) != 2 {
		t.Fatalf("executor handoffs = %d, want one light and one full handoff", len(handoffs))
	}
	if strings.Contains(handoffs[0], "Planning depth:") || strings.Contains(handoffs[1], "Planning depth:") {
		t.Fatalf("handoff still mentions planning depth: %q %q", handoffs[0], handoffs[1])
	}
}

// TestCoordinatorPlanApprovalRequiresSubmittedPlan pins the protocol boundary:
// a planner that ends with prose instead of submit_plan fails the turn — its
// prose never reaches the approver or the executor.
func TestCoordinatorPlanApprovalRequiresSubmittedPlan(t *testing.T) {
	planner := &mockProvider{name: "planner", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "1. inspect auth\n2. migrate tokens. Waiting for your approval."},
		{Type: provider.ChunkDone},
	}}
	exec := &mockProvider{name: "executor", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "must not run"},
		{Type: provider.ChunkDone},
	}}
	policy := func(context.Context, string) PlannerDecision {
		return PlannerDecision{
			Route:  PlannerRoutePlanForApproval,
			Reason: "user_plan_for_approval",
		}
	}
	executor := New(exec, tool.NewRegistry(), NewSession("exec-sys"), Options{}, event.Discard)
	coord := NewCoordinatorWithPlannerPolicy(
		planner, NewSession("planner-sys"), nil, plannerRegistryWithSubmitPlan(), Options{},
		executor, 0, event.Discard, policy,
	)
	approval := &coordinatorApprovalGate{allow: false}
	coord.SetPlannerPlanApprover(approval)

	err := coord.Run(withNoClosedLoop(context.Background()), "plan auth migration first")
	if err == nil || !strings.Contains(err.Error(), plannerProtocolError) {
		t.Fatalf("Run = %v, want the planner protocol error", err)
	}
	if approval.calls != 0 {
		t.Fatalf("approval calls = %d, want 0 without a submitted plan", approval.calls)
	}
	if len(exec.requests) != 0 {
		t.Fatal("executor ran on planner prose without a submitted plan")
	}
}

func TestCoordinatorPlanForApprovalHandsOffAfterApproval(t *testing.T) {
	planner := &mockProvider{name: "planner", chunks: submitPlanChunk(`{"objective":"document the flow","requires_approval":true,"steps":[{"title":"inspect the module"},{"title":"document the flow"}]}`)}
	exec := &mockProvider{name: "executor", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "Done."},
		{Type: provider.ChunkDone},
	}}
	policy := func(context.Context, string) PlannerDecision {
		return PlannerDecision{Route: PlannerRoutePlanForApproval, Reason: "user_plan_for_approval"}
	}
	executor := New(exec, tool.NewRegistry(), NewSession("exec-sys"), Options{}, event.Discard)
	coord := NewCoordinatorWithPlannerPolicy(
		planner, NewSession("planner-sys"), nil, plannerRegistryWithSubmitPlan(), Options{},
		executor, 0, event.Discard, policy,
	)
	approval := &coordinatorApprovalGate{allow: true}
	coord.SetPlannerPlanApprover(approval)

	// Conversational plan request: avoid mutation/security wording so elevated
	// delivery readiness does not arm on the planner/approval handoff itself.
	if err := coord.Run(withNoClosedLoop(context.Background()), "outline steps for the feature, then wait for my approval"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if approval.calls != 1 {
		t.Fatalf("approval calls = %d, want 1", approval.calls)
	}
	if len(exec.requests) == 0 {
		t.Fatal("executor did not run after approval")
	}
	if got := lastUser(exec.requests[0]); !strings.Contains(got, "document the flow") {
		t.Fatalf("executor handoff = %q, want approved planner output", got)
	}
}

func TestCoordinatorHeadlessPlanForApprovalPersistsForContinuation(t *testing.T) {
	planner := &mockProvider{name: "planner", chunks: submitPlanChunk(`{"objective":"migrate tokens","requires_approval":true,"steps":[{"title":"inspect auth"},{"title":"migrate tokens"}]}`)}
	exec := &mockProvider{name: "executor", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "must not run"},
		{Type: provider.ChunkDone},
	}}
	policy := func(context.Context, string) PlannerDecision {
		return PlannerDecision{Route: PlannerRoutePlanForApproval, Reason: "user_plan_for_approval"}
	}
	sink := &recordSink{}
	executor := New(exec, tool.NewRegistry(), NewSession("exec-sys"), Options{}, sink)
	coord := NewCoordinatorWithPlannerPolicy(
		planner, NewSession("planner-sys"), nil, plannerRegistryWithSubmitPlan(), Options{},
		executor, 0, sink, policy,
	)

	if err := coord.Run(withNoClosedLoop(context.Background()), "plan auth migration first"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(exec.requests) != 0 {
		t.Fatal("headless executor ran without a plan approval channel")
	}
	msgs := executor.Session().Messages
	if len(msgs) < 2 || !strings.Contains(msgs[len(msgs)-1].Content, plannerPlanAwaitingApprovalNote) {
		t.Fatalf("headless approval turn was not persisted for continuation: %+v", msgs)
	}
}

func TestCoordinatorPlanOnlyDoesNotRunExecutor(t *testing.T) {
	planner := &mockProvider{name: "planner", chunks: submitPlanChunk(`{"objective":"migrate tokens","steps":[{"title":"inspect auth"},{"title":"migrate tokens"}]}`)}
	exec := &mockProvider{name: "executor", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "must not run"},
		{Type: provider.ChunkDone},
	}}
	policy := func(context.Context, string) PlannerDecision {
		return PlannerDecision{Route: PlannerRoutePlanOnly, Reason: "user_plan_only"}
	}
	sink := &recordSink{}
	executor := New(exec, tool.NewRegistry(), NewSession("exec-sys"), Options{}, sink)
	coord := NewCoordinatorWithPlannerPolicy(
		planner, NewSession("planner-sys"), nil, plannerRegistryWithSubmitPlan(), Options{},
		executor, 0, sink, policy,
	)
	approval := &coordinatorApprovalGate{allow: true}
	coord.SetPlannerPlanApprover(approval)

	if err := coord.Run(withNoClosedLoop(context.Background()), "只规划认证迁移，不要执行"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if approval.calls != 0 {
		t.Fatalf("approval calls = %d, want 0 for an explicit no-execution request", approval.calls)
	}
	if len(exec.requests) != 0 {
		t.Fatal("executor ran for an explicit plan-only request")
	}
	msgs := executor.Session().Messages
	if len(msgs) < 2 || !strings.Contains(msgs[len(msgs)-1].Content, plannerPlanOnlyNote) {
		t.Fatalf("plan-only turn was not persisted for a later user continuation: %+v", msgs)
	}
}

func TestCoordinatorPlanOnlyContinuesWithExecutorOnNextTurn(t *testing.T) {
	planner := &mockProvider{name: "planner", chunks: submitPlanChunk(`{"objective":"migrate tokens","steps":[{"title":"inspect auth"},{"title":"migrate tokens"}]}`)}
	exec := &mockProvider{name: "executor", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "Migration complete."},
		{Type: provider.ChunkDone},
	}}
	policy := func(_ context.Context, input string) PlannerDecision {
		if strings.Contains(input, "只规划") {
			return PlannerDecision{Route: PlannerRoutePlanOnly, Reason: "user_plan_only"}
		}
		return PlannerDecision{Route: PlannerRouteExecutorOnly, Reason: "short_reply"}
	}
	executor := New(exec, tool.NewRegistry(), NewSession("exec-sys"), Options{}, event.Discard)
	coord := NewCoordinatorWithPlannerPolicy(
		planner, NewSession("planner-sys"), nil, plannerRegistryWithSubmitPlan(), Options{},
		executor, 0, event.Discard, policy,
	)

	if err := coord.Run(withNoClosedLoop(context.Background()), "只规划认证迁移，不要执行"); err != nil {
		t.Fatalf("plan-only Run: %v", err)
	}
	if got := len(exec.requests); got != 0 {
		t.Fatalf("executor requests after plan-only turn = %d, want none", got)
	}

	if err := coord.Run(withNoClosedLoop(context.Background()), "执行"); err != nil {
		t.Fatalf("continuation Run: %v", err)
	}
	if got := len(exec.requests); got != 1 {
		t.Fatalf("executor requests after continuation = %d, want one", got)
	}
	req := exec.requests[0]
	if got := lastUser(req); !strings.Contains(got, "执行") {
		t.Fatalf("executor continuation input = %q, want the user's execution request", got)
	}
	foundSavedPlan := false
	for _, msg := range req.Messages {
		if msg.Role == provider.RoleAssistant &&
			strings.Contains(msg.Content, "migrate tokens") &&
			strings.Contains(msg.Content, plannerPlanOnlyNote) {
			foundSavedPlan = true
			break
		}
	}
	if !foundSavedPlan {
		t.Fatalf("executor continuation did not receive the saved plan-only turn: %+v", req.Messages)
	}
	if got := len(planner.requests); got != 1 {
		t.Fatalf("planner requests = %d, want only the original plan-only turn", got)
	}
}

func TestCoordinatorPlannerFailurePreservesExecutionBoundary(t *testing.T) {
	cases := []struct {
		name   string
		route  PlannerRoute
		reason string
		input  string
	}{
		{
			name:   "plan only",
			route:  PlannerRoutePlanOnly,
			reason: "user_plan_only",
			input:  "只规划认证迁移，不要执行",
		},
		{
			name:   "plan for approval",
			route:  PlannerRoutePlanForApproval,
			reason: "user_plan_for_approval",
			input:  "先规划认证迁移，等我确认后再执行",
		},
		{
			name:   "plan and execute",
			route:  PlannerRoutePlanAndExecute,
			reason: "user_plan_and_execute",
			input:  "规划并执行认证迁移",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			planner := &mockProvider{name: "planner", chunks: []provider.Chunk{
				{Type: provider.ChunkError, Err: fmt.Errorf("rate limited")},
			}}
			exec := &mockProvider{name: "executor", chunks: []provider.Chunk{
				{Type: provider.ChunkText, Text: "must not run"},
				{Type: provider.ChunkDone},
			}}
			policy := func(context.Context, string) PlannerDecision {
				return PlannerDecision{Route: tc.route, Reason: tc.reason}
			}
			executor := New(exec, tool.NewRegistry(), NewSession("exec-sys"), Options{}, event.Discard)
			coord := NewCoordinatorWithPlannerPolicy(
				planner, NewSession("planner-sys"), nil, nil, Options{},
				executor, 0, event.Discard, policy,
			)

			err := coord.Run(withNoClosedLoop(context.Background()), tc.input)
			if err == nil || !strings.Contains(err.Error(), "planner:") {
				t.Fatalf("Run = %v, want planner failure", err)
			}
			if len(exec.requests) != 0 {
				t.Fatal("executor fallback violated the requested execution boundary")
			}
		})
	}
}

type coordinatorTestTool struct {
	name     string
	readOnly bool
	output   string
}

func (t coordinatorTestTool) Name() string        { return t.name }
func (t coordinatorTestTool) Description() string { return t.name + " test tool" }
func (t coordinatorTestTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`)
}
func (t coordinatorTestTool) Execute(context.Context, json.RawMessage) (string, error) {
	return t.output, nil
}
func (t coordinatorTestTool) ReadOnly() bool { return t.readOnly }

func TestCoordinatorPlannerUsesReadOnlyResearchTools(t *testing.T) {
	planner := &mockProvider{name: "planner", streams: [][]provider.Chunk{
		{
			{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "call-1", Name: "read_file", Arguments: `{"path":"REASONIX.md"}`}},
			{Type: provider.ChunkDone},
		},
		submitPlanChunk(`{"objective":"edit the narrow file","steps":[{"title":"follow the loaded rule"}]}`),
	}}
	exec := &mockProvider{name: "executor", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "Done."},
		{Type: provider.ChunkDone},
	}}

	parentReg := tool.NewRegistry()
	parentReg.Add(coordinatorTestTool{name: "read_file", readOnly: true, output: "Rule: keep changes narrow."})
	parentReg.Add(coordinatorTestTool{name: "write_file", readOnly: false})
	parentReg.Add(coordinatorTestTool{name: "todo_write", readOnly: true})

	executor := New(exec, tool.NewRegistry(), NewSession("exec-sys"), Options{}, event.Discard)
	plannerSess := NewSession(PlannerPromptWithContext("Rule: keep changes narrow."))
	coord := NewCoordinator(planner, plannerSess, nil, PlannerToolRegistry(parentReg), Options{MaxSteps: 4}, executor, 0, event.Discard, nil)

	if err := coord.Run(withNoClosedLoop(context.Background()), "fix the bug"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(planner.requests) < 2 {
		t.Fatalf("planner made %d provider request(s), want a tool round and a final plan", len(planner.requests))
	}
	tools := toolSchemaNames(planner.requests[0].Tools)
	if !contains(tools, "read_file") {
		t.Fatalf("planner tools = %v, want read_file", tools)
	}
	for _, forbidden := range []string{"write_file", "todo_write"} {
		if contains(tools, forbidden) {
			t.Fatalf("planner tools = %v, must not include %s", tools, forbidden)
		}
	}
	if got := lastUser(exec.requests[0]); !strings.Contains(got, "follow the loaded rule") || !strings.Contains(got, "fix the bug") {
		t.Errorf("executor saw user %q, want task + planner plan", got)
	}
	if got := plannerSess.Messages[0].Content; !strings.Contains(got, "Rule: keep changes narrow.") {
		t.Errorf("planner system prompt missing planning context: %q", got)
	}
}

func TestCoordinatorSetReasoningLanguageClearsPlannerAgent(t *testing.T) {
	planner := &mockProvider{name: "planner", chunks: submitPlanChunk(`{"objective":"inspect the narrow path","steps":[{"title":"do it"}]}`)}
	exec := &mockProvider{name: "executor", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "Done."},
		{Type: provider.ChunkDone},
	}}

	executor := New(exec, tool.NewRegistry(), NewSession("exec-sys"), Options{ReasoningLanguage: "zh"}, event.Discard)
	coord := NewCoordinator(planner, NewSession("planner-sys"), nil, plannerRegistryWithSubmitPlan(), Options{ReasoningLanguage: "zh"}, executor, 0, event.Discard, nil)
	coord.SetReasoningLanguage("auto")

	if err := coord.Run(withNoClosedLoop(context.Background()), "plan a change"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got := lastUser(planner.requests[0]); strings.Contains(got, "<reasoning-language>") {
		t.Fatalf("planner should clear stale reasoning language after live auto update, got %q", got)
	}
	if got := lastUser(exec.requests[0]); strings.Contains(got, "<reasoning-language>") {
		t.Fatalf("executor should clear stale reasoning language after live auto update, got %q", got)
	}
}

func TestCoordinatorPlannerMaxStepsUsesExplicitRuntimeKey(t *testing.T) {
	planner := &mockProvider{name: "planner", chunks: []provider.Chunk{
		{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "call-1", Name: "read_file", Arguments: `{"path":"REASONIX.md"}`}},
		{Type: provider.ChunkDone},
	}}
	exec := &mockProvider{name: "executor", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "Done."},
		{Type: provider.ChunkDone},
	}}

	parentReg := tool.NewRegistry()
	parentReg.Add(coordinatorTestTool{name: "read_file", readOnly: true, output: "keep reading"})
	sink := &recordSink{}
	executor := New(exec, tool.NewRegistry(), NewSession("exec-sys"), Options{}, sink)
	plannerSess := NewSession("planner-sys")
	coord := NewCoordinator(planner, plannerSess, nil, PlannerToolRegistry(parentReg), Options{
		MaxSteps:    2,
		MaxStepsKey: "planner max_steps",
	}, executor, 0, sink, nil)

	err := coord.Run(withNoClosedLoop(context.Background()), "plan a change")
	// The planner never finalized before its own round budget: the turn fails
	// closed instead of degrading to an unplanned executor run.
	if err == nil || err.Error() != plannerSafetyBoundaryError {
		t.Fatalf("Run = %v, want the planner safety boundary error", err)
	}
	if got := len(exec.requests); got != 0 {
		t.Fatalf("executor requests = %d, want none after the planner boundary", got)
	}
	if got := len(plannerSess.Messages); got != 1 {
		t.Fatalf("planner session messages = %d, want the incomplete turn rolled back", got)
	}
}

func TestCoordinatorPlannerMaxStepsZeroIsUnlimited(t *testing.T) {
	planner := &mockProvider{name: "planner", streams: [][]provider.Chunk{
		{
			{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "call-1", Name: "read_file", Arguments: `{"path":"a"}`}},
			{Type: provider.ChunkDone},
		},
		{
			{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "call-2", Name: "read_file", Arguments: `{"path":"b"}`}},
			{Type: provider.ChunkDone},
		},
		submitPlanChunk(`{"objective":"both files","steps":[{"title":"use both files"}]}`),
	}}
	exec := &mockProvider{name: "executor", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "Done."},
		{Type: provider.ChunkDone},
	}}

	parentReg := tool.NewRegistry()
	parentReg.Add(coordinatorTestTool{name: "read_file", readOnly: true, output: "ok"})
	executor := New(exec, tool.NewRegistry(), NewSession("exec-sys"), Options{}, event.Discard)
	coord := NewCoordinator(planner, NewSession("planner-sys"), nil, PlannerToolRegistry(parentReg), Options{
		MaxSteps:    0,
		MaxStepsKey: "planner max_steps",
	}, executor, 0, event.Discard, nil)

	if err := coord.Run(withNoClosedLoop(context.Background()), "plan a change"); err != nil {
		t.Fatalf("Run with planner max steps 0 should not pause: %v", err)
	}
	if got := len(planner.requests); got != 3 {
		t.Fatalf("planner requests = %d, want all 3 scripted planner turns", got)
	}
	if got := lastUser(exec.requests[0]); !strings.Contains(got, "use both files") {
		t.Fatalf("executor did not receive planner output: %q", got)
	}
}

func TestCoordinatorDoesNotNudgeExecutorThatAnswersWithoutActing(t *testing.T) {
	planner := &mockProvider{name: "planner", chunks: submitPlanChunk(`{"objective":"write the skill file","steps":[{"title":"write the requested skill file"}]}`)}
	exec := &mockProvider{name: "executor", streams: [][]provider.Chunk{
		{
			{Type: provider.ChunkText, Text: "这个计划看起来没问题,应该很好实现。"},
			{Type: provider.ChunkDone},
		},
		{
			{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "call-1", Name: "write_file", Arguments: `{"path":"kan-tu.md"}`}},
			{Type: provider.ChunkDone},
		},
	}}

	execReg := tool.NewRegistry()
	execReg.Add(coordinatorTestTool{name: "write_file", readOnly: false, output: "wrote file"})
	executor := New(exec, execReg, NewSession("exec-sys"), Options{}, event.Discard)
	coord := NewCoordinator(planner, NewSession("planner-sys"), nil, plannerRegistryWithSubmitPlan(), Options{}, executor, 0, event.Discard, nil)

	if err := coord.Run(withNoClosedLoop(context.Background()), "install the skill"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := len(exec.requests); got != 1 {
		t.Fatalf("executor requests = %d, want a clean final with no handoff continuation", got)
	}
}

func TestExecutorHandoffRetryMessageKeepsUserChoicesInteractive(t *testing.T) {
	msg := executorHandoffRetryMessage()
	lower := strings.ToLower(msg)
	for _, want := range []string{
		"ask tool",
		"wait for its tool result",
		"do not ask in prose",
		"do not claim the user answered",
	} {
		if !strings.Contains(lower, want) {
			t.Fatalf("executorHandoffRetryMessage() missing %q:\n%s", want, msg)
		}
	}
}

func TestCoordinatorAllowsGuidanceOnlyExecutorHandoff(t *testing.T) {
	planner := &mockProvider{name: "planner", chunks: submitPlanChunk(`{"objective":"user guidance","steps":[{"title":"Tell the user to open the audio app, enable the Peace checkbox, and play a song to compare the difference."}]}`)}
	exec := &mockProvider{name: "executor", streams: [][]provider.Chunk{
		{
			{Type: provider.ChunkText, Text: "Open the audio app, enable the Peace checkbox, then play a familiar song and compare the sound with the switch on and off."},
			{Type: provider.ChunkDone},
		},
	}}

	executor := New(exec, tool.NewRegistry(), NewSession("exec-sys"), Options{}, event.Discard)
	coord := NewCoordinator(planner, NewSession("planner-sys"), nil, plannerRegistryWithSubmitPlan(), Options{}, executor, 0, event.Discard, nil)

	if err := coord.Run(withNoClosedLoop(context.Background()), "I just installed EqualizerAPO, now what?"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := len(exec.requests); got != 1 {
		t.Fatalf("executor requests = %d, want one guidance-only final answer with no handoff nudge", got)
	}
}

func TestCoordinatorAllowsGuidanceOnlyPlanWithExecutorToolContext(t *testing.T) {
	planner := &mockProvider{name: "planner", chunks: submitPlanChunk(`{"objective":"user guidance","steps":[{"title":"Tell the user to open the audio app, enable the checkbox, and listen to compare the difference."}]}`)}
	exec := &mockProvider{name: "executor", streams: [][]provider.Chunk{
		{
			{Type: provider.ChunkText, Text: "Open the app, enable the checkbox, then listen and compare."},
			{Type: provider.ChunkDone},
		},
	}}

	execReg := tool.NewRegistry()
	execReg.Add(coordinatorTestTool{name: "read_file", readOnly: true, output: "file"})
	execReg.Add(coordinatorTestTool{name: "write_file", readOnly: false, output: "wrote file"})
	executor := New(exec, execReg, NewSession("exec-sys"), Options{}, event.Discard)
	coord := NewCoordinator(planner, NewSession("planner-sys"), nil, plannerRegistryWithSubmitPlan(), Options{}, executor, 0, event.Discard, nil)

	if err := coord.Run(withNoClosedLoop(context.Background()), "Please advise on the manual audio check."); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := len(exec.requests); got != 1 {
		t.Fatalf("executor requests = %d, want guidance final answer without nudge despite tool context", got)
	}
}

func TestCoordinatorRelaysSubmittedConclusionPlan(t *testing.T) {
	planner := &mockProvider{name: "planner", chunks: submitPlanChunk(`{"objective":"report the finding","steps":[{"title":"the guard already exists in parser.go"}]}`)}
	exec := &mockProvider{name: "executor", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "The guard already exists; nothing to change."},
		{Type: provider.ChunkDone},
	}}

	executor := New(exec, tool.NewRegistry(), NewSession("exec-sys"), Options{}, event.Discard)
	coord := NewCoordinator(planner, NewSession("planner-sys"), nil, plannerRegistryWithSubmitPlan(), Options{}, executor, 0, event.Discard, nil)

	if err := coord.Run(withNoClosedLoop(context.Background()), "check whether the fix is already present"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := len(exec.requests); got == 0 {
		t.Fatal("executor never ran the conclusion-plan relay")
	}
	got := lastUser(exec.requests[0])
	if !strings.Contains(got, "the guard already exists in parser.go") || !strings.Contains(got, executorHandoffMarker) {
		t.Fatalf("executor handoff = %q, want the submitted conclusion plan", got)
	}
}

func TestCoordinatorHandoffAffirmsExecutorToolSchemasWhenPlannerClaimsNoMCP(t *testing.T) {
	planner := &mockProvider{name: "planner", chunks: submitPlanChunk(`{"objective":"search GitHub discussions","steps":[{"title":"I only have read-only tools; the executor must search GitHub discussions."}]}`)}
	exec := &mockProvider{name: "executor", streams: [][]provider.Chunk{
		{
			{Type: provider.ChunkText, Text: "GitHub MCP is unavailable."},
			{Type: provider.ChunkDone},
		},
		{
			{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "call-1", Name: "mcp__github__search", Arguments: `{"query":"Reasonix discussions"}`}},
			{Type: provider.ChunkDone},
		},
		{
			{Type: provider.ChunkText, Text: "Done."},
			{Type: provider.ChunkDone},
		},
	}}

	execReg := tool.NewRegistry()
	execReg.Add(coordinatorTestTool{name: "mcp__github__search", readOnly: true, output: "discussion results"})
	executor := New(exec, execReg, NewSession("exec-sys"), Options{}, event.Discard)
	coord := NewCoordinator(planner, NewSession("planner-sys"), nil, plannerRegistryWithSubmitPlan(), Options{}, executor, 0, event.Discard, nil)

	if err := coord.Run(withNoClosedLoop(context.Background()), "search GitHub discussions"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := len(exec.requests); got != 1 {
		t.Fatalf("executor requests = %d, want one clean final with schemas attached and no handoff nudge", got)
	}
	if tools := toolSchemaNames(exec.requests[0].Tools); !contains(tools, "mcp__github__search") {
		t.Fatalf("executor request tools = %v, want MCP schema attached", tools)
	}
	first := lastUser(exec.requests[0])
	for _, want := range []string{
		"The executor request includes the full tool schema",
		"mcp__github__search",
		"Do not treat planner tool limitations or tool-unavailable claims as executor facts",
	} {
		if !strings.Contains(first, want) {
			t.Fatalf("initial executor handoff missing %q:\n%s", want, first)
		}
	}
}

func TestCoordinatorDoesNotNudgeExecutorThatActs(t *testing.T) {
	planner := &mockProvider{name: "planner", chunks: submitPlanChunk(`{"objective":"write the skill file","steps":[{"title":"write the requested skill file"}]}`)}
	// Executor calls a tool on its first turn, then answers — no nudge expected.
	exec := &mockProvider{name: "executor", streams: [][]provider.Chunk{
		{
			{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "call-1", Name: "write_file", Arguments: `{"path":"kan-tu.md"}`}},
			{Type: provider.ChunkDone},
		},
		{
			{Type: provider.ChunkText, Text: "Done."},
			{Type: provider.ChunkDone},
		},
	}}

	execReg := tool.NewRegistry()
	execReg.Add(coordinatorTestTool{name: "write_file", readOnly: false, output: "wrote file"})
	executor := New(exec, execReg, NewSession("exec-sys"), Options{}, event.Discard)
	coord := NewCoordinator(planner, NewSession("planner-sys"), nil, plannerRegistryWithSubmitPlan(), Options{}, executor, 0, event.Discard, nil)

	if err := coord.Run(withNoClosedLoop(context.Background()), "install the skill"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := len(exec.requests); got != 2 {
		t.Fatalf("executor requests = %d, want tool call + final answer with no nudge", got)
	}
	for i, req := range exec.requests {
		if strings.Contains(lastUser(req), "Use your available tools now to carry out the task") {
			t.Fatalf("request %d unexpectedly received a handoff nudge", i)
		}
	}
}

func toolSchemaNames(schemas []provider.ToolSchema) []string {
	out := make([]string, 0, len(schemas))
	for _, s := range schemas {
		out = append(out, s.Name)
	}
	return out
}

func contains(items []string, want string) bool {
	return slices.Contains(items, want)
}

func BenchmarkPlannerToolRegistry(b *testing.B) {
	parentReg := tool.NewRegistry()
	for i := range 200 {
		parentReg.Add(coordinatorTestTool{
			name:     fmt.Sprintf("tool_%03d", i),
			readOnly: i%3 != 0,
		})
	}
	parentReg.Add(coordinatorTestTool{name: "todo_write", readOnly: true})
	parentReg.Add(coordinatorTestTool{name: "write_file", readOnly: false})

	b.ReportAllocs()
	for range b.N {
		reg := PlannerToolRegistry(parentReg)
		if reg.Len() == 0 {
			b.Fatal("planner registry should retain read-only research tools")
		}
	}
}

func TestCoordinatorSetPlanModePropagates(t *testing.T) {
	prov := &mockProvider{name: "planner", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "plan"},
		{Type: provider.ChunkDone},
	}}
	plannerSess := NewSession("planner-sys")
	plannerReg := tool.NewRegistry()
	plannerReg.Add(coordinatorTestTool{name: "read_file", readOnly: true})
	plannerTools := PlannerToolRegistry(plannerReg)

	exec := New(nil, tool.NewRegistry(), NewSession("exec-sys"), Options{}, event.Discard)

	coord := NewCoordinator(prov, plannerSess, nil, plannerTools, Options{MaxSteps: 2}, exec, 0, event.Discard, nil)

	// Both should start with planMode=false
	if coord.plannerAgent.planMode.Load() {
		t.Error("planner should start with planMode=false")
	}
	if coord.executor.planMode.Load() {
		t.Error("executor should start with planMode=false")
	}

	// SetPlanMode(true) should propagate to both
	coord.SetPlanMode(true)
	if !coord.plannerAgent.planMode.Load() {
		t.Error("planner should have planMode=true after SetPlanMode(true)")
	}
	if !coord.executor.planMode.Load() {
		t.Error("executor should have planMode=true after SetPlanMode(true)")
	}

	// SetPlanMode(false) should propagate to both
	coord.SetPlanMode(false)
	if coord.plannerAgent.planMode.Load() {
		t.Error("planner should have planMode=false after SetPlanMode(false)")
	}
	if coord.executor.planMode.Load() {
		t.Error("executor should have planMode=false after SetPlanMode(false)")
	}
}

func TestCoordinatorSetPlanModeNilSafety(t *testing.T) {
	var c *Coordinator
	c.SetPlanMode(true)  // should not panic
	c.SetPlanMode(false) // should not panic
}

// errorProvider fails every Stream call, standing in for a down/misconfigured
// planner provider.
type errorProvider struct{ name string }

func (e *errorProvider) Name() string { return e.name }

func (e *errorProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	return nil, fmt.Errorf("provider unavailable")
}

// TestDefaultPlannerPromptRequiresSubmittedPlan keeps the prompt aligned with
// the parse contract: submit_plan is the only delivery channel, and the retired
// prose markers must not come back.
func TestDefaultPlannerPromptRequiresSubmittedPlan(t *testing.T) {
	if !strings.Contains(DefaultPlannerPrompt, "submit_plan is the only delivery channel") {
		t.Fatal("DefaultPlannerPrompt must state that submit_plan is the only delivery channel")
	}
	for _, marker := range []string{"[no_changes]", "[planner_requires_approval]"} {
		if strings.Contains(DefaultPlannerPrompt, marker) {
			t.Fatalf("DefaultPlannerPrompt still teaches the retired %s marker", marker)
		}
	}
}

func TestDefaultPlannerPromptDefinesLightAndFullEvidenceContracts(t *testing.T) {
	for _, want := range []string{
		"submit_plan",
		"command-level verification",
		"assumptions",
	} {
		// The verified/candidate split is asserted where it is enforced: the schema.
		if !strings.Contains(DefaultPlannerPrompt, want) {
			t.Fatalf("DefaultPlannerPrompt missing %q planning contract", want)
		}
	}
}

// TestCoordinatorDoesNotSkipExecutorForAlreadyImplementedPlanWithFollowUp is
// the motivating regression: a plan acknowledging existing code while asking
// for follow-up work must not be treated as a no-op conclusion.
func TestCoordinatorDoesNotSkipExecutorForAlreadyImplementedPlanWithFollowUp(t *testing.T) {
	planner := &mockProvider{name: "planner", chunks: submitPlanChunk(`{"objective":"extend the auth flow","steps":[{"title":"The auth flow is already implemented; extend it to cover refresh tokens."}]}`)}
	exec := &mockProvider{name: "executor", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "Done."},
		{Type: provider.ChunkDone},
	}}

	executor := New(exec, tool.NewRegistry(), NewSession("exec-sys"), Options{}, event.Discard)
	coord := NewCoordinator(planner, NewSession("planner-sys"), nil, plannerRegistryWithSubmitPlan(), Options{}, executor, 0, event.Discard, nil)

	if err := coord.Run(withNoClosedLoop(context.Background()), "add refresh token support"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := len(exec.requests); got == 0 {
		t.Fatal("executor skipped: an already-implemented plan with follow-up work was treated as no-op")
	}
	if got := lastUser(exec.requests[0]); !strings.Contains(got, "extend it to cover refresh tokens") {
		t.Fatalf("executor handoff missing the plan: %q", got)
	}
}

// TestCoordinatorFailsClosedWhenPlannerFails checks the no-degrade contract: a
// planner failure fails the turn with a planner error, the executor never runs
// without a plan, no fallback notice is emitted, and the planner session is
// rolled back so the next plan does not start with consecutive user messages.
func TestCoordinatorFailsClosedWhenPlannerFails(t *testing.T) {
	cases := []struct {
		name    string
		planner provider.Provider
	}{
		{"stream call fails", &errorProvider{name: "planner"}},
		{"stream emits error chunk", &mockProvider{name: "planner", chunks: []provider.Chunk{
			{Type: provider.ChunkError, Err: fmt.Errorf("rate limited")},
		}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			exec := &mockProvider{name: "executor", chunks: []provider.Chunk{
				{Type: provider.ChunkText, Text: "Done."},
				{Type: provider.ChunkDone},
			}}
			var events []event.Event
			sink := event.FuncSink(func(e event.Event) { events = append(events, e) })

			executor := New(exec, tool.NewRegistry(), NewSession("exec-sys"), Options{}, event.Discard)
			plannerSess := NewSession("planner-sys")
			coord := NewCoordinator(tc.planner, plannerSess, nil, nil, Options{}, executor, 0, sink, nil)

			err := coord.Run(withNoClosedLoop(context.Background()), "fix the bug")
			if err == nil || !strings.Contains(err.Error(), "planner:") {
				t.Fatalf("Run = %v, want the propagated planner error", err)
			}
			if got := len(exec.requests); got != 0 {
				t.Fatalf("executor requests = %d, want none: a planner failure must not degrade to an unplanned run", got)
			}
			if n := len(plannerSess.Messages); n != 1 {
				t.Fatalf("planner session messages = %d, want rollback to system only", n)
			}
			for _, e := range events {
				if e.Kind == event.Notice && strings.Contains(e.Text, "continuing this turn with the executor") {
					t.Fatal("fallback notice emitted; planner failures must not degrade the turn")
				}
			}
		})
	}
}

// TestCoordinatorPropagatesPlannerErrorWhenTurnCancelled keeps cancellation
// semantics: a turn the user aborted must not silently restart on the executor.
func TestCoordinatorPropagatesPlannerErrorWhenTurnCancelled(t *testing.T) {
	exec := &mockProvider{name: "executor", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "Should not run."},
		{Type: provider.ChunkDone},
	}}
	executor := New(exec, tool.NewRegistry(), NewSession("exec-sys"), Options{}, event.Discard)
	coord := NewCoordinator(&errorProvider{name: "planner"}, NewSession("planner-sys"), nil, nil, Options{}, executor, 0, event.Discard, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := coord.Run(ctx, "fix the bug")
	if err == nil || !strings.Contains(err.Error(), "planner:") {
		t.Fatalf("Run = %v, want propagated planner error on cancelled turn", err)
	}
	if got := len(exec.requests); got != 0 {
		t.Fatalf("executor requests = %d, want none after user cancellation", got)
	}
}

// TestCoordinatorRollsBackPlannerSessionOnToolPlannerFailure covers the
// production two-model wiring (boot passes PlannerToolRegistry, so planning
// runs through planWithTools): when the tool-enabled planner fails, the turn
// fails and the rollback must not leave the planner session with a dangling
// user message or partial tool rounds — the next plan would otherwise start
// with consecutive user roles, which some providers reject.
func TestCoordinatorRollsBackPlannerSessionOnToolPlannerFailure(t *testing.T) {
	cases := []struct {
		name    string
		planner provider.Provider
	}{
		{"stream call fails", &errorProvider{name: "planner"}},
		{"fails after a tool round", &mockProvider{name: "planner", streams: [][]provider.Chunk{
			{
				{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "call-1", Name: "read_file", Arguments: `{"path":"main.go"}`}},
				{Type: provider.ChunkDone},
			},
			{
				{Type: provider.ChunkError, Err: fmt.Errorf("rate limited")},
			},
		}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			exec := &mockProvider{name: "executor", chunks: []provider.Chunk{
				{Type: provider.ChunkText, Text: "Done."},
				{Type: provider.ChunkDone},
			}}
			plannerReg := tool.NewRegistry()
			plannerReg.Add(coordinatorTestTool{name: "read_file", readOnly: true, output: "package main"})

			executor := New(exec, tool.NewRegistry(), NewSession("exec-sys"), Options{}, event.Discard)
			plannerSess := NewSession("planner-sys")
			coord := NewCoordinator(tc.planner, plannerSess, nil, PlannerToolRegistry(plannerReg), Options{}, executor, 0, event.Discard, nil)

			err := coord.Run(withNoClosedLoop(context.Background()), "fix the bug")
			if err == nil || !strings.Contains(err.Error(), "planner:") {
				t.Fatalf("Run = %v, want the propagated planner error", err)
			}
			if got := len(exec.requests); got != 0 {
				t.Fatalf("executor requests = %d, want none after the planner failure", got)
			}
			if n := len(plannerSess.Messages); n != 1 {
				t.Fatalf("planner session messages = %d, want rollback to system only", n)
			}
		})
	}
}

func TestCoordinatorPlannerSafetyBoundaryPreservesExecutionBoundaries(t *testing.T) {
	for _, route := range []PlannerRoute{PlannerRoutePlanOnly, PlannerRoutePlanForApproval} {
		t.Run(string(route), func(t *testing.T) {
			planner := &mockProvider{name: "planner", chunks: []provider.Chunk{
				{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "call-1", Name: "read_file", Arguments: `{"path":"main.go"}`}},
				{Type: provider.ChunkDone},
			}}
			exec := &mockProvider{name: "executor"}
			plannerReg := tool.NewRegistry()
			plannerReg.Add(coordinatorTestTool{name: "read_file", readOnly: true, output: "package main"})
			policy := func(context.Context, string) PlannerDecision {
				return PlannerDecision{Route: route, Reason: "explicit_boundary"}
			}

			executor := New(exec, tool.NewRegistry(), NewSession("exec-sys"), Options{}, event.Discard)
			plannerSess := NewSession("planner-sys")
			coord := NewCoordinatorWithPlannerPolicy(
				planner, plannerSess, nil, plannerReg, Options{MaxSteps: 1, MaxStepsKey: "planner emergency rounds"},
				executor, 0, event.Discard, policy,
			)

			err := coord.Run(withNoClosedLoop(context.Background()), "plan the migration")
			if err == nil || err.Error() != plannerSafetyBoundaryError {
				t.Fatalf("Run = %v, want the safe planner boundary error", err)
			}
			if got := len(exec.requests); got != 0 {
				t.Fatalf("executor requests = %d, want none across %s", got, route)
			}
			if got := len(plannerSess.Messages); got != 1 {
				t.Fatalf("planner session messages = %d, want the incomplete turn rolled back", got)
			}
		})
	}
}

func TestCoordinatorRollbackAfterRewriteDropsPausedPlannerToolCall(t *testing.T) {
	plannerSess := NewSession("planner-sys")
	before := plannerSess.Snapshot()
	rewriteBefore := plannerSess.RewriteVersion()

	plannerSess.Replace([]provider.Message{
		{Role: provider.RoleSystem, Content: "planner-sys"},
		{Role: provider.RoleUser, Content: summaryTagOpen + "\ncompacted research\n</summary>"},
		{Role: provider.RoleAssistant, Content: "Completed evidence from the bounded research rounds."},
	})
	plannerSess.IncrementRewrite()
	plannerSess.Add(provider.Message{Role: provider.RoleUser, Content: "Do not call any more tools; finalize."})
	plannerSess.Add(provider.Message{
		Role: provider.RoleAssistant,
		ToolCalls: []provider.ToolCall{{
			ID: "ignored-finalization-call", Name: "read_file", Arguments: `{"path":"more.go"}`,
		}},
	})

	coord := &Coordinator{plannerSess: plannerSess}
	coord.rollbackPlannerTurn(before, rewriteBefore)

	msgs := plannerSess.Snapshot()
	if len(msgs) != 3 {
		t.Fatalf("planner session messages = %d, want compacted prefix plus completed evidence", len(msgs))
	}
	if last := msgs[len(msgs)-1]; last.Role != provider.RoleAssistant ||
		len(last.ToolCalls) != 0 || last.Content == "" {
		t.Fatalf("planner session has an unusable pause tail: %+v", last)
	}
	if normalized := provider.NormalizeMessages(msgs); len(normalized) != len(msgs) {
		t.Fatalf("planner session still needs tool-pair repair after rollback: %+v", normalized)
	}
}

// TestCoordinatorRunsExecutorWhenMarkerNotAlone is retired with the marker
// contract; prose-plan routing tests above cover the submitted-plan path.

// TestCoordinatorHandoffSurvivesPlannerCompaction pins plan delivery across
// projection compaction. Projection compaction must not lose this turn's
// submitted plan before the executor handoff is built.
func TestCoordinatorHandoffSurvivesPlannerCompaction(t *testing.T) {
	planner := &mockProvider{name: "planner", streams: [][]provider.Chunk{
		{ // preflight compaction on the large filler history (estimate-based)
			{Type: provider.ChunkText, Text: "- goal: prior filler\n- pending: plan the fix"},
			{Type: provider.ChunkDone},
		},
		// the plan turn after projection is in place: compaction usage on the
		// submit_plan round keeps the estimate-based preflight armed
		{
			{Type: provider.ChunkUsage, Usage: &provider.Usage{PromptTokens: 400, TotalTokens: 450}},
			{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "call-1", Name: "submit_plan", Arguments: `{"objective":"fix","steps":[{"title":"Edit main.go and add the missing guard."}]}`}},
			{Type: provider.ChunkDone},
		},
	}}
	exec := &mockProvider{name: "executor", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "Done."},
		{Type: provider.ChunkDone},
	}}

	plannerReg := tool.NewRegistry()
	plannerReg.Add(coordinatorTestTool{name: "read_file", readOnly: true, output: "ok"})

	executor := New(exec, tool.NewRegistry(), NewSession("exec-sys"), Options{}, event.Discard)
	plannerSess := NewSession("planner-sys")
	// Preset enough planner history that context preflight compacts before the
	// plan stream. Canonical history stays intact; the handoff must still find
	// the plan on the canonical transcript.
	filler := strings.Repeat("planner history filler. ", 150)
	for range 3 {
		plannerSess.Add(provider.Message{Role: provider.RoleUser, Content: filler})
		plannerSess.Add(provider.Message{Role: provider.RoleAssistant, Content: filler})
	}
	coord := NewCoordinator(planner, plannerSess, nil, PlannerToolRegistry(plannerReg), Options{ContextWindow: 2000}, executor, 0, event.Discard, nil)

	if err := coord.Run(withNoClosedLoop(context.Background()), "fix the bug"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Projection compaction no longer rewrites the planner session; handoff
	// must still deliver the plan even when RewriteVersion stays 0.
	if plannerSess.RewriteVersion() != 0 {
		t.Fatalf("canonical rewrite version = %d, want 0", plannerSess.RewriteVersion())
	}
	if got := len(exec.requests); got == 0 {
		t.Fatal("executor never ran")
	}
	got := lastUser(exec.requests[0])
	if !strings.Contains(got, "Edit main.go and add the missing guard.") || !strings.Contains(got, executorHandoffMarker) {
		t.Fatalf("executor input lost the plan handoff after planner compaction:\n%s", got)
	}
}

// TestCoordinatorNoOpConclusionAttributedToPlanner is retired with the no-op
// relay path: submitted plans are emitted with planner attribution in
// planWithTools, covered by TestSubmittedPlanIsRenderedToTheSink.

// TestCoordinatorHandoffOmitsToolContextWithoutMCPTools checks that the handoff
// does not restate the built-in tool schema: the tool-context block exists to
// counter planner claims about MCP availability and is dropped entirely when
// the executor carries no MCP tools.
func TestCoordinatorHandoffOmitsToolContextWithoutMCPTools(t *testing.T) {
	planner := &mockProvider{name: "planner", chunks: submitPlanChunk(`{"objective":"fix the guard","steps":[{"title":"Edit main.go and add the missing guard."}]}`)}
	exec := &mockProvider{name: "executor", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "Done."},
		{Type: provider.ChunkDone},
	}}

	execReg := tool.NewRegistry()
	execReg.Add(coordinatorTestTool{name: "write_file", readOnly: false, output: "ok"})
	executor := New(exec, execReg, NewSession("exec-sys"), Options{}, event.Discard)
	coord := NewCoordinator(planner, NewSession("planner-sys"), nil, plannerRegistryWithSubmitPlan(), Options{}, executor, 0, event.Discard, nil)

	if err := coord.Run(withNoClosedLoop(context.Background()), "fix the missing guard"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got := lastUser(exec.requests[0])
	for _, unwanted := range []string{"Executor tool context", "Tool names include"} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("handoff restates built-in tool schema (%q):\n%s", unwanted, got)
		}
	}
	if !strings.Contains(got, "Edit main.go") {
		t.Fatalf("handoff missing the plan: %q", got)
	}
}

// TestCoordinatorPassesTurnContextToPlannerGate pins the C2 contract: the gate
// receives the live turn context, so a classifier-backed gate is cancelled
// with the turn instead of running out its own timeout.
func TestCoordinatorPassesTurnContextToPlannerGate(t *testing.T) {
	type gateCtxKey struct{}
	exec := &mockProvider{name: "executor", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "It does X."},
		{Type: provider.ChunkDone},
	}}
	executor := New(exec, tool.NewRegistry(), NewSession("exec-sys"), Options{}, event.Discard)

	var sawTurnValue bool
	gate := func(ctx context.Context, _ string) bool {
		sawTurnValue = ctx.Value(gateCtxKey{}) != nil
		return false
	}
	coord := NewCoordinator(&mockProvider{name: "planner"}, NewSession("planner-sys"), nil, nil, Options{}, executor, 0, event.Discard, gate)

	ctx := context.WithValue(context.Background(), gateCtxKey{}, "turn")
	if err := coord.Run(ctx, "what does this do?"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !sawTurnValue {
		t.Fatal("planner gate did not receive the turn context")
	}
}

// TestCoordinatorFailedTurnRollbackKeepsCompaction pins rollback economics
// under projection compaction: when preflight/auto compaction fires and the
// planner then fails, restoring the pre-turn snapshot must not erase the
// projection or leave a dangling plain user turn that would produce
// consecutive user roles on the next plan.
func TestCoordinatorFailedTurnRollbackKeepsCompaction(t *testing.T) {
	planner := &mockProvider{name: "planner", streams: [][]provider.Chunk{
		{ // preflight compaction on large filler history
			{Type: provider.ChunkText, Text: "- goal: guard work\n- pending: continue"},
			{Type: provider.ChunkDone},
		},
		{ // tool round after projection is installed
			{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "call-1", Name: "read_file", Arguments: `{"path":"main.go"}`}},
			{Type: provider.ChunkUsage, Usage: &provider.Usage{PromptTokens: 400, TotalTokens: 450}},
			{Type: provider.ChunkDone},
		},
		{ // the next planner round fails
			{Type: provider.ChunkError, Err: fmt.Errorf("rate limited")},
		},
	}}
	exec := &mockProvider{name: "executor", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "Done."},
		{Type: provider.ChunkDone},
	}}

	plannerReg := tool.NewRegistry()
	plannerReg.Add(coordinatorTestTool{name: "read_file", readOnly: true, output: "package main"})

	executor := New(exec, tool.NewRegistry(), NewSession("exec-sys"), Options{}, event.Discard)
	plannerSess := NewSession("planner-sys")
	filler := strings.Repeat("planner history filler. ", 150)
	for range 3 {
		plannerSess.Add(provider.Message{Role: provider.RoleUser, Content: filler})
		plannerSess.Add(provider.Message{Role: provider.RoleAssistant, Content: filler})
	}
	coord := NewCoordinator(planner, plannerSess, nil, PlannerToolRegistry(plannerReg), Options{ContextWindow: 2000}, executor, 0, event.Discard, nil)

	err := coord.Run(withNoClosedLoop(context.Background()), "fix the bug")
	if err == nil || !strings.Contains(err.Error(), "planner:") {
		t.Fatalf("Run = %v, want the propagated planner error", err)
	}
	// The failed turn runs nothing: executor requests stay at zero.
	if got := len(exec.requests); got != 0 {
		t.Fatalf("executor requests = %d, want none after the planner failure", got)
	}
	// Canonical transcript is never rewrite-compacted.
	if plannerSess.RewriteVersion() != 0 {
		t.Fatalf("canonical rewrite version = %d, want 0", plannerSess.RewriteVersion())
	}
	// Canonical history is restored/cleaned without a dangling user turn so the
	// next plan can continue. Projection lives on the planner agent and is not
	// wiped by snapshot rollback of Session.Messages alone.
	msgs := plannerSess.Snapshot()
	if last := msgs[len(msgs)-1]; last.Role == provider.RoleUser && !isCompactionSummary(last) {
		t.Fatalf("planner session ends in a plain user message after rollback: %q", last.Content)
	}
}

// TestCoordinatorPersistsDeniedPlanTurnToExecutorSession pins the denial
// bookkeeping: a plan the user declines must still land in the executor
// session (like the no-op path) so the turn survives save/reload, with a note
// telling the next executor turn that nothing ran, plus a user-facing notice.
func TestCoordinatorPersistsDeniedPlanTurnToExecutorSession(t *testing.T) {
	planner := &mockProvider{name: "planner", chunks: submitPlanChunk(`{"objective":"rewrite auth","requires_approval":true,"steps":[{"title":"rewrite auth"}]}`)}
	exec := &mockProvider{name: "executor", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "should not run"},
		{Type: provider.ChunkDone},
	}}
	sink := &recordSink{}
	executor := New(exec, tool.NewRegistry(), NewSession("exec-sys"), Options{}, sink)
	coord := NewCoordinator(planner, NewSession("planner-sys"), nil, plannerRegistryWithSubmitPlan(), Options{}, executor, 0, sink, nil)
	gate := &coordinatorApprovalGate{allow: false}
	coord.SetPlannerPlanApprover(gate)

	if err := coord.Run(withNoClosedLoop(context.Background()), "rewrite auth"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if gate.calls != 1 {
		t.Fatalf("approval gate calls = %d, want 1", gate.calls)
	}
	if len(exec.requests) != 0 {
		t.Fatal("executor must not run when the plan is denied")
	}
	msgs := executor.sess.conversation.Messages
	if len(msgs) < 2 {
		t.Fatalf("executor session messages = %d, want the denied turn persisted", len(msgs))
	}
	last := msgs[len(msgs)-1]
	if last.Role != provider.RoleAssistant || !strings.Contains(last.Content, plannerPlanNotApprovedNote) {
		t.Fatalf("last executor message = %q (%s), want plan with not-approved note", last.Content, last.Role)
	}
	prev := msgs[len(msgs)-2]
	if prev.Role != provider.RoleUser || !strings.Contains(prev.Content, "rewrite auth") {
		t.Fatalf("persisted user turn = %q (%s), want original input", prev.Content, prev.Role)
	}
	foundNotice := false
	for _, e := range sink.kinds(event.Notice) {
		if strings.Contains(e.Text, "not approved") {
			foundNotice = true
		}
	}
	if !foundNotice {
		t.Fatal("denied plan should emit a user-facing notice")
	}
}

// TestCoordinatorApprovalGateReadsOnlyTheField pins the negation side of the
// field-only contract: a plan that rules out an approval round in prose (and in
// its requires_approval field) hands off directly instead of raising a
// needless approval prompt.
func TestCoordinatorApprovalGateReadsOnlyTheField(t *testing.T) {
	planner := &mockProvider{name: "planner", chunks: submitPlanChunk(`{"objective":"修改 config","steps":[{"title":"修改 config.go，无需等待用户批准，直接执行修改"}]}`)}
	exec := &mockProvider{name: "executor", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "Done."},
		{Type: provider.ChunkDone},
	}}
	executor := New(exec, tool.NewRegistry(), NewSession("exec-sys"), Options{}, event.Discard)
	coord := NewCoordinator(planner, NewSession("planner-sys"), nil, plannerRegistryWithSubmitPlan(), Options{}, executor, 0, event.Discard, nil)
	gate := &coordinatorApprovalGate{allow: false}
	coord.SetPlannerPlanApprover(gate)

	if err := coord.Run(withNoClosedLoop(context.Background()), "tweak config"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if gate.calls != 0 {
		t.Fatalf("approval gate calls = %d, want 0 for requires_approval=false", gate.calls)
	}
	if len(exec.requests) == 0 {
		t.Fatal("executor should run directly for a plan that does not require approval")
	}
}
