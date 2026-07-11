package agent

import (
	"context"
	"encoding/json"
	"testing"

	"reasonix/internal/diff"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// recordingRenamer is a stub BatchPreviewer that records the PendingBatchState
// each PreviewInBatch call receives, so a test can assert the agent threads and
// updates the batch state across calls. It previews every call as a rename of
// its (src,dst) args, ignoring disk entirely.
type recordingRenamer struct {
	seen *[]tool.PendingBatchState
}

func (recordingRenamer) Name() string                  { return "move_file" }
func (recordingRenamer) Description() string            { return "stub" }
func (recordingRenamer) Schema() json.RawMessage        { return json.RawMessage(`{"type":"object"}`) }
func (recordingRenamer) ReadOnly() bool                 { return false }
func (recordingRenamer) Execute(context.Context, json.RawMessage) (string, error) { return "", nil } // unused

func (r recordingRenamer) Preview(args json.RawMessage) (diff.Change, error) {
	return r.PreviewInBatch(args, tool.PendingBatchState{})
}

func (r recordingRenamer) PreviewInBatch(args json.RawMessage, pending tool.PendingBatchState) (diff.Change, error) {
	// snapshot the pending sets (copy: the agent mutates its live maps between calls)
	*r.seen = append(*r.seen, clonePending(pending))
	var p struct {
		Src string `json:"source_path"`
		Dst string `json:"destination_path"`
	}
	_ = json.Unmarshal(args, &p)
	return diff.BuildRename(p.Src, p.Dst), nil
}

func clonePending(p tool.PendingBatchState) tool.PendingBatchState {
	c := tool.PendingBatchState{Created: map[string]bool{}, Removed: map[string]bool{}}
	for k := range p.Created {
		c.Created[k] = true
	}
	for k := range p.Removed {
		c.Removed[k] = true
	}
	return c
}

// TestWithPreviewFileDiffsThreadsChainedRenameState proves the agent-level
// wiring: withPreviewFileDiffs feeds each move_file the effects of the earlier
// renames in the same batch. For a→b; b→c, the second call must see b in
// Created (so its card renders) and a in Removed.
func TestWithPreviewFileDiffsThreadsChainedRenameState(t *testing.T) {
	var seen []tool.PendingBatchState
	reg := tool.NewRegistry()
	reg.Add(recordingRenamer{seen: &seen})
	a := &Agent{tools: reg}

	calls := []provider.ToolCall{
		{ID: "1", Name: "move_file", Arguments: `{"source_path":"a","destination_path":"b"}`},
		{ID: "2", Name: "move_file", Arguments: `{"source_path":"b","destination_path":"c"}`},
	}
	out := a.withPreviewFileDiffs(calls)

	// Both cards must be tagged rename with the right paths.
	if out[0].Kind != string(diff.Rename) || out[0].SrcPath != "a" || out[0].DstPath != "b" {
		t.Errorf("call 0 = %q %q→%q, want rename a→b", out[0].Kind, out[0].SrcPath, out[0].DstPath)
	}
	if out[1].Kind != string(diff.Rename) || out[1].SrcPath != "b" || out[1].DstPath != "c" {
		t.Errorf("call 1 = %q %q→%q, want rename b→c", out[1].Kind, out[1].SrcPath, out[1].DstPath)
	}

	if len(seen) != 2 {
		t.Fatalf("expected 2 PreviewInBatch calls, got %d", len(seen))
	}
	// First call sees an empty batch state.
	if len(seen[0].Created) != 0 || len(seen[0].Removed) != 0 {
		t.Errorf("call 0 pending = %+v, want empty", seen[0])
	}
	// Second call sees a's removal and b's creation from the first rename.
	if !seen[1].Created["b"] {
		t.Errorf("call 1 pending.Created missing b: %+v", seen[1].Created)
	}
	if !seen[1].Removed["a"] {
		t.Errorf("call 1 pending.Removed missing a: %+v", seen[1].Removed)
	}
	if seen[1].Created["a"] {
		t.Errorf("call 1 pending.Created must not retain a (it was moved away): %+v", seen[1].Created)
	}
}
