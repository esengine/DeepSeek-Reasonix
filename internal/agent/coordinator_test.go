package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"reasonix/internal/event"
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
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == provider.RoleUser {
			return req.Messages[i].Content
		}
	}
	return ""
}

// TestCoordinatorHandsPlanToExecutor checks the two-session handoff: the planner
// sees the raw task in its own session, and the executor receives the plan.
func TestCoordinatorHandsPlanToExecutor(t *testing.T) {
	planner := &mockProvider{name: "planner", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "1. read main.go\n2. fix the loop"},
		{Type: provider.ChunkDone},
	}}
	exec := &mockProvider{name: "executor", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "Done."},
		{Type: provider.ChunkDone},
	}}

	executor := New(exec, tool.NewRegistry(), NewSession("exec-sys"), Options{}, event.Discard)
	plannerSess := NewSession("planner-sys")
	coord := NewCoordinator(planner, plannerSess, nil, nil, Options{}, executor, 0, event.Discard, nil)

	if err := coord.Run(context.Background(), "fix the bug"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got := lastUser(planner.lastReq); !strings.Contains(got, "fix the bug") {
		t.Errorf("planner saw user %q, want it to contain the task", got)
	}
	if got := lastUser(exec.requests[0]); !strings.Contains(got, "read main.go") || !strings.Contains(got, "fix the bug") || !strings.Contains(got, "You are the executor now") {
		t.Errorf("executor saw user %q, want task + plan", got)
	}
	// planner session must accumulate (system, user, assistant-plan) so its
	// prefix grows prepend-only and stays cache-stable.
	if n := len(plannerSess.Messages); n != 3 {
		t.Errorf("planner session has %d messages, want 3", n)
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

type coordinatorDecisionGate struct {
	calls  int
	answer string
}

func (g *coordinatorDecisionGate) RunWithPlannerUserDecision(ctx context.Context, _ string, _ event.AskQuestion, run func(context.Context, string) error) error {
	g.calls++
	if strings.TrimSpace(g.answer) == "" {
		return nil
	}
	return run(ctx, g.answer)
}

func TestCoordinatorBindsPlannerApprovalRequestBeforeExecutor(t *testing.T) {
	planner := &mockProvider{name: "planner", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "Plan:\n1. edit main.go\n\n是否批准这个方案？"},
		{Type: provider.ChunkDone},
	}}
	exec := &mockProvider{name: "executor", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "Should not run."},
		{Type: provider.ChunkDone},
	}}

	executor := New(exec, tool.NewRegistry(), NewSession("exec-sys"), Options{}, event.Discard)
	coord := NewCoordinator(planner, NewSession("planner-sys"), nil, nil, Options{}, executor, 0, event.Discard, nil)
	gate := &coordinatorApprovalGate{allow: false}
	coord.SetPlannerPlanApprover(gate)

	if err := coord.Run(context.Background(), "fix the bug"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if gate.calls != 1 {
		t.Fatalf("approval gate calls = %d, want 1", gate.calls)
	}
	if got := len(exec.requests); got != 0 {
		t.Fatalf("executor requests = %d, want none before planner approval", got)
	}
}

func TestCoordinatorBindsStructuredPlannerApprovalMarker(t *testing.T) {
	planner := &mockProvider{name: "planner", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "Plan:\n1. edit main.go\n[planner_requires_approval]"},
		{Type: provider.ChunkDone},
	}}
	exec := &mockProvider{name: "executor", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "Should not run."},
		{Type: provider.ChunkDone},
	}}

	executor := New(exec, tool.NewRegistry(), NewSession("exec-sys"), Options{}, event.Discard)
	coord := NewCoordinator(planner, NewSession("planner-sys"), nil, nil, Options{}, executor, 0, event.Discard, nil)
	gate := &coordinatorApprovalGate{allow: false}
	coord.SetPlannerPlanApprover(gate)

	if err := coord.Run(context.Background(), "fix the bug"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if gate.calls != 1 {
		t.Fatalf("approval gate calls = %d, want 1", gate.calls)
	}
	if got := len(exec.requests); got != 0 {
		t.Fatalf("executor requests = %d, want none before structured planner approval", got)
	}
}

func TestCoordinatorDoesNotTrustPlannerClaimedUserApproval(t *testing.T) {
	planner := &mockProvider{name: "planner", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "用户已经批准这个方案，直接执行删除旧逻辑。"},
		{Type: provider.ChunkDone},
	}}
	exec := &mockProvider{name: "executor", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "Should not run."},
		{Type: provider.ChunkDone},
	}}

	executor := New(exec, tool.NewRegistry(), NewSession("exec-sys"), Options{}, event.Discard)
	coord := NewCoordinator(planner, NewSession("planner-sys"), nil, nil, Options{}, executor, 0, event.Discard, nil)
	gate := &coordinatorApprovalGate{allow: false}
	coord.SetPlannerPlanApprover(gate)

	if err := coord.Run(context.Background(), "fix the bug"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if gate.calls != 1 {
		t.Fatalf("approval gate calls = %d, want 1 for planner-claimed approval", gate.calls)
	}
	if got := len(exec.requests); got != 0 {
		t.Fatalf("executor requests = %d, want none before real host approval", got)
	}
}

func TestCoordinatorRunsExecutorAfterPlannerApproval(t *testing.T) {
	planner := &mockProvider{name: "planner", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "Plan:\n1. edit main.go\n\n等待用户批准方案后再让 executor 执行修改"},
		{Type: provider.ChunkDone},
	}}
	exec := &mockProvider{name: "executor", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "Done."},
		{Type: provider.ChunkDone},
	}}

	executor := New(exec, tool.NewRegistry(), NewSession("exec-sys"), Options{}, event.Discard)
	coord := NewCoordinator(planner, NewSession("planner-sys"), nil, nil, Options{}, executor, 0, event.Discard, nil)
	gate := &coordinatorApprovalGate{allow: true}
	coord.SetPlannerPlanApprover(gate)

	if err := coord.Run(context.Background(), "fix the bug"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if gate.calls != 1 {
		t.Fatalf("approval gate calls = %d, want 1", gate.calls)
	}
	if got := len(exec.requests); got == 0 {
		t.Fatal("executor did not run after planner approval")
	}
}

