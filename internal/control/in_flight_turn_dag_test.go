package control

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/store"
	"reasonix/internal/tool"
)

func dagLogEntryTypes(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(store.SessionEventLog(path))
	if err != nil {
		t.Fatal(err)
	}
	var types []string
	for line := range strings.SplitSeq(strings.TrimSpace(string(b)), "\n") {
		_, rest, _ := strings.Cut(line, `"type":"`)
		typ, _, _ := strings.Cut(rest, `"`)
		types = append(types, typ)
	}
	return types
}

// TestInterruptedTurnRecoveryUsesLogMarkersForSchemaTwo pins the schema-2
// crash contract: a turn left open in the log is closed on resume by
// dropping its tail through a rewind marker, keeping the user prompt, and
// the earlier bytes are never truncated.
func TestInterruptedTurnRecoveryUsesLogMarkersForSchemaTwo(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	sess := agent.NewSession("sys")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	exec := agent.New(nil, nil, sess, agent.Options{}, event.Discard)
	c := New(Options{Executor: exec, SessionDir: dir, SessionPath: path, Label: "test", Sink: event.Discard})
	if err := c.Snapshot(); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if _, ok := sess.Head(); !ok {
		t.Fatal("session must be schema 2 after its first save")
	}
	start := sess.Len()
	marker := c.markInFlightTurn(start, true)
	if marker.ID == "" || marker.HeadID != agent.SessionMainHead {
		t.Fatalf("marker = %+v, want a log-backed marker", marker)
	}
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "second"})
	sess.Add(provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "c1", Name: "bash", Arguments: `{}`}}})
	if err := c.Snapshot(); err != nil {
		t.Fatalf("mid-turn snapshot: %v", err)
	}
	if meta, ok, _ := agent.LoadBranchMeta(path); ok && meta.InFlightTurn != nil {
		t.Fatal("schema-2 turns must not write the sidecar marker")
	}
	logBefore, _ := os.ReadFile(store.SessionEventLog(path))

	// "Crash": a fresh runtime resumes the same path.
	recovered, err := agent.LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	if open, ok := recovered.OpenTurn(); !ok || open.TurnID != marker.ID {
		t.Fatalf("open turn after crash = %+v ok=%v", open, ok)
	}
	exec2 := agent.New(nil, nil, recovered, agent.Options{}, event.Discard)
	sink := &noticeSink{}
	c2 := New(Options{Executor: exec2, SessionDir: dir, SessionPath: path, Label: "test", Sink: sink})
	c2.recoverInterruptedTurn(path)

	got := exec2.Session().Snapshot()
	if len(got) < 4 || got[3].Content != "second" || got[3].Role != provider.RoleUser {
		t.Fatalf("recovered transcript = %+v, want the user prompt kept", got)
	}
	last := got[len(got)-1]
	if !last.LocalOnly || last.InterruptedTurn == nil {
		t.Fatalf("recovery must leave an interrupted-turn display record, got %+v", last)
	}
	logAfter, _ := os.ReadFile(store.SessionEventLog(path))
	if !strings.HasPrefix(string(logAfter), string(logBefore)) {
		t.Fatal("recovery must append, never rewrite earlier bytes")
	}
	types := dagLogEntryTypes(t, path)
	if types[len(types)-1] != "turn_end" {
		t.Fatalf("recovery entries = %v, want turn_end last", types)
	}
	reloaded, err := agent.LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reloaded.OpenTurn(); ok {
		t.Fatal("recovery must close the open turn")
	}
	if len(reloaded.Messages) != len(got) {
		t.Fatalf("reloaded transcript length %d, want %d", len(reloaded.Messages), len(got))
	}
	// A second resume finds nothing to recover and changes nothing.
	c2.recoverInterruptedTurn(path)
	if again := dagLogEntryTypes(t, path); len(again) != len(types) {
		t.Fatalf("idempotent recovery appended entries: %v", again)
	}
}

func TestFinishedTurnClosesLogMarkerInOneBatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	sess := agent.NewSession("sys")
	exec := agent.New(nil, nil, sess, agent.Options{}, event.Discard)
	c := New(Options{Executor: exec, SessionDir: dir, SessionPath: path, Label: "test", Sink: event.Discard})
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	if err := c.Snapshot(); err != nil {
		t.Fatal(err)
	}
	start := sess.Len()
	marker := c.markInFlightTurn(start, true)
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "second"})
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "two"})
	c.finishInFlightTurn(start, marker)
	types := dagLogEntryTypes(t, path)
	if got := strings.Join(types[len(types)-4:], ","); got != "message,message,turn_begin,turn_end" {
		t.Fatalf("tail entries = %v", types)
	}
	reloaded, err := agent.LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reloaded.OpenTurn(); ok {
		t.Fatal("finished turn must not stay open")
	}
}

func TestConcurrentWriterEmitsNoticeOnBothSides(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "shared.jsonl")
	const systemPrompt = "SYS"
	reply := [][]provider.Chunk{{{Type: provider.ChunkText, Text: "ok"}, {Type: provider.ChunkDone}}}
	sinkA, sinkB := &noticeSink{}, &noticeSink{}
	execA := agent.New(&recordingProvider{streams: reply}, tool.NewRegistry(), agent.NewSession(systemPrompt), agent.Options{}, event.Discard)
	ctrlA := New(Options{Runner: execA, Executor: execA, SystemPrompt: systemPrompt, SessionDir: dir, SessionPath: path, Label: "a", Sink: sinkA})
	if err := ctrlA.RunTurn(context.Background(), "first from A"); err != nil {
		t.Fatal(err)
	}
	loaded, err := agent.LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	execB := agent.New(&recordingProvider{streams: reply}, tool.NewRegistry(), agent.NewSession(systemPrompt), agent.Options{}, event.Discard)
	ctrlB := New(Options{Runner: execB, Executor: execB, SystemPrompt: systemPrompt, SessionDir: dir, SessionPath: path, Label: "b", Sink: sinkB})
	ctrlB.Resume(loaded, path)
	if err := ctrlA.RunTurn(context.Background(), "second from A"); err != nil {
		t.Fatal(err)
	}
	if err := ctrlB.RunTurn(context.Background(), "second from B"); err != nil {
		t.Fatal(err)
	}
	notice, ok := sinkB.lastNotice()
	if !ok || notice.Code != event.NoticeCodeSessionConcurrentWriter {
		t.Fatalf("B notice = %+v ok=%v, want concurrent writer notice", notice, ok)
	}
	// A reopen after both wrote lands on the newest head and reports the other.
	reopened, err := agent.LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	execC := agent.New(&recordingProvider{streams: reply}, tool.NewRegistry(), agent.NewSession(systemPrompt), agent.Options{}, event.Discard)
	sinkC := &noticeSink{}
	ctrlC := New(Options{Runner: execC, Executor: execC, SystemPrompt: systemPrompt, SessionDir: dir, SessionPath: path, Label: "c", Sink: sinkC})
	ctrlC.Resume(reopened, path)
	notice, ok = sinkC.lastNotice()
	if !ok || notice.Code != event.NoticeCodeSessionHeadSwitched {
		t.Fatalf("C notice = %+v ok=%v, want head switched notice", notice, ok)
	}
}
