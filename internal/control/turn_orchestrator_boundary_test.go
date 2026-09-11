package control

import (
	"context"
	"path/filepath"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/event"
)

func TestTurnOrchestratorCheckpointBoundaryPrecedesUserMessage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	sess := agent.NewSession("sys")
	exec := agent.New(nil, nil, sess, agent.Options{}, event.Discard)
	runner := &recordingSessionRunner{session: sess}
	c := New(Options{
		Runner:      runner,
		Executor:    exec,
		SessionDir:  dir,
		SessionPath: path,
		Label:       "test",
	})

	o := newTurnOrchestrator(c)
	if err := o.runTurnWithRawDisplay(context.Background(), "write the test", "write the test", ""); err != nil {
		t.Fatal(err)
	}

	if !c.CheckpointHasBoundary(0) {
		t.Fatal("checkpoint boundary should be available for the orchestrated turn")
	}
	if len(sess.Messages) != 2 || sess.Messages[1].Content != "write the test" {
		t.Fatalf("session messages after turn = %+v, want system + user", sess.Messages)
	}
	loaded, err := agent.LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Messages) != 2 {
		t.Fatalf("saved messages = %d, want system + user", len(loaded.Messages))
	}
	meta, ok, err := agent.LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("load branch meta ok=%v err=%v", ok, err)
	}
	if meta.UpdatedAt.IsZero() {
		t.Fatal("activity meta should be marked after transcript changes")
	}
	if err := c.Rewind(0, RewindConversation); err != nil {
		t.Fatal(err)
	}
	// A schema-2 rewind moves the live session onto a new head of the same
	// log; the boundary precedes the user message, so one message remains,
	// and the parent chain survives on the main head.
	live := exec.Session()
	if live == nil || len(live.Messages) != 1 {
		t.Fatalf("rewind did not truncate the live transcript: %+v", live)
	}
	if ref, ok := live.Head(); !ok || ref.HeadID == agent.SessionMainHead || c.SessionPath() != path {
		t.Fatalf("rewind must move to a new head of the same log: head=%+v ok=%v path=%q", ref, ok, c.SessionPath())
	}
	heads, err := agent.ListSessionHeads(path)
	if err != nil || len(heads) != 2 || heads[0].MessageCount != 2 {
		t.Fatalf("parent chain must survive on the main head: %+v err=%v", heads, err)
	}
}
