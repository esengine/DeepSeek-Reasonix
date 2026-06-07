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
	if got := lastUser(exec.lastReq); !strings.Contains(got, "read main.go") || !strings.Contains(got, "fix the bug") {
		t.Errorf("executor saw user %q, want task + plan", got)
	}
	// planner session must accumulate (system, user, assistant-plan) so its
	// prefix grows prepend-only and stays cache-stable.
	if n := len(plannerSess.Messages); n != 3 {
		t.Errorf("planner session has %d messages, want 3", n)
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
	if got := lastUser(exec.lastReq); !strings.Contains(got, "follow the loaded rule") || !strings.Contains(got, "fix the bug") {
		t.Errorf("executor saw user %q, want task + planner plan", got)
	}
	if got := plannerSess.Messages[0].Content; !strings.Contains(got, "Rule: keep changes narrow.") {
		t.Errorf("planner system prompt missing planning context: %q", got)
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
