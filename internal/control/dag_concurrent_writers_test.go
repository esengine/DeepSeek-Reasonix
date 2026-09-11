package control

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/store"
	"reasonix/internal/tool"
)

// TestConcurrentControllersShareOneLogWithoutRecoveryCopies is the
// controller-level contract for the schema-2 log: two runtimes on one
// conversation each keep their own head in the same log, nothing is lost,
// and no recovery copy is ever created.
func TestConcurrentControllersShareOneLogWithoutRecoveryCopies(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "shared.jsonl")
	const systemPrompt = "SYS"
	reply := [][]provider.Chunk{{{Type: provider.ChunkText, Text: "ok"}, {Type: provider.ChunkDone}}}

	provA := &recordingProvider{streams: reply}
	execA := agent.New(provA, tool.NewRegistry(), agent.NewSession(systemPrompt), agent.Options{}, event.Discard)
	ctrlA := New(Options{Runner: execA, Executor: execA, SystemPrompt: systemPrompt, SessionDir: dir, SessionPath: path, Label: "a", Sink: event.Discard})
	if err := ctrlA.RunTurn(context.Background(), "first from A"); err != nil {
		t.Fatalf("A first turn: %v", err)
	}
	if err := ctrlA.Snapshot(); err != nil {
		t.Fatalf("A snapshot: %v", err)
	}

	loaded, err := agent.LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	provB := &recordingProvider{streams: reply}
	execB := agent.New(provB, tool.NewRegistry(), agent.NewSession(systemPrompt), agent.Options{}, event.Discard)
	ctrlB := New(Options{Runner: execB, Executor: execB, SystemPrompt: systemPrompt, SessionDir: dir, SessionPath: path, Label: "b", Sink: event.Discard})
	ctrlB.Resume(loaded, path)

	if err := ctrlA.RunTurn(context.Background(), "second from A"); err != nil {
		t.Fatalf("A second turn: %v", err)
	}
	if err := ctrlB.RunTurn(context.Background(), "second from B"); err != nil {
		t.Fatalf("B turn: %v", err)
	}
	if err := ctrlA.Snapshot(); err != nil {
		t.Fatalf("A snapshot: %v", err)
	}
	if err := ctrlB.Snapshot(); err != nil {
		t.Fatalf("B snapshot: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if store.IsSessionTranscriptName(entry.Name()) && entry.Name() != "shared.jsonl" {
			t.Fatalf("concurrent controllers created a transcript copy: %s", entry.Name())
		}
	}
	heads, err := agent.ListSessionHeads(path)
	if err != nil {
		t.Fatalf("ListSessionHeads: %v", err)
	}
	if len(heads) != 2 {
		t.Fatalf("heads = %+v, want main plus one concurrent head", heads)
	}
	for _, h := range heads {
		if h.MessageCount != 5 {
			t.Fatalf("head %s has %d messages, want system + two exchanges", h.ID, h.MessageCount)
		}
	}
	if heads[1].Kind != agent.HeadKindConcurrent {
		t.Fatalf("second head kind = %q", heads[1].Kind)
	}
	if ctrlA.SessionPath() != path || ctrlB.SessionPath() != path {
		t.Fatalf("controllers moved off the shared path: %q %q", ctrlA.SessionPath(), ctrlB.SessionPath())
	}
	if len(ctrlB.History()) != 5 || len(ctrlA.History()) != 5 {
		t.Fatalf("histories A=%d B=%d", len(ctrlA.History()), len(ctrlB.History()))
	}
}