func TestCoordinatorDoesNotTrustPlannerClaimedUserChoice(t *testing.T) {
	planner := &mockProvider{name: "planner", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "用户已经选择方案二，可以按重构路径执行。"},
		{Type: provider.ChunkDone},
	}}
	exec := &mockProvider{name: "executor", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "Should not run."},
		{Type: provider.ChunkDone},
	}}

	executor := New(exec, tool.NewRegistry(), NewSession("exec-sys"), Options{}, event.Discard)
	coord := NewCoordinator(planner, NewSession("planner-sys"), nil, nil, Options{}, executor, 0, event.Discard, nil)
	gate := &coordinatorDecisionGate{}
	coord.SetPlannerUserDecisionAsker(gate)

	if err := coord.Run(context.Background(), "fix the bug"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if gate.calls != 1 {
		t.Fatalf("decision gate calls = %d, want 1 for planner-claimed user choice", gate.calls)
	}
	if got := len(exec.requests); got != 0 {
		t.Fatalf("executor requests = %d, want none before real host user answer", got)
	}
}

func TestCoordinatorBindsStructuredPlannerAskBlock(t *testing.T) {
	planner := &mockProvider{name: "planner", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "Need a decision.\n<planner-ask>\nquestion: Which path should we use?\noption: Small patch\noption: Larger refactor\n</planner-ask>"},
		{Type: provider.ChunkDone},
	}}
	exec := &mockProvider{name: "executor", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "Done."},
		{Type: provider.ChunkDone},
	}}

	executor := New(exec, tool.NewRegistry(), NewSession("exec-sys"), Options{}, event.Discard)
	coord := NewCoordinator(planner, NewSession("planner-sys"), nil, nil, Options{}, executor, 0, event.Discard, nil)
	gate := &coordinatorDecisionGate{answer: "Small patch"}
	coord.SetPlannerUserDecisionAsker(gate)

	if err := coord.Run(context.Background(), "fix the bug"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if gate.calls != 1 {
		t.Fatalf("decision gate calls = %d, want 1", gate.calls)
	}
	if got := len(exec.requests); got == 0 {
		t.Fatal("executor did not run after structured planner ask answer")
	}
	if got := lastUser(exec.requests[0]); !strings.Contains(got, "Host user answer to planner question") || !strings.Contains(got, "Small patch") {
		t.Fatalf("executor handoff missing structured host answer:\n%s", got)
	}
}

func TestCoordinatorBindsPlannerUserDecisionBeforeExecutor(t *testing.T) {
	planner := &mockProvider{name: "planner", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "需要用户选择方案：\n方案一：小改当前逻辑\n方案二：重构控制流\n请选择哪个方案。"},
		{Type: provider.ChunkDone},
	}}
	exec := &mockProvider{name: "executor", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "Should not run."},
		{Type: provider.ChunkDone},
	}}

	executor := New(exec, tool.NewRegistry(), NewSession("exec-sys"), Options{}, event.Discard)
	coord := NewCoordinator(planner, NewSession("planner-sys"), nil, nil, Options{}, executor, 0, event.Discard, nil)
	gate := &coordinatorDecisionGate{}
	coord.SetPlannerUserDecisionAsker(gate)

	if err := coord.Run(context.Background(), "fix the bug"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if gate.calls != 1 {
		t.Fatalf("decision gate calls = %d, want 1", gate.calls)
	}
	if got := len(exec.requests); got != 0 {
		t.Fatalf("executor requests = %d, want none before user decision", got)
	}
}

func TestCoordinatorDoesNotAskForOrdinaryPlanVerificationWording(t *testing.T) {
	planner := &mockProvider{name: "planner", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "Plan:\n1. 确认文件存在\n2. 修改 main.go\n3. 运行测试"},
		{Type: provider.ChunkDone},
	}}
	exec := &mockProvider{name: "executor", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "Done."},
		{Type: provider.ChunkDone},
	}}

	executor := New(exec, tool.NewRegistry(), NewSession("exec-sys"), Options{}, event.Discard)
	coord := NewCoordinator(planner, NewSession("planner-sys"), nil, nil, Options{}, executor, 0, event.Discard, nil)
	gate := &coordinatorDecisionGate{answer: "should not be used"}
	coord.SetPlannerUserDecisionAsker(gate)

	if err := coord.Run(context.Background(), "fix the bug"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if gate.calls != 0 {
		t.Fatalf("decision gate calls = %d, want no AskRequest for ordinary verification wording", gate.calls)
	}
	if got := len(exec.requests); got == 0 {
		t.Fatal("executor should run for ordinary plan wording")
	}
}

