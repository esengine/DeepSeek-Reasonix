package control

import (
	"path/filepath"
	"slices"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// TestTurnDoneStampsHeadReferenceForSchemaTwo pins that the terminal turn
// envelope names the head and leaf message the turn ended on, so projection
// acks and orphan repair can identify the transcript position without a
// revision/digest pair.
func TestTurnDoneStampsHeadReferenceForSchemaTwo(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	reply := [][]provider.Chunk{{{Type: provider.ChunkText, Text: "ok"}, {Type: provider.ChunkDone}}}
	exec := agent.New(&recordingProvider{streams: reply}, tool.NewRegistry(), agent.NewSession("SYS"), agent.Options{}, event.Discard)
	done := make(chan event.Event, 4)
	c := New(Options{Runner: exec, Executor: exec, SystemPrompt: "SYS", SessionDir: dir, SessionPath: path, Label: "test",
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.TurnDone {
				done <- e
			}
		})})
	t.Cleanup(c.Close)
	// The first turn's save creates the schema-2 log; the second turn ends on it.
	for _, input := range []string{"first", "second"} {
		c.Submit(input)
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("turn %q did not finish", input)
		}
	}
	records, err := c.TurnEventsAfter(0)
	if err != nil {
		t.Fatalf("TurnEventsAfter: %v", err)
	}
	var terminal int
	for i, record := range slices.Backward(records) {
		if record.Kind == "turn_done" {
			terminal = i
			break
		}
	}
	ref, ok := exec.Session().Head()
	if !ok {
		t.Fatal("session must be schema 2 after two saved turns")
	}
	got := records[terminal]
	if got.HeadID != ref.HeadID || got.LeafMessageID == "" || got.LeafMessageID != exec.Session().LeafID() {
		t.Fatalf("terminal envelope head=%q leaf=%q, want head %q leaf %q", got.HeadID, got.LeafMessageID, ref.HeadID, exec.Session().LeafID())
	}
	view, err := c.TurnEventReplay(0)
	if err != nil || view.HeadID != ref.HeadID || view.LeafMessageID != got.LeafMessageID {
		t.Fatalf("replay view = %+v err=%v", view, err)
	}
}
