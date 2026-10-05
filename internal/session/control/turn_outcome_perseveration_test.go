package control

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/state/sessionstore"
)

// perseverationLoopProvider streams one byte-identical block every round, which
// the guard reads as a degenerate loop.
type perseverationLoopProvider struct{ requests int }

func (p *perseverationLoopProvider) Name() string { return "control-perseveration" }

func (p *perseverationLoopProvider) Stream(_ context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	p.requests++
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: strings.Repeat("Let me write.\n\nHmm.\n\nOK.\n\n", 400)}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

// A perseveration pause ends the turn as no_progress, the same outcome a
// progress-watch pause carries, so a frontend keeps the stall strip and shows
// the paused note instead of a failure card.
func TestTurnOutcomeClassifiesPerseverationPause(t *testing.T) {
	dir := testenv.TempDir(t)
	prov := &perseverationLoopProvider{}
	sess := sessionstore.NewSession("sys")
	zero := 0
	exec := agent.New(prov, tool.NewRegistry(), sess,
		agent.Options{MaxPerseverationRetries: &zero}, event.Discard)
	exec.SetProgressWatch(agent.ProgressWatch{Pause: true, Rounds: 1000, TokenMultiple: 1000})
	sink, done, _ := collectSink()
	c := New(Options{
		Runner:      exec,
		Executor:    exec,
		Sink:        sink,
		SessionDir:  dir,
		SessionPath: filepath.Join(dir, "s.jsonl"),
	})
	t.Cleanup(func() { c.autosaveWG.Wait() })

	c.Submit("write the file")
	finished := waitForDone(t, done)
	if finished.Outcome != event.TurnOutcomeNoProgress {
		t.Fatalf("TurnDone.Outcome = %q, want %q", finished.Outcome, event.TurnOutcomeNoProgress)
	}
	if info, ok := agent.InspectRunPause(finished.Err); !ok || info.Kind != agent.PauseKindPerseveration {
		t.Fatalf("TurnDone.Err = %v (pause %+v, %v), want a perseveration pause", finished.Err, info, ok)
	}
}