func TestCoordinatorPassesHostUserDecisionToExecutor(t *testing.T) {
	planner := &mockProvider{name: "planner", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "需要用户选择方案：\n方案一：小改当前逻辑\n方案二：重构控制流\n请选择哪个方案。"},
		{Type: provider.ChunkDone},
	}}
	exec := &mockProvider{name: "executor", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "Done."},
		{Type: provider.ChunkDone},
	}}

	executor := New(exec, tool.NewRegistry(), NewSession("exec-sys"), Options{}, event.Discard)
	coord := NewCoordinator(planner, NewSession("planner-sys"), nil, nil, Options{}, executor, 0, event.Discard, nil)
	gate := &coordinatorDecisionGate{answer: "方案二：重构控制流"}
	coord.SetPlannerUserDecisionAsker(gate)

	if err := coord.Run(context.Background(), "fix the bug"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if gate.calls != 1 {
		t.Fatalf("decision gate calls = %d, want 1", gate.calls)
	}
	if got := len(exec.requests); got == 0 {
		t.Fatal("executor did not run after user decision")
	}
	if got := lastUser(exec.requests[0]); !strings.Contains(got, "Host user answer to planner question") || !strings.Contains(got, "方案二") {
		t.Fatalf("executor handoff missing host user answer:\n%s", got)
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

	if err := coord.Run(context.Background(), "what does this function do?"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if planner.lastReq.Messages != nil {
		t.Error("planner should not be called for a skipped turn")
	}
	if got := lastUser(exec.lastReq); got != "what does this function do?" {
		t.Errorf("executor saw %q, want the raw input with no plan handoff", got)
	}
	if n := len(plannerSess.Messages); n != 1 { // just the system message
		t.Errorf("planner session has %d messages, want 1 (untouched)", n)
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
		{
			{Type: provider.ChunkText, Text: "1. follow the loaded rule\n2. edit the narrow file"},
			{Type: provider.ChunkDone},
		},
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

	if err := coord.Run(context.Background(), "fix the bug"); err != nil {
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
	planner := &mockProvider{name: "planner", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "1. inspect the narrow path"},
		{Type: provider.ChunkDone},
	}}
	exec := &mockProvider{name: "executor", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "Done."},
		{Type: provider.ChunkDone},
	}}

	executor := New(exec, tool.NewRegistry(), NewSession("exec-sys"), Options{ReasoningLanguage: "zh"}, event.Discard)
	coord := NewCoordinator(planner, NewSession("planner-sys"), nil, tool.NewRegistry(), Options{ReasoningLanguage: "zh"}, executor, 0, event.Discard, nil)
	coord.SetReasoningLanguage("auto")

	if err := coord.Run(context.Background(), "plan a change"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got := lastUser(planner.requests[0]); strings.Contains(got, "<reasoning-language>") {
		t.Fatalf("planner should clear stale reasoning language after live auto update, got %q", got)
	}
	if got := lastUser(exec.requests[0]); strings.Contains(got, "<reasoning-language>") {
		t.Fatalf("executor should clear stale reasoning language after live auto update, got %q", got)
	}
}

func TestCoordinatorPlannerMaxStepsUsesPlannerConfigKey(t *testing.T) {
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
	executor := New(exec, tool.NewRegistry(), NewSession("exec-sys"), Options{}, event.Discard)
	coord := NewCoordinator(planner, NewSession("planner-sys"), nil, PlannerToolRegistry(parentReg), Options{
		MaxSteps:    2,
		MaxStepsKey: "agent.planner_max_steps",
	}, executor, 0, event.Discard, nil)

	err := coord.Run(context.Background(), "plan a change")
	if err == nil {
		t.Fatal("Run should pause when the planner reaches its configured step limit")
	}
	msg := err.Error()
	if !strings.Contains(msg, "planner: paused after 2 tool-call rounds (agent.planner_max_steps)") {
		t.Fatalf("pause error = %q, want planner_max_steps message", msg)
	}
	if strings.Contains(msg, "agent.max_steps") {
		t.Fatalf("planner pause should not point at agent.max_steps: %q", msg)
	}
	if got := len(planner.requests); got != 3 {
		t.Fatalf("planner requests = %d, want exactly the configured 2 rounds", got)
	}
	if len(exec.requests) != 0 {
		t.Fatal("executor should not run when planner pauses before producing a plan")
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
		{
			{Type: provider.ChunkText, Text: "1. use both files"},
			{Type: provider.ChunkDone},
		},
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
		MaxStepsKey: "agent.planner_max_steps",
	}, executor, 0, event.Discard, nil)

	if err := coord.Run(context.Background(), "plan a change"); err != nil {
		t.Fatalf("Run with planner max steps 0 should not pause: %v", err)
	}
	if got := len(planner.requests); got != 3 {
		t.Fatalf("planner requests = %d, want all 3 scripted planner turns", got)
	}
	if got := lastUser(exec.requests[0]); !strings.Contains(got, "use both files") {
		t.Fatalf("executor did not receive planner output: %q", got)
	}
}

func TestCoordinatorNudgesExecutorThatAnswersWithoutActing(t *testing.T) {
	planner := &mockProvider{name: "planner", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "Write the requested skill file."},
		{Type: provider.ChunkDone},
	}}
	// The first turn is a plain final answer with no tool call and no
	// planner-vocabulary — the nudge must fire on the missing action, not on words.
	exec := &mockProvider{name: "executor", streams: [][]provider.Chunk{
		{
			{Type: provider.ChunkText, Text: "这个计划看起来没问题,应该很好实现。"},
			{Type: provider.ChunkDone},
		},
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
	coord := NewCoordinator(planner, NewSession("planner-sys"), nil, nil, Options{}, executor, 0, event.Discard, nil)

	if err := coord.Run(context.Background(), "install the skill"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := len(exec.requests); got != 3 {
		t.Fatalf("executor requests = %d, want answer-without-acting, nudge tool call, final answer", got)
	}
	if got := lastUser(exec.requests[1]); !strings.Contains(got, "Use your available tools now to carry out the task") {
		t.Fatalf("second executor request missing handoff nudge message: %q", got)
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
	planner := &mockProvider{name: "planner", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "Tell the user to open the audio app, enable the Peace checkbox, and play a song to compare the difference."},
		{Type: provider.ChunkDone},
	}}
	exec := &mockProvider{name: "executor", streams: [][]provider.Chunk{
		{
			{Type: provider.ChunkText, Text: "Open the audio app, enable the Peace checkbox, then play a familiar song and compare the sound with the switch on and off."},
			{Type: provider.ChunkDone},
		},
	}}

	executor := New(exec, tool.NewRegistry(), NewSession("exec-sys"), Options{}, event.Discard)
	coord := NewCoordinator(planner, NewSession("planner-sys"), nil, nil, Options{}, executor, 0, event.Discard, nil)

	if err := coord.Run(context.Background(), "I just installed EqualizerAPO, now what?"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := len(exec.requests); got != 1 {
		t.Fatalf("executor requests = %d, want one guidance-only final answer with no handoff nudge", got)
	}
}

func TestCoordinatorAllowsGuidanceOnlyPlanWithExecutorToolContext(t *testing.T) {
	planner := &mockProvider{name: "planner", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "Tell the user to open the audio app, enable the checkbox, and listen to compare the difference."},
		{Type: provider.ChunkDone},
	}}
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
	coord := NewCoordinator(planner, NewSession("planner-sys"), nil, nil, Options{}, executor, 0, event.Discard, nil)

	if err := coord.Run(context.Background(), "Please advise on the manual audio check."); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := len(exec.requests); got != 1 {
		t.Fatalf("executor requests = %d, want guidance final answer without nudge despite tool context", got)
	}
}

