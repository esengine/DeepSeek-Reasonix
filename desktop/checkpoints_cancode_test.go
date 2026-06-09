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
}
