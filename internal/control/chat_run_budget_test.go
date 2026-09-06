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
	calls    atomic.Int32
	max      int32
	progress chan struct{}
}

func (p *wanderingChatProvider) Name() string { return "wandering" }

func (p *wanderingChatProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	round := p.calls.Add(1)
	if p.progress != nil {
		select {
		case p.progress <- struct{}{}:
		default:
		}
	}
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
	path := filepath.Join(dir, "s.jsonl")
	// Match production's live writer authority so durability checks use tail
	// CAS rather than repeatedly decoding a legacy, unleased transcript.
	writer, err := agent.AcquireSessionWriter(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(writer.Release)
	if err := writer.Bind(exec.Session(), agent.NextSessionWriteGeneration()); err != nil {
		t.Fatal(err)
	}
	sink, done, _ := collectSink()
	c := New(Options{
		Runner:      exec,
		Executor:    exec,
		Sink:        sink,
		SessionDir:  dir,
		SessionPath: path,
	})
	t.Cleanup(c.autosaveWG.Wait)
	return c, done
}

// An ordinary chat turn has no round ceiling. Reaching a high round count
// without crossing the cost or wall-clock budget means the rounds are
// individually cheap and fast, which is the case least worth stopping — 120
// rounds would have been truncated by the ceiling this replaced.
func TestOrdinaryChatTurnRunsPastTheOldRoundCeiling(t *testing.T) {
	prov := &wanderingChatProvider{max: 120, progress: make(chan struct{}, 1)}
	reg := tool.NewRegistry()
	reg.Add(fakeControlTool{name: "read_file"})
	exec := agent.New(prov, reg, agent.NewSession("sys"), agent.Options{}, event.Discard)
	c, done := newChatBudgetController(t, exec)

	c.Submit("collect the real state, then rewrite HANDOVER.md")
	waitForChatBudgetProgress(t, done, prov.progress)

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

	// Instant mock rounds can fit more than a handful into 1ms; the gate must
	// still stop a runaway well short of an unbounded loop.
	if got := prov.calls.Load(); got > 50 {
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

// This is a round-ceiling regression, not a disk-throughput benchmark. Keep a
// five-second no-progress watchdog while bounding the entire 121-round run.
func waitForChatBudgetProgress(t *testing.T, done <-chan event.Event, progress <-chan struct{}) {
	t.Helper()
	idle := time.NewTimer(5 * time.Second)
	defer idle.Stop()
	total := time.NewTimer(30 * time.Second)
	defer total.Stop()
	for {
		select {
		case <-done:
			return
		case <-progress:
			idle.Reset(5 * time.Second)
		case <-idle.C:
			t.Fatal("chat turn made no progress for five seconds")
		case <-total.C:
			t.Fatal("121-round chat turn exceeded its total test deadline")
		}
	}
}