func TestCoordinatorNudgesWorkTaskEvenIfPlannerMentionsUserGuidance(t *testing.T) {
	planner := &mockProvider{name: "planner", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "Tell the user to edit main.go and add the missing branch."},
		{Type: provider.ChunkDone},
	}}
	exec := &mockProvider{name: "executor", streams: [][]provider.Chunk{
		{
			{Type: provider.ChunkText, Text: "Open main.go and add the missing branch in the handler."},
			{Type: provider.ChunkDone},
		},
		{
			{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "call-1", Name: "write_file", Arguments: `{"path":"main.go"}`}},
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
	coord := NewCoordinator(planner, NewSession("planner-sys"), nil, nil, Options{}, executor, 0, event.Discard, nil)

	if err := coord.Run(context.Background(), "fix the bug"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := len(exec.requests); got != 3 {
		t.Fatalf("executor requests = %d, want text answer, nudge tool call, final answer", got)
	}
	if got := lastUser(exec.requests[1]); !strings.Contains(got, "Use your available tools now to carry out the task") {
		t.Fatalf("second executor request missing handoff nudge message: %q", got)
	}
}

func TestCoordinatorNudgesMixedGuidanceAndWorkTask(t *testing.T) {
	planner := &mockProvider{name: "planner", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "Tell the user to summarize the behavior and update README."},
		{Type: provider.ChunkDone},
	}}
	exec := &mockProvider{name: "executor", streams: [][]provider.Chunk{
		{
			{Type: provider.ChunkText, Text: "Here is the current behavior summary."},
			{Type: provider.ChunkDone},
		},
		{
			{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "call-1", Name: "write_file", Arguments: `{"path":"README.md"}`}},
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
	coord := NewCoordinator(planner, NewSession("planner-sys"), nil, nil, Options{}, executor, 0, event.Discard, nil)

	if err := coord.Run(context.Background(), "summarize the current behavior and update the README"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := len(exec.requests); got != 3 {
		t.Fatalf("executor requests = %d, want mixed guidance/work task to nudge before tool call", got)
	}
	if got := lastUser(exec.requests[1]); !strings.Contains(got, "Use your available tools now to carry out the task") {
		t.Fatalf("second executor request missing handoff nudge message: %q", got)
	}
}

func TestCoordinatorSkipsExecutorWhenPlannerConcludesNoChanges(t *testing.T) {
	planner := &mockProvider{name: "planner", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "No changes are needed; the current implementation already handles this.\n[no_changes]"},
		{Type: provider.ChunkDone},
	}}
	exec := &mockProvider{name: "executor", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "Should not run."},
		{Type: provider.ChunkDone},
	}}

	executor := New(exec, tool.NewRegistry(), NewSession("exec-sys"), Options{}, event.Discard)
	coord := NewCoordinator(planner, NewSession("planner-sys"), nil, nil, Options{}, executor, 0, event.Discard, nil)

	if err := coord.Run(context.Background(), "check whether the fix is already present"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := len(exec.requests); got != 0 {
		t.Fatalf("executor requests = %d, want skip after no-op planner conclusion", got)
	}
	messages := executor.session.Messages
	if got := len(messages); got != 3 {
		t.Fatalf("executor session messages = %d, want system + user + no-op assistant", got)
	}
	if got := messages[1].Content; !strings.Contains(got, "check whether the fix is already present") {
		t.Fatalf("persisted executor user message = %q, want original task", got)
	}
	if got := messages[2].Content; !strings.Contains(got, "No changes are needed") {
		t.Fatalf("persisted executor assistant message = %q, want no-op planner conclusion", got)
	}
}

func TestCoordinatorDoesNotTreatGenericPositivePlanAsNoOp(t *testing.T) {
	planner := &mockProvider{name: "planner", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "Looks good. Edit main.go and add the missing guard."},
		{Type: provider.ChunkDone},
	}}
	exec := &mockProvider{name: "executor", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "Done."},
		{Type: provider.ChunkDone},
	}}

	executor := New(exec, tool.NewRegistry(), NewSession("exec-sys"), Options{}, event.Discard)
	coord := NewCoordinator(planner, NewSession("planner-sys"), nil, nil, Options{}, executor, 0, event.Discard, nil)

	if err := coord.Run(context.Background(), "fix the missing guard"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := len(exec.requests); got == 0 {
		t.Fatal("executor should run for a plan that still contains work")
	}
}

func TestCoordinatorDoesNotSkipExecutorForPartialNoOpPlanWithActions(t *testing.T) {
	planner := &mockProvider{name: "planner", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "No changes are needed in code, but run the test suite."},
		{Type: provider.ChunkDone},
	}}
	exec := &mockProvider{name: "executor", streams: [][]provider.Chunk{
		{
			{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "call-1", Name: "bash", Arguments: `{"cmd":"go test ./..."}`}},
			{Type: provider.ChunkDone},
		},
		{
			{Type: provider.ChunkText, Text: "Tests passed."},
			{Type: provider.ChunkDone},
		},
	}}

	execReg := tool.NewRegistry()
	execReg.Add(coordinatorTestTool{name: "bash", readOnly: false, output: "ok"})
	executor := New(exec, execReg, NewSession("exec-sys"), Options{}, event.Discard)
	coord := NewCoordinator(planner, NewSession("planner-sys"), nil, nil, Options{}, executor, 0, event.Discard, nil)

	if err := coord.Run(context.Background(), "check the implementation and test it"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := len(exec.requests); got != 2 {
		t.Fatalf("executor requests = %d, want tool execution and final answer", got)
	}
}

