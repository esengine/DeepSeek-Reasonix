package agent

import (
	"context"
	"reasonix/internal/event"
	"strings"
	"testing"

	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// mockProvider replays preset chunks and records the last request it received.
type mockProvider struct {
	name    string
	chunks  []provider.Chunk
	lastReq provider.Request
}

func (m *mockProvider) Name() string { return m.name }

func (m *mockProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	m.lastReq = req
	ch := make(chan provider.Chunk, len(m.chunks))
	for _, c := range m.chunks {
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
	coord := NewCoordinator(planner, plannerSess, nil, executor, 0, event.Discard, nil)

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
	coord := NewCoordinator(planner, plannerSess, nil, executor, 0, event.Discard, func(string) bool { return false })

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
