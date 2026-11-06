package agent

import (
	"context"
	"errors"
	"path/filepath"
	"reasonix/internal/state/sessionstore"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
)

// appendDuringSummary appends one user turn to sess while answering the summary
// request — a host append racing a fold, which is what the stale-context branch
// in foldContext exists for.
type appendDuringSummary struct {
	calls int
	sess  *sessionstore.Session
}

func (p *appendDuringSummary) Name() string { return "append-during-summary" }

func (p *appendDuringSummary) Stream(_ context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	p.calls++
	if p.calls == 1 && p.sess != nil {
		p.sess.Add(provider.Message{Role: provider.RoleUser, Content: "steer: retry the failing step"})
	}
	ch := make(chan provider.Chunk, 3)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "## Files & code\n- internal/parser/lexer.go was rewritten"}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func latchTranscript() []provider.Message {
	return []provider.Message{
		{Role: provider.RoleSystem, Content: "system"},
		{Role: provider.RoleUser, Content: "task"},
		{Role: provider.RoleAssistant, Content: strings.Repeat("old work ", 400)},
		{Role: provider.RoleUser, Content: "current"},
		{Role: provider.RoleAssistant, Content: "tail"},
	}
}

// And the latch is not merely a lost optimisation: at the physical ceiling it
// fails the turn, quoting a retry that never comes.
func TestStaleContextBlockFailsTheNextOverflowTurn(t *testing.T) {
	const window = 10_000
	path := filepath.Join(testenv.TempDir(t), "session.jsonl")
	sess := &sessionstore.Session{Messages: latchTranscript()}
	prov := &appendDuringSummary{sess: sess}
	a := New(prov, tool.NewRegistry(), sess, Options{
		ContextWindow: window, CompactRatio: 0.85, WorkspaceID: "workspace", ModelRef: "model",
	}, event.Discard)
	a.BindSessionPath(path, true)
	if _, err := a.window().contextManager().Prepare(context.Background(), ContextPreparePolicy{
		Trigger: CompactionTriggerPressure, ObservedInputTokens: 8700,
	}); err != nil {
		t.Fatalf("first prepare: %v", err)
	}

	_, err := a.window().contextManager().Prepare(context.Background(), ContextPreparePolicy{
		Trigger: CompactionTriggerOverflow, ObservedInputTokens: 9900,
	})
	if errors.Is(err, ErrCompactionRequired) {
		t.Fatalf("an overflow one turn after a raced fold is refused instead of folded: %v (summary calls = %d)", err, prov.calls)
	}
}