func TestCoordinatorHandoffAffirmsExecutorToolSchemasWhenPlannerClaimsNoMCP(t *testing.T) {
	planner := &mockProvider{name: "planner", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "I only have read-only tools and cannot access GitHub MCP; use the executor to search GitHub."},
		{Type: provider.ChunkDone},
	}}
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
	coord := NewCoordinator(planner, NewSession("planner-sys"), nil, nil, Options{}, executor, 0, event.Discard, nil)

	if err := coord.Run(context.Background(), "search GitHub discussions"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := len(exec.requests); got != 3 {
		t.Fatalf("executor requests = %d, want initial answer, corrective nudge, final answer", got)
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
	retry := lastUser(exec.requests[1])
	for _, want := range []string{
		"The tool schema is still attached to this executor request",
		"Do not invent that MCP servers or tools are unavailable",
	} {
		if !strings.Contains(retry, want) {
			t.Fatalf("executor retry nudge missing %q:\n%s", want, retry)
		}
	}
}

func TestCoordinatorDoesNotNudgeExecutorThatActs(t *testing.T) {
	planner := &mockProvider{name: "planner", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "Write the requested skill file."},
		{Type: provider.ChunkDone},
	}}
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
	coord := NewCoordinator(planner, NewSession("planner-sys"), nil, nil, Options{}, executor, 0, event.Discard, nil)

	if err := coord.Run(context.Background(), "install the skill"); err != nil {
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
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func BenchmarkPlannerToolRegistry(b *testing.B) {
	parentReg := tool.NewRegistry()
	for i := 0; i < 200; i++ {
		parentReg.Add(coordinatorTestTool{
			name:     fmt.Sprintf("tool_%03d", i),
			readOnly: i%3 != 0,
		})
	}
	parentReg.Add(coordinatorTestTool{name: "todo_write", readOnly: true})
	parentReg.Add(coordinatorTestTool{name: "write_file", readOnly: false})

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
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

// TestIsNoOpPlan pins the no-op conclusion contract: only a final non-empty
// line that is exactly the [no_changes] marker skips the executor. Phrase
// conclusions without the marker deliberately do not — a wrong skip silently
// drops the task, a missed one costs a single executor round.
func TestIsNoOpPlan(t *testing.T) {
	cases := []struct {
		name string
		plan string
		want bool
	}{
		{"empty", "", false},
		{"conclusion phrase without marker", "No changes are needed; the current implementation already handles this.", false},
		{"already implemented with follow-up work", "The auth flow is already implemented; extend it to cover refresh tokens.", false},
		{"mid-plan aside is not a conclusion", "Findings:\nThis part is already handled by the retry helper.\nConfirm the desired direction with the user.", false},
		{"explicit marker on final line", "The retry logic exists in client.go and the tests already run this path.\n[no_changes]", true},
		{"marker with surrounding whitespace", "Notes on the guard.\n  [no_changes]  ", true},
		{"marker mentioned before remaining work", "[no_changes] does not apply here.\nEdit main.go to add the missing guard.", false},
		{"final line mentions marker in prose", "The guard exists but the tests are missing.\nDo not emit [no_changes] because work remains.", false},
		{"marker with trailing prose on final line", "[no_changes] — but confirm the flag default first.", false},
		{"negated conclusion", "It is not already implemented.", false},
		{"no-op phrase with action verb", "No changes are needed in code, but run the test suite.", false},
		{"chinese conclusion without marker", "无需改动,当前逻辑已经覆盖该场景。", false},
		{"chinese follow-up work", "重试逻辑已经实现,但需要扩展覆盖刷新令牌。", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isNoOpPlan(tc.plan); got != tc.want {
				t.Errorf("isNoOpPlan(%q) = %v, want %v", tc.plan, got, tc.want)
			}
		})
	}
}

// TestDefaultPlannerPromptRequestsNoChangesMarker keeps the producer and parser
// of the no-op contract in sync: isNoOpPlan trusts the marker because the
// planner prompt asks for it.
func TestDefaultPlannerPromptRequestsNoChangesMarker(t *testing.T) {
	if !strings.Contains(DefaultPlannerPrompt, noChangesMarker) {
		t.Fatalf("DefaultPlannerPrompt does not request the %s marker isNoOpPlan parses", noChangesMarker)
	}
}

// TestCoordinatorDoesNotSkipExecutorForAlreadyImplementedPlanWithFollowUp is
// the motivating regression: a plan acknowledging existing code while asking
// for follow-up work must not be treated as a no-op conclusion.
func TestCoordinatorDoesNotSkipExecutorForAlreadyImplementedPlanWithFollowUp(t *testing.T) {
	planner := &mockProvider{name: "planner", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "The auth flow is already implemented; extend it to cover refresh tokens."},
		{Type: provider.ChunkDone},
	}}
	exec := &mockProvider{name: "executor", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "Done."},
		{Type: provider.ChunkDone},
	}}

	executor := New(exec, tool.NewRegistry(), NewSession("exec-sys"), Options{}, event.Discard)
	coord := NewCoordinator(planner, NewSession("planner-sys"), nil, nil, Options{}, executor, 0, event.Discard, nil)

	if err := coord.Run(context.Background(), "add refresh token support"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := len(exec.requests); got == 0 {
		t.Fatal("executor skipped: an already-implemented plan with follow-up work was treated as no-op")
	}
	if got := lastUser(exec.requests[0]); !strings.Contains(got, "extend it to cover refresh tokens") {
		t.Fatalf("executor handoff missing the plan: %q", got)
	}
}

