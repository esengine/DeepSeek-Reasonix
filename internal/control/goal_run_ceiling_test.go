package control

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// workingGoalTool is a writer, so every round counts as acting on the
// investigation rather than only looking at it.
type workingGoalTool struct{ name string }

func (t workingGoalTool) Name() string      { return t.name }
func (workingGoalTool) Description() string { return "apply an edit" }
func (workingGoalTool) ReadOnly() bool      { return false }
func (workingGoalTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`)
}
func (workingGoalTool) Execute(context.Context, json.RawMessage) (string, error) {
	return "applied", nil
}

// steadyWorkProvider makes real progress every round: a distinct mutation, so
// neither the zero-evidence ladder nor the exploration decay ever escalates.
type steadyWorkProvider struct {
	calls atomic.Int32
	max   int32
}

func (p *steadyWorkProvider) Name() string { return "steady-work" }

func (p *steadyWorkProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	round := p.calls.Add(1)
	ch := make(chan provider.Chunk, 4)
	if round > p.max {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "All done."}
		ch <- provider.Chunk{Type: provider.ChunkDone}
		close(ch)
		return ch, nil
	}
	ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{
		ID:        fmt.Sprintf("edit-%d", round),
		Name:      "apply_edit",
		Arguments: fmt.Sprintf(`{"path":"pkg%d/file.go"}`, round),
	}}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

// A Goal turn used to be cut at 16 model rounds. Rounds were the wrong unit
// there for the same reason they were wrong for chat: a turn that reaches a
// high count while still producing new work is the case least worth
// interrupting. Goal now ends on its own terms — completion, a block, or a
// structural no-progress loop — none of which is a round count.
func TestGoalTurnRunsPastTheOldRoundCeiling(t *testing.T) {
	prov := &steadyWorkProvider{max: 24}
	reg := tool.NewRegistry()
	reg.Add(workingGoalTool{name: "apply_edit"})
	exec := agent.New(prov, reg, agent.NewSession("sys"), agent.Options{}, event.Discard)
	c, done := newChatBudgetController(t, exec)

	c.SetGoal("apply every pending edit")
	c.Submit("start")
	waitForDone(t, done)

	if got := prov.calls.Load(); got <= 16 {
		t.Fatalf("provider rounds = %d, want productive work to run past the retired 16-round ceiling", got)
	}
}
