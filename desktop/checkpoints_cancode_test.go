package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/checkpoint"
	"reasonix/internal/control"
	"reasonix/internal/event"
)

func seedCheckpoint(t *testing.T, ckptDir string, c checkpoint.Checkpoint) {
	t.Helper()
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ckptDir, "turn-"+strconv.Itoa(c.Turn)+".json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertCheckpointFilesEncodeAsArray(t *testing.T, metas []CheckpointMeta) {
	t.Helper()
	raw, err := json.Marshal(metas)
	if err != nil {
		t.Fatal(err)
	}
	var payload []struct {
		Files json.RawMessage `json:"files"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	for i, item := range payload {
		if string(item.Files) == "null" {
			t.Fatalf("checkpoint %d files encoded as null; frontend expects []", i)
		}
		if len(item.Files) == 0 || item.Files[0] != '[' {
			t.Fatalf("checkpoint %d files encoded as %s, want JSON array", i, item.Files)
		}
	}
}

// TestCheckpointsCanCodePropagatesToEarlierTurns covers #3438: RestoreCode(turn)
// reverts files touched in that turn or any later one, so a turn with no file
// changes of its own can still rewind code when a later turn changed files. The
// desktop CanCode flag must reflect that suffix capability, not just the turn's
// own paths.
func TestCheckpointsCanCodePropagatesToEarlierTurns(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "s.jsonl")
	ckptDir := sessionPath[:len(sessionPath)-len(".jsonl")] + ".ckpt"
	if err := os.MkdirAll(ckptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "old"
	now := time.Now()
	seedCheckpoint(t, ckptDir, checkpoint.Checkpoint{Turn: 0, Time: now, Prompt: "ask only", MsgIndex: 0})
	seedCheckpoint(t, ckptDir, checkpoint.Checkpoint{Turn: 1, Time: now, Prompt: "edit a file", MsgIndex: 2,
		Files: []checkpoint.FileSnap{{Path: "a.txt", Content: &content}}})
	seedCheckpoint(t, ckptDir, checkpoint.Checkpoint{Turn: 2, Time: now, Prompt: "ask again", MsgIndex: 4})

	ag := agent.New(nil, nil, agent.NewSession("sys"), agent.Options{}, event.Discard)
	ctrl := control.New(control.Options{Executor: ag, SessionDir: dir, Label: "test"})
	ctrl.SetSessionPath(sessionPath)

	app := &App{}
	app.setTestCtrl(ctrl, "test")

	metas := app.CheckpointsForTab("test")
	if len(metas) != 3 {
		t.Fatalf("checkpoints = %d, want 3", len(metas))
	}
	got := map[int]bool{}
	for _, m := range metas {
		got[m.Turn] = m.CanCode
	}
	if !got[0] {
		t.Error("turn 0 (no files of its own) should allow code rewind — turn 1 changed files")
	}
	if !got[1] {
		t.Error("turn 1 changed files, should allow code rewind")
	}
	if got[2] {
		t.Error("turn 2 is after the last file-bearing turn, should NOT allow code rewind")
	}
	if metas[0].TurnFileCount != 0 {
		t.Fatalf("turn 0 file count = %d, want 0 for this turn", metas[0].TurnFileCount)
	}
	if metas[1].TurnFileCount != 1 {
		t.Fatalf("turn 1 file count = %d, want 1 for this turn", metas[1].TurnFileCount)
	}
	if len(metas[0].Files) != 1 || metas[0].Files[0] != "a.txt" {
		t.Fatalf("turn 0 cumulative files = %#v, want [a.txt]", metas[0].Files)
	}
	if metas[0].FileCount != 1 || metas[0].FilesTruncated {
		t.Fatalf("turn 0 file summary = count %d truncated %v, want count 1 truncated false", metas[0].FileCount, metas[0].FilesTruncated)
	}
	if len(metas[2].Files) != 0 {
		t.Fatalf("turn 2 cumulative files = %#v, want empty", metas[2].Files)
	}
	assertCheckpointFilesEncodeAsArray(t, metas)
}

func TestCheckpointsForTabLimitsCumulativeFilePreview(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "s.jsonl")
	ckptDir := sessionPath[:len(sessionPath)-len(".jsonl")] + ".ckpt"
	if err := os.MkdirAll(ckptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "old"
	files := make([]checkpoint.FileSnap, 0, checkpointFilePreviewLimit+5)
	for i := 0; i < checkpointFilePreviewLimit+5; i++ {
		files = append(files, checkpoint.FileSnap{Path: "file-" + strconv.Itoa(1000+i) + ".txt", Content: &content})
	}
	now := time.Now()
	seedCheckpoint(t, ckptDir, checkpoint.Checkpoint{Turn: 0, Time: now, Prompt: "before edits", MsgIndex: 0})
	seedCheckpoint(t, ckptDir, checkpoint.Checkpoint{Turn: 1, Time: now, Prompt: "edit many files", MsgIndex: 2, Files: files})

	ag := agent.New(nil, nil, agent.NewSession("sys"), agent.Options{}, event.Discard)
	ctrl := control.New(control.Options{Executor: ag, SessionDir: dir, Label: "test"})
	ctrl.SetSessionPath(sessionPath)

	app := &App{}
	app.setTestCtrl(ctrl, "test")

	metas := app.CheckpointsForTab("test")
	if len(metas) != 2 {
		t.Fatalf("checkpoints = %d, want 2", len(metas))
	}
	if metas[0].FileCount != checkpointFilePreviewLimit+5 {
		t.Fatalf("turn 0 cumulative file count = %d, want %d", metas[0].FileCount, checkpointFilePreviewLimit+5)
	}
	if len(metas[0].Files) != checkpointFilePreviewLimit {
		t.Fatalf("turn 0 preview files = %d, want %d", len(metas[0].Files), checkpointFilePreviewLimit)
	}
	if !metas[0].FilesTruncated {
		t.Fatal("turn 0 should mark file preview as truncated")
	}
	if metas[0].TurnFileCount != 0 {
		t.Fatalf("turn 0 file count = %d, want 0 for this turn", metas[0].TurnFileCount)
	}
	if metas[1].TurnFileCount != checkpointFilePreviewLimit+5 {
		t.Fatalf("turn 1 file count = %d, want %d", metas[1].TurnFileCount, checkpointFilePreviewLimit+5)
	}
	assertCheckpointFilesEncodeAsArray(t, metas)
}