// TestCoordinatorSkipsExecutorOnExplicitNoChangesMarker checks the marker
// contract end to end: research prose above the marker may mention runs/tests
// of existing code without vetoing the explicit conclusion.
func TestCoordinatorSkipsExecutorOnExplicitNoChangesMarker(t *testing.T) {
	planner := &mockProvider{name: "planner", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "The retry logic exists in client.go and the tests already run this path.\n[no_changes]"},
		{Type: provider.ChunkDone},
	}}
	exec := &mockProvider{name: "executor", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "Should not run."},
		{Type: provider.ChunkDone},
	}}

	executor := New(exec, tool.NewRegistry(), NewSession("exec-sys"), Options{}, event.Discard)
	coord := NewCoordinator(planner, NewSession("planner-sys"), nil, nil, Options{}, executor, 0, event.Discard, nil)

	if err := coord.Run(context.Background(), "check whether retries are covered"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := len(exec.requests); got != 0 {
		t.Fatalf("executor requests = %d, want skip on explicit [no_changes] marker", got)
	}
	messages := executor.session.Messages
	if got := len(messages); got != 3 {
		t.Fatalf("executor session messages = %d, want system + user + no-op assistant", got)
	}
	if got := messages[2].Content; !strings.Contains(got, "[no_changes]") {
		t.Fatalf("persisted executor assistant message = %q, want the planner conclusion", got)
	}
}

// TestCoordinatorFallsBackToExecutorWhenPlannerFails checks that a planner
// failure degrades the turn to executor-only instead of failing it: the
// executor gets the raw input (no handoff boilerplate), a warning notice is
// emitted, and the planner session is rolled back so the next plan does not
// start with consecutive user messages.
func TestCoordinatorFallsBackToExecutorWhenPlannerFails(t *testing.T) {
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

			if err := coord.Run(context.Background(), "fix the bug"); err != nil {
				t.Fatalf("Run should fall back to the executor, got: %v", err)
			}
			if got := len(exec.requests); got != 1 {
				t.Fatalf("executor requests = %d, want 1 fallback run", got)
			}
			got := lastUser(exec.requests[0])
			if got != "fix the bug" || strings.Contains(got, "You are the executor now") {
				t.Fatalf("fallback executor input = %q, want the raw task without handoff boilerplate", got)
			}
			if n := len(plannerSess.Messages); n != 1 {
				t.Fatalf("planner session messages = %d, want rollback to system only", n)
			}
			var warned bool
			for _, e := range events {
				if e.Kind == event.Notice && e.Level == event.LevelWarn && strings.Contains(e.Text, "Planner failed") {
					warned = true
				}
			}
			if !warned {
				t.Fatal("missing warn notice about the planner fallback")
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
// runs through planWithTools): when the tool-enabled planner fails, the
// executor fallback must not leave the planner session with a dangling user
// message or partial tool rounds — the next plan would otherwise start with
// consecutive user roles, which some providers reject.
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
			coord := NewCoordinator(tc.planner, plannerSess, nil, plannerReg, Options{}, executor, 0, event.Discard, nil)

			if err := coord.Run(context.Background(), "fix the bug"); err != nil {
				t.Fatalf("Run should fall back to the executor, got: %v", err)
			}
			if got := len(exec.requests); got != 1 {
				t.Fatalf("executor requests = %d, want 1 fallback run", got)
			}
			if n := len(plannerSess.Messages); n != 1 {
				t.Fatalf("planner session messages = %d, want rollback to system only", n)
			}
		})
	}
}

// TestCoordinatorKeepsPlannerSessionOnMaxStepsPause pins the rollback
// exemption: a max-steps pause is control flow whose saved work the user is
// asked to continue from, so planWithTools must not roll the planner session
// back to the pre-turn snapshot.
func TestCoordinatorKeepsPlannerSessionOnMaxStepsPause(t *testing.T) {
	planner := &mockProvider{name: "planner", chunks: []provider.Chunk{
		{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "call-1", Name: "read_file", Arguments: `{"path":"main.go"}`}},
		{Type: provider.ChunkDone},
	}}
	exec := &mockProvider{name: "executor"}
	plannerReg := tool.NewRegistry()
	plannerReg.Add(coordinatorTestTool{name: "read_file", readOnly: true, output: "package main"})

	executor := New(exec, tool.NewRegistry(), NewSession("exec-sys"), Options{}, event.Discard)
	plannerSess := NewSession("planner-sys")
	coord := NewCoordinator(planner, plannerSess, nil, plannerReg, Options{MaxSteps: 1}, executor, 0, event.Discard, nil)

	err := coord.Run(context.Background(), "fix the bug")
	if err == nil || !strings.Contains(err.Error(), "planner:") {
		t.Fatalf("Run = %v, want the propagated max-steps pause", err)
	}
	if got := len(exec.requests); got != 0 {
		t.Fatalf("executor requests = %d, want none on a paused planner turn", got)
	}
	if n := len(plannerSess.Messages); n <= 1 {
		t.Fatalf("planner session messages = %d, want the saved work preserved for continue", n)
	}
}

// TestCoordinatorRunsExecutorWhenMarkerNotAlone is the F2 regression: a final
// line that mentions [no_changes] in prose is not the no-op conclusion, so the
// executor must still run.
func TestCoordinatorRunsExecutorWhenMarkerNotAlone(t *testing.T) {
	planner := &mockProvider{name: "planner", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "The guard exists but the tests are missing.\nDo not emit [no_changes] because work remains."},
		{Type: provider.ChunkDone},
	}}
	exec := &mockProvider{name: "executor", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "Done."},
		{Type: provider.ChunkDone},
	}}

	executor := New(exec, tool.NewRegistry(), NewSession("exec-sys"), Options{}, event.Discard)
	coord := NewCoordinator(planner, NewSession("planner-sys"), nil, nil, Options{}, executor, 0, event.Discard, nil)

	if err := coord.Run(context.Background(), "add the missing tests"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := len(exec.requests); got == 0 {
		t.Fatal("executor skipped: a final line mentioning the marker in prose was treated as a no-op conclusion")
	}
}

