package control

import (
	"context"
	"path/filepath"
	"reasonix/internal/state/sessionstore"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
)

type turnIdentityProvider struct{}

func (p *turnIdentityProvider) Name() string { return "turn-identity" }

func (p *turnIdentityProvider) Stream(_ context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "answered"}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

// TestCheckpointTurnIsNotTheAuthoredTurn runs one real turn and reads both
// numbers off it. They are two namespaces over the same turn: the conversation
// counts authored turns from 1, the checkpoint store counts snapshots from 0.
// An implementation that took one from the other reads identical here and wrong
// everywhere a turn opens no snapshot — or a snapshot opens no turn.
func TestCheckpointTurnIsNotTheAuthoredTurn(t *testing.T) {
	dir := testenv.TempDir(t)
	sess := sessionstore.NewSession("sys")
	exec := agent.New(&turnIdentityProvider{}, tool.NewRegistry(), sess, agent.Options{}, event.Discard)
	sink, done, events := collectSink()
	c := New(Options{
		Runner:      exec,
		Executor:    exec,
		Sink:        sink,
		SessionDir:  dir,
		SessionPath: filepath.Join(dir, "s.jsonl"),
	})
	defer c.Close()
	defer c.autosaveWG.Wait()

	c.Submit("first prompt")
	turnDone := waitForDone(t, done)

	var started *event.Event
	for _, e := range events() {
		if e.Kind == event.TurnStarted && e.AuthoredTurn != nil {
			started = &e
			break
		}
	}
	if started == nil {
		t.Fatal("no turn_started named the message its turn was about")
	}
	if *started.AuthoredTurn != 1 {
		t.Fatalf("first authored turn = %d, want 1", *started.AuthoredTurn)
	}
	if turnDone.CheckpointTurn == nil {
		t.Fatal("TurnDone carried no checkpoint turn; this test needs both numbers")
	}
	if *turnDone.CheckpointTurn != 0 {
		t.Fatalf("first checkpoint turn = %d, want 0", *turnDone.CheckpointTurn)
	}
	// The message index is the only thing the two share, and it belongs to
	// neither counter: it names the message both are about.
	if started.MsgIndex == nil {
		t.Fatal("turn_started carried no message index")
	}
	boundary, ok := c.checkpoints.boundary(*turnDone.CheckpointTurn)
	if !ok || boundary != *started.MsgIndex {
		t.Fatalf("checkpoint boundary = %d (ok=%v), turn_started named %d", boundary, ok, *started.MsgIndex)
	}
}
