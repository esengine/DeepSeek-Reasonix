package main

import (
	"encoding/json"
	"path/filepath"
	"reasonix/internal/checkpoint"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/eventwire"
	"reasonix/internal/provider"
	"reasonix/internal/turnevent"
	"testing"
)

type turnResultReader struct {
	control.SessionAPI
	path      string
	changes   *checkpoint.TurnChanges
	afterRead func()
}

func (r *turnResultReader) SessionPath() string { return r.path }
func (r *turnResultReader) CheckpointTurnChanges(int) *checkpoint.TurnChanges {
	if r.afterRead != nil {
		r.afterRead()
	}
	return r.changes
}
func (r *turnResultReader) History() []provider.Message {
	if r.afterRead != nil {
		r.afterRead()
	}
	return []provider.Message{{Role: provider.RoleTool, ID: "entry-old", ToolCallID: "call", Content: "old log"}, {Role: provider.RoleTool, ID: "entry-new", ToolCallID: "call", Content: "new log"}}
}

func TestWorkspaceTurnResultIsSessionAndResultScoped(t *testing.T) {
	ctrl := &turnResultReader{path: "session-a", changes: &checkpoint.TurnChanges{ID: "result-a", Turn: 0, Coverage: "complete", Files: []checkpoint.TurnFile{{Path: "f", Patch: "frozen"}}, Reasons: []string{}}}
	a := &App{tabs: map[string]*WorkspaceTab{"a": {ID: "a", Ctrl: ctrl}}}
	if got := a.WorkspaceTurnChanges("a", "session-a", 0, "result-a"); got.Coverage != "complete" || got.Files[0].Patch != "" {
		t.Fatalf("summary: %+v", got)
	}
	if got := a.WorkspaceTurnChangeDetail("a", "session-a", 0, "result-a", "f"); got == nil || got.Patch != "frozen" {
		t.Fatalf("detail: %+v", got)
	}
	if a.WorkspaceTurnChangeDetail("a", "session-a", 0, "old-result", "f") != nil {
		t.Fatal("reused turn leaked another result")
	}
	if a.WorkspaceTurnChangeDetail("a", "session-a", 0, "result-a", "../unrecorded") != nil {
		t.Fatal("unrecorded path read")
	}
	if a.TurnCheckLog("a", "session-a", "call", "entry-old").Output != "old log" {
		t.Fatal("reused provider ID selected a newer log")
	}
	if a.TurnCheckLog("a", "session-a", "call", "missing") != nil {
		t.Fatal("missing source ID read succeeded")
	}
	if a.TurnCheckLog("a", "session-b", "call", "entry-old") != nil {
		t.Fatal("wrong session log")
	}
	ctrl.afterRead = func() { ctrl.path = "session-b" }
	if a.WorkspaceTurnChanges("a", "session-a", 0, "result-a").Coverage != "unknown" {
		t.Fatal("session rotated during result read")
	}
	ctrl.path = "session-a"
	if a.TurnCheckLog("a", "session-a", "call", "entry-old") != nil {
		t.Fatal("session rotated during log read")
	}
}

func TestWorkspaceTurnResultMissingControllerHasEmptyArrays(t *testing.T) {
	a := &App{}
	r := a.WorkspaceTurnChanges("missing", "session", 0, "result")
	if r.Files == nil || r.Reasons == nil || r.Coverage != "unknown" {
		t.Fatalf("result: %+v", r)
	}
	if a.WorkspaceTurnChangeDetail("missing", "session", 0, "result", "f") != nil || a.TurnCheckLog("missing", "session", "tool", "entry") != nil {
		t.Fatal("missing controller read succeeded")
	}
}

func TestTurnResultDisplayPersistsAndReplaysWithoutModelMessages(t *testing.T) {
	turn := 0
	receipt := &event.CompletionReceipt{Verdict: "partial", Diff: &checkpoint.TurnChanges{ID: "frozen", Turn: 0, Coverage: "complete", Files: []checkpoint.TurnFile{}, Reasons: []string{}}, Verifications: []event.ReceiptVerification{{Command: "go test ./...", ToolCallID: "check-id", Passed: true}}}
	done := event.Event{Kind: event.TurnDone, TurnID: "turn-id", CheckpointTurn: &turn, Receipt: receipt}
	var tab WorkspaceTab
	tab.recordDisplayEvent(event.Event{Kind: event.Message, Text: "normal answer"})
	tab.recordDisplayEvent(done)
	rows := tab.takeDisplayTurn(false)
	if len(rows) != 1 || rows[0].CompletionReceipt == nil || rows[0].CheckpointTurn == nil || *rows[0].CheckpointTurn != 0 {
		t.Fatalf("display rows: %+v", rows)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "session.json")
	if err := recordSessionPlannerDisplayForTurn(dir, path, "turn-id", "prompt", rows); err != nil {
		t.Fatal(err)
	}
	stored := sessionPlannerDisplayTurns(dir, path)
	if len(stored) != 1 || stored[0].Messages[0].CompletionReceipt.Verifications[0].ToolCallID != "check-id" {
		t.Fatalf("stored: %+v", stored)
	}
	b, err := json.Marshal(rows[0])
	if err != nil {
		t.Fatal(err)
	}
	var old struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(b, &old); err != nil || old.Role != "notice" || old.Content != "" {
		t.Fatalf("old reader: %+v %v", old, err)
	}
	projection := turnevent.PendingProjection{Status: event.TurnCompleted, Events: []turnevent.Envelope{{Kind: "turn_done", TurnID: "turn-id", Event: eventwire.ToWire(done)}}}
	replayed := displayMessagesFromProjection(projection)
	if len(replayed) != 1 || replayed[0].CompletionReceipt.Verifications[0].ToolCallID != "check-id" {
		t.Fatalf("durable replay: %+v", replayed)
	}
}