// TestCoordinatorHandoffSurvivesPlannerCompaction pins the plan-scan boundary
// against session rewrites: when the tool-enabled planner's final answer pushes
// usage past the compaction trigger, Agent.Run rewrites and shortens the
// planner session right after producing the plan. The pre-turn message count
// then no longer bounds "this turn's messages" — scanning from it must not
// hide the plan, or Coordinator.Run degrades to a raw executor turn despite a
// successful plan.
func TestCoordinatorHandoffSurvivesPlannerCompaction(t *testing.T) {
	planner := &mockProvider{name: "planner", streams: [][]provider.Chunk{
		{ // the plan, with usage past the force-compaction watermark
			{Type: provider.ChunkText, Text: "Edit main.go and add the missing guard."},
			{Type: provider.ChunkUsage, Usage: &provider.Usage{PromptTokens: 1900, TotalTokens: 1950}},
			{Type: provider.ChunkDone},
		},
		{ // the compaction summarizer call
			{Type: provider.ChunkText, Text: "- goal: guard work\n- pending: none"},
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
	// Preset enough planner history that the fold shrinks the session to (or
	// below) its pre-turn length, which is what strands a boundary based on
	// the pre-turn message count.
	filler := strings.Repeat("planner history filler. ", 150)
	for i := 0; i < 3; i++ {
		plannerSess.Add(provider.Message{Role: provider.RoleUser, Content: filler})
		plannerSess.Add(provider.Message{Role: provider.RoleAssistant, Content: filler})
	}
	coord := NewCoordinator(planner, plannerSess, nil, plannerReg, Options{ContextWindow: 2000}, executor, 0, event.Discard, nil)

	if err := coord.Run(context.Background(), "fix the bug"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if plannerSess.RewriteVersion() == 0 {
		t.Fatal("test setup: planner compaction did not fire, the rewrite boundary is not exercised")
	}
	if got := len(exec.requests); got == 0 {
		t.Fatal("executor never ran")
	}
	got := lastUser(exec.requests[0])
	if !strings.Contains(got, "Edit main.go and add the missing guard.") || !strings.Contains(got, executorHandoffMarker) {
		t.Fatalf("executor input lost the plan handoff after planner compaction:\n%s", got)
	}
}

// TestCoordinatorNoOpConclusionAttributedToPlanner pins the event source on the
// relayed no-op conclusion: it is planner text and must not be attributed to
// the executor by sinks that key styling/usage off Source.
func TestCoordinatorNoOpConclusionAttributedToPlanner(t *testing.T) {
	planner := &mockProvider{name: "planner", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "The guard already exists in parser.go.\n[no_changes]"},
		{Type: provider.ChunkDone},
	}}
	exec := &mockProvider{name: "executor"}
	var events []event.Event
	sink := event.FuncSink(func(e event.Event) { events = append(events, e) })

	executor := New(exec, tool.NewRegistry(), NewSession("exec-sys"), Options{}, event.Discard)
	coord := NewCoordinator(planner, NewSession("planner-sys"), nil, nil, Options{}, executor, 0, sink, nil)

	if err := coord.Run(context.Background(), "check the parser guard"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	var conclusion *event.Event
	for i := range events {
		if events[i].Kind == event.Text && strings.Contains(events[i].Text, "[no_changes]") {
			conclusion = &events[i]
		}
	}
	if conclusion == nil {
		t.Fatal("no-op conclusion text event not emitted")
	}
	if conclusion.Source != event.UsageSourcePlanner {
		t.Fatalf("no-op conclusion Source = %q, want planner attribution", conclusion.Source)
	}
}

// TestCoordinatorHandoffOmitsToolContextWithoutMCPTools checks that the handoff
// does not restate the built-in tool schema: the tool-context block exists to
// counter planner claims about MCP availability and is dropped entirely when
// the executor carries no MCP tools.
func TestCoordinatorHandoffOmitsToolContextWithoutMCPTools(t *testing.T) {
	planner := &mockProvider{name: "planner", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "Edit main.go and add the missing guard."},
		{Type: provider.ChunkDone},
	}}
	exec := &mockProvider{name: "executor", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "Done."},
		{Type: provider.ChunkDone},
	}}

	execReg := tool.NewRegistry()
	execReg.Add(coordinatorTestTool{name: "write_file", readOnly: false, output: "ok"})
	executor := New(exec, execReg, NewSession("exec-sys"), Options{}, event.Discard)
	coord := NewCoordinator(planner, NewSession("planner-sys"), nil, nil, Options{}, executor, 0, event.Discard, nil)

	if err := coord.Run(context.Background(), "fix the missing guard"); err != nil {
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

// TestCoordinatorFailedTurnRollbackKeepsCompaction pins the rewrite-aware
// rollback economics: when auto-compaction fires mid-turn (after a tool round)
// and the planner THEN fails, restoring the pre-turn snapshot would revert the
// compaction — wasting its summarizer call and re-growing the prompt. The
// rollback must instead keep the compacted log and only drop trailing plain
// user messages, so the next plan still starts from the folded history without
// consecutive user roles.
func TestCoordinatorFailedTurnRollbackKeepsCompaction(t *testing.T) {
	planner := &mockProvider{name: "planner", streams: [][]provider.Chunk{
		{ // tool round whose usage crosses the force-compaction watermark
			{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "call-1", Name: "read_file", Arguments: `{"path":"main.go"}`}},
			{Type: provider.ChunkUsage, Usage: &provider.Usage{PromptTokens: 1900, TotalTokens: 1950}},
			{Type: provider.ChunkDone},
		},
		{ // the compaction summarizer call
			{Type: provider.ChunkText, Text: "- goal: guard work\n- pending: continue"},
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
	for i := 0; i < 3; i++ {
		plannerSess.Add(provider.Message{Role: provider.RoleUser, Content: filler})
		plannerSess.Add(provider.Message{Role: provider.RoleAssistant, Content: filler})
	}
	coord := NewCoordinator(planner, plannerSess, nil, plannerReg, Options{ContextWindow: 2000}, executor, 0, event.Discard, nil)

	if err := coord.Run(context.Background(), "fix the bug"); err != nil {
		t.Fatalf("Run should fall back to the executor, got: %v", err)
	}
	if got := len(exec.requests); got != 1 {
		t.Fatalf("executor requests = %d, want 1 fallback run", got)
	}
	if plannerSess.RewriteVersion() == 0 {
		t.Fatal("test setup: planner compaction did not fire, the rewrite-aware rollback is not exercised")
	}
	msgs := plannerSess.Snapshot()
	var hasSummary bool
	for _, m := range msgs {
		if isCompactionSummary(m) {
			hasSummary = true
		}
	}
	if !hasSummary {
		t.Fatal("rollback reverted the compaction: no compaction summary left in the planner session")
	}
	if last := msgs[len(msgs)-1]; last.Role == provider.RoleUser && !isCompactionSummary(last) {
		t.Fatalf("planner session ends in a plain user message after rollback: %q", last.Content)
	}
}

// TestCoordinatorPersistsDeniedPlanTurnToExecutorSession pins the denial
// bookkeeping: a plan the user declines must still land in the executor
// session (like the no-op path) so the turn survives save/reload, with a note
// telling the next executor turn that nothing ran, plus a user-facing notice.
func TestCoordinatorPersistsDeniedPlanTurnToExecutorSession(t *testing.T) {
	planner := &mockProvider{name: "planner", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "Plan: rewrite auth.\n[planner_requires_approval]"},
		{Type: provider.ChunkDone},
	}}
	exec := &mockProvider{name: "executor", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "should not run"},
		{Type: provider.ChunkDone},
	}}
	sink := &recordSink{}
	executor := New(exec, tool.NewRegistry(), NewSession("exec-sys"), Options{}, sink)
	coord := NewCoordinator(planner, NewSession("planner-sys"), nil, nil, Options{}, executor, 0, sink, nil)
	gate := &coordinatorApprovalGate{allow: false}
	coord.SetPlannerPlanApprover(gate)

	if err := coord.Run(context.Background(), "rewrite auth"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if gate.calls != 1 {
		t.Fatalf("approval gate calls = %d, want 1", gate.calls)
	}
	if len(exec.requests) != 0 {
		t.Fatal("executor must not run when the plan is denied")
	}
	msgs := executor.session.Messages
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

// TestCoordinatorPersistsUnansweredDecisionTurnToExecutorSession is the same
// contract for the ask path: a cancelled/unanswered planner question must not
// erase the turn from the persisted executor session.
func TestCoordinatorPersistsUnansweredDecisionTurnToExecutorSession(t *testing.T) {
	planner := &mockProvider{name: "planner", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "Plan draft.\n<planner-ask>\nquestion: Which database?\noption: sqlite\noption: postgres\n</planner-ask>"},
		{Type: provider.ChunkDone},
	}}
	exec := &mockProvider{name: "executor", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "should not run"},
		{Type: provider.ChunkDone},
	}}
	sink := &recordSink{}
	executor := New(exec, tool.NewRegistry(), NewSession("exec-sys"), Options{}, sink)
	coord := NewCoordinator(planner, NewSession("planner-sys"), nil, nil, Options{}, executor, 0, sink, nil)
	gate := &coordinatorDecisionGate{answer: ""}
	coord.SetPlannerUserDecisionAsker(gate)

	if err := coord.Run(context.Background(), "set up storage"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if gate.calls != 1 {
		t.Fatalf("decision gate calls = %d, want 1", gate.calls)
	}
	if len(exec.requests) != 0 {
		t.Fatal("executor must not run without a user answer")
	}
	msgs := executor.session.Messages
	if len(msgs) < 2 {
		t.Fatalf("executor session messages = %d, want the unanswered turn persisted", len(msgs))
	}
	last := msgs[len(msgs)-1]
	if last.Role != provider.RoleAssistant || !strings.Contains(last.Content, plannerDecisionUnansweredNote) {
		t.Fatalf("last executor message = %q, want plan with unanswered-decision note", last.Content)
	}
}

