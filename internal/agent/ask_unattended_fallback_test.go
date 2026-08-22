package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

type unattendedAskerProvider struct {
	streams int
}

// Stream never runs: the fixture drives the ask tool directly. Kept to satisfy
// provider.Provider.
func (p *unattendedAskerProvider) Name() string { return "unattended-asker-host" }
func (p *unattendedAskerProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "ok"}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

type failingAsker struct{ err error }

func (a failingAsker) Ask(_ context.Context, _ []event.AskQuestion) ([]event.AskAnswer, error) {
	return nil, a.err
}

func withAsker(ctx context.Context, asker Asker) context.Context {
	return WithToolCallContext(ctx, "test-parent", nil, asker, false)
}

// An ErrAskUnattended resolution must degrade to the model-assumption fallback
// — the same text the headless run produces — so unattended ACP runs keep
// going instead of the turn dying (#8238).
func TestAskToolFallsBackWhenHostIsUnattended(t *testing.T) {
	askTool := NewAskTool()

	out, err := askTool.Execute(withAsker(context.Background(), failingAsker{err: ErrAskUnattended}),
		[]byte(`{"questions":[{"question":"How should the autopilot proceed?","options":[{"label":"A"},{"label":"B"}]}]}`))
	if err != nil {
		t.Fatalf("unattended ask must not fail the tool: %v", err)
	}
	if !strings.Contains(out, "No interactive user answered") || !strings.Contains(out, "model-assumption fallback") {
		t.Fatalf("unexpected fallback text: %q", out)
	}
}

// Every other ask error stays an error — only the unattended sentinel degrades.
func TestAskToolKeepsOrdinaryAskErrors(t *testing.T) {
	askTool := NewAskTool()

	if _, err := askTool.Execute(withAsker(context.Background(), failingAsker{err: errors.New("host exploded")}),
		[]byte(`{"questions":[{"question":"q","options":[{"label":"A"}]}]}`)); err == nil {
		t.Fatal("ordinary ask error must surface")
	}
}
