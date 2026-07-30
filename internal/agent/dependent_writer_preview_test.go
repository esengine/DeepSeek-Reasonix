package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
	"reasonix/internal/tool/builtin"
)

type mutateThenFailTool struct{ path string }

func (m mutateThenFailTool) Name() string            { return "mutate_then_fail" }
func (m mutateThenFailTool) Description() string     { return "test writer that mutates before failing" }
func (m mutateThenFailTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (m mutateThenFailTool) ReadOnly() bool          { return false }
func (m mutateThenFailTool) Execute(context.Context, json.RawMessage) (string, error) {
	if err := os.WriteFile(m.path, []byte("status=\"ready\"\n"), 0o600); err != nil {
		return "", err
	}
	return "", errors.New("simulated failure after write")
}

func TestDependentSameBatchEditRefreshesPreviewBeforeExecution(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "task.txt")
	if err := os.WriteFile(path, []byte("status=\"draft\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	reg := tool.NewRegistry()
	for _, tl := range (builtin.Workspace{Dir: dir}).Tools("edit_file") {
		reg.Add(tl)
	}
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{
			toolCallChunk("c1", "edit_file", `{"path":"task.txt","old_string":"draft","new_string":"ready"}`),
			toolCallChunk("c2", "edit_file", `{"path":"task.txt","old_string":"ready","new_string":"done"}`),
			{Type: provider.ChunkDone},
		},
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}
	var events []event.Event
	a := New(prov, reg, NewSession(""), Options{}, event.FuncSink(func(e event.Event) {
		events = append(events, e)
	}))
	if err := a.Run(context.Background(), "advance status twice"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "status=\"done\"\n" {
		t.Fatalf("final file = %q", data)
	}

	var fullDispatches []event.Event
	lastUpdatedDispatch := -1
	secondResult := -1
	for i, e := range events {
		switch {
		case e.Kind == event.ToolDispatch && !e.Tool.Partial && e.Tool.ID == "c2":
			fullDispatches = append(fullDispatches, e)
			if strings.Contains(e.Tool.Diff, `-status="ready"`) && strings.Contains(e.Tool.Diff, `+status="done"`) {
				lastUpdatedDispatch = i
			}
		case e.Kind == event.ToolResult && e.Tool.ID == "c2":
			secondResult = i
		}
	}
	if len(fullDispatches) != 2 {
		t.Fatalf("second edit full dispatches = %d, want initial plus refreshed", len(fullDispatches))
	}
	if fullDispatches[0].Tool.Diff != "" {
		t.Fatalf("dependent edit should not be previewable against the batch's initial state:\n%s", fullDispatches[0].Tool.Diff)
	}
	if lastUpdatedDispatch < 0 {
		t.Fatal("second edit never emitted a preview refreshed against the first edit")
	}
	if !fullDispatches[1].Tool.Refreshed {
		t.Fatal("updated preview dispatch must be marked refreshed for append-only sinks")
	}
	if secondResult < 0 || lastUpdatedDispatch >= secondResult {
		t.Fatalf("updated dispatch index %d must precede result index %d", lastUpdatedDispatch, secondResult)
	}
	if got := lastToolResult(a.session, "edit_file"); !strings.Contains(got, "-ready") || !strings.Contains(got, "+done") {
		t.Fatalf("second edit result did not ground the actual replacement:\n%s", got)
	}
	var archived provider.ToolCall
	for _, msg := range a.session.Snapshot() {
		for _, call := range msg.ToolCalls {
			if call.ID == "c2" {
				archived = call
			}
		}
	}
	if !strings.Contains(archived.Diff, `-status="ready"`) || !strings.Contains(archived.Diff, `+status="done"`) {
		t.Fatalf("session archived stale dependent preview:\n%s", archived.Diff)
	}
	if !a.session.NeedsRewriteSave() {
		t.Fatal("refreshing an already-appended assistant call must require a rewrite-safe snapshot")
	}
}

func TestDependentPreviewRefreshesAfterFailedWriterSideEffect(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "task.txt")
	if err := os.WriteFile(path, []byte("status=\"draft\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	reg := tool.NewRegistry()
	reg.Add(mutateThenFailTool{path: path})
	for _, tl := range (builtin.Workspace{Dir: dir}).Tools("edit_file") {
		reg.Add(tl)
	}
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{
			toolCallChunk("c1", "mutate_then_fail", `{}`),
			toolCallChunk("c2", "edit_file", `{"path":"task.txt","old_string":"ready","new_string":"done"}`),
			{Type: provider.ChunkDone},
		},
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}
	var refreshed bool
	a := New(prov, reg, NewSession(""), Options{}, event.FuncSink(func(e event.Event) {
		if e.Kind == event.ToolDispatch && e.Tool.ID == "c2" && e.Tool.Refreshed &&
			strings.Contains(e.Tool.Diff, `-status="ready"`) && strings.Contains(e.Tool.Diff, `+status="done"`) {
			refreshed = true
		}
	}))
	if err := a.Run(context.Background(), "run dependent edit after a partial failure"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "status=\"done\"\n" {
		t.Fatalf("final file = %q", data)
	}
	if !refreshed {
		t.Fatal("dependent preview was not refreshed after a failed writer changed disk")
	}
}