// TestCoordinatorSkipsApprovalGateForNegatedApprovalWording pins the negation
// veto: a plan that explicitly rules out an approval round must hand off
// directly instead of raising a needless approval prompt.
func TestCoordinatorSkipsApprovalGateForNegatedApprovalWording(t *testing.T) {
	planner := &mockProvider{name: "planner", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "Plan:\n1. 修改 config.go\n2. 无需等待用户批准，直接执行修改"},
		{Type: provider.ChunkDone},
	}}
	exec := &mockProvider{name: "executor", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "Done."},
		{Type: provider.ChunkDone},
	}}
	executor := New(exec, tool.NewRegistry(), NewSession("exec-sys"), Options{}, event.Discard)
	coord := NewCoordinator(planner, NewSession("planner-sys"), nil, nil, Options{}, executor, 0, event.Discard, nil)
	gate := &coordinatorApprovalGate{allow: false}
	coord.SetPlannerPlanApprover(gate)

	if err := coord.Run(context.Background(), "tweak config"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if gate.calls != 0 {
		t.Fatalf("approval gate calls = %d, want 0 for negated approval wording", gate.calls)
	}
	if len(exec.requests) == 0 {
		t.Fatal("executor should run directly for negated approval wording")
	}
}

// TestCoordinatorDoesNotAskForTargetConfirmationWording pins the pruned
// decision phrases: ordinary verification wording such as "确认目标行为不变"
// must not conjure an ask dialog.
func TestCoordinatorDoesNotAskForTargetConfirmationWording(t *testing.T) {
	planner := &mockProvider{name: "planner", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "Plan:\n1. 修改 handler.go\n2. 运行测试确认目标行为不变\n3. 更新用户选择器组件"},
		{Type: provider.ChunkDone},
	}}
	exec := &mockProvider{name: "executor", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "Done."},
		{Type: provider.ChunkDone},
	}}
	executor := New(exec, tool.NewRegistry(), NewSession("exec-sys"), Options{}, event.Discard)
	coord := NewCoordinator(planner, NewSession("planner-sys"), nil, nil, Options{}, executor, 0, event.Discard, nil)
	gate := &coordinatorDecisionGate{answer: "should not be used"}
	coord.SetPlannerUserDecisionAsker(gate)

	if err := coord.Run(context.Background(), "refactor handler"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if gate.calls != 0 {
		t.Fatalf("decision gate calls = %d, want 0 for ordinary verification wording", gate.calls)
	}
	if len(exec.requests) == 0 {
		t.Fatal("executor should run for ordinary plan wording")
	}
}
