package control

import (
	"context"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// wanderingChatProvider never stops on its own: every round reads one more
// path, so nothing repeats and the adaptive guards stay silent. It is the
// reported runaway's shape, driven through the controller rather than the agent.
type wanderingChatProvider struct{ calls atomic.Int32 }

func (p *wanderingChatProvider) Name() string { return "wandering" }

func (p *wanderingChatProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	round := p.calls.Add(1)
	ch := make(chan provider.Chunk, 4)
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

// An ordinary chat turn had no Run ceiling: [agent].max_steps was retired and
// normalized to zero, and the adaptive guards escalate on repetition, so a loop
// that keeps reading something new ran until the user noticed. The backstop
// bounds it without truncating the work — the turn ends with one tool-free
// summary and stays resumable.
func TestOrdinaryChatTurnStopsAtTheRunBackstop(t *testing.T) {
	dir := t.TempDir()
	prov := &wanderingChatProvider{}
	reg := tool.NewRegistry()
	reg.Add(fakeControlTool{name: "read_file"})
	exec := agent.New(prov, reg, agent.NewSession("sys"), agent.Options{}, event.Discard)
	sink, done, _ := collectSink()
	c := New(Options{
		Runner:      exec,
		Executor:    exec,
		Sink:        sink,
		SessionDir:  dir,
		SessionPath: filepath.Join(dir, "s.jsonl"),
	})
	defer c.autosaveWG.Wait()

	c.Submit("collect the real state, then rewrite HANDOVER.md")
	waitForDone(t, done)

	// One committed sample per bounded round, then the single tool-free
	// finalization the limit allows.
	if got, want := prov.calls.Load(), int32(chatRunRoundLimit+1); got != want {
		t.Fatalf("provider rounds = %d, want %d (%d bounded rounds plus one summary)", got, want, chatRunRoundLimit)
	}
}

// The backstop must not touch a turn the user bounded explicitly.
func TestExplicitMaxStepsOwnsTheOrdinaryTurn(t *testing.T) {
	dir := t.TempDir()
	prov := &wanderingChatProvider{}
	reg := tool.NewRegistry()
	reg.Add(fakeControlTool{name: "read_file"})
	exec := agent.New(prov, reg, agent.NewSession("sys"), agent.Options{MaxSteps: 3}, event.Discard)
	sink, done, _ := collectSink()
	c := New(Options{
		Runner:      exec,
		Executor:    exec,
		Sink:        sink,
		SessionDir:  dir,
		SessionPath: filepath.Join(dir, "s.jsonl"),
	})
	defer c.autosaveWG.Wait()

	c.Submit("collect the real state")
	waitForDone(t, done)

	if got := prov.calls.Load(); got != 4 {
		t.Fatalf("provider rounds = %d, want the explicit 3 plus one summary", got)
	}
}
