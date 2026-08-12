package control

import (
	"context"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// wanderingChatProvider never repeats itself: every round reads one more path,
// so the adaptive guards stay silent. It is the reported runaway's shape,
// driven through the controller rather than the agent. With max > 0 it stops
// on its own after that many rounds.
type wanderingChatProvider struct {
	calls atomic.Int32
	max   int32
}

func (p *wanderingChatProvider) Name() string { return "wandering" }

func (p *wanderingChatProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	round := p.calls.Add(1)
	ch := make(chan provider.Chunk, 4)
	if p.max > 0 && round > p.max {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "Done."}
		ch <- provider.Chunk{Type: provider.ChunkDone}
		close(ch)
		return ch, nil
	}
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "收到。先收集当前真实状态。"}
	ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{
		ID:        fmt.Sprintf("call-%d", round),
		Name:      "read_file",
		Arguments: fmt.Sprintf(`{"path":"internal/pkg%d/file.go"}`, round),
	}}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func newChatBudgetController(t *testing.T, exec *agent.Agent) (*Controller, chan event.Event) {
	t.Helper()
	dir := t.TempDir()
	sink, done, _ := collectSink()
	c := New(Options{
		Runner:      exec,
		Executor:    exec,
		Sink:        sink,
		SessionDir:  dir,
		SessionPath: filepath.Join(dir, "s.jsonl"),
	})
	t.Cleanup(c.autosaveWG.Wait)
	return c, done
}

// An ordinary chat turn has no round ceiling. Reaching a high round count
// without crossing the cost or wall-clock budget means the rounds are
// individually cheap and fast, which is the case least worth stopping — 120
// rounds would have been truncated by the ceiling this replaced.
func TestOrdinaryChatTurnRunsPastTheOldRoundCeiling(t *testing.T) {
	prov := &wanderingChatProvider{max: 120}
	reg := tool.NewRegistry()
	reg.Add(fakeControlTool{name: "read_file"})
	exec := agent.New(prov, reg, agent.NewSession("sys"), agent.Options{}, event.Discard)
	c, done := newChatBudgetController(t, exec)

	c.Submit("collect the real state, then rewrite HANDOVER.md")
	waitForDone(t, done)

	if got, want := prov.calls.Load(), int32(121); got != want {
		t.Fatalf("provider rounds = %d, want %d (the model's own 120 plus its final answer)", got, want)
	}
}

// What actually bounds it: the wall-clock axis, which catches even a runaway
// whose rounds are free — the one case a cost budget cannot see.
func TestOrdinaryChatTurnStopsOnItsTimeBudget(t *testing.T) {
	prov := &wanderingChatProvider{}
	reg := tool.NewRegistry()
	reg.Add(fakeControlTool{name: "read_file"})
	exec := agent.New(prov, reg, agent.NewSession("sys"),
		agent.Options{TaskBudget: agent.TaskBudget{Wall: time.Millisecond}}, event.Discard)
	c, done := newChatBudgetController(t, exec)

	c.Submit("collect the real state, then rewrite HANDOVER.md")
	waitForDone(t, done)

	// An unbounded provider: without the spend gate this never returns.
	if got := prov.calls.Load(); got > 20 {
		t.Fatalf("provider rounds = %d, want the wall-clock budget to land it promptly", got)
	}
}

// The gate must not touch a turn the user bounded explicitly.
func TestExplicitMaxStepsOwnsTheOrdinaryTurn(t *testing.T) {
	prov := &wanderingChatProvider{}
	reg := tool.NewRegistry()
	reg.Add(fakeControlTool{name: "read_file"})
	exec := agent.New(prov, reg, agent.NewSession("sys"), agent.Options{MaxSteps: 3}, event.Discard)
	c, done := newChatBudgetController(t, exec)

	c.Submit("collect the real state")
	waitForDone(t, done)

	if got := prov.calls.Load(); got != 4 {
		t.Fatalf("provider rounds = %d, want the explicit 3 plus one summary", got)
	}
}
