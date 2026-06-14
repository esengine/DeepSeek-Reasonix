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
	coord := NewCoordinator(planner, plannerSess, nil, nil, Options{}, executor, 0, event.Discard, func(string) bool { return false })

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
	if got := len(planner.requests); got != 2 {
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
