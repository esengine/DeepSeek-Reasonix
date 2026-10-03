package serve

import (
	"path/filepath"
	"testing"

	"reasonix/internal/base/diff"
	"reasonix/internal/base/testenv"
	"reasonix/internal/session/control"
	"reasonix/internal/state/checkpoint"
	"reasonix/internal/state/store"
)

func TestCheckpointsReportsRewindRangeFileCount(t *testing.T) {
	root := testenv.TempDir(t)
	session := filepath.Join(testenv.TempDir(t), "session.jsonl")
	snapshots := checkpoint.New(store.SessionCheckpointDir(session), root)
	snapshots.Begin(0, "no edits", 0)
	snapshots.Begin(1, "a and b", 2)
	for _, name := range []string{"a.txt", "b.txt"} {
		snapshots.Snapshot(diff.Change{Path: filepath.Join(root, name), Kind: diff.Modify, OldText: "before"})
	}
	snapshots.Begin(2, "a again and c", 4)
	snapshots.Snapshot(diff.Change{Path: "./a.txt", Kind: diff.Modify, OldText: "middle"})
	snapshots.Snapshot(diff.Change{Path: "c.txt", Kind: diff.Create})
	snapshots.Begin(3, "finalize", 6)
	ctrl := control.New(control.Options{WorkspaceRoot: root, SessionPath: session})
	defer ctrl.Close()
	server := &Server{ctrl: ctrl}
	var response []struct {
		Turn  int `json:"turn"`
		Files int `json:"files"`
	}
	decodeGet(t, server.checkpoints, "/checkpoints", &response)
	if len(response) != 4 {
		t.Fatalf("checkpoints: %+v", response)
	}
	for i, cp := range response {
		if cp.Turn != i || cp.Files != []int{3, 3, 2, 0}[i] {
			t.Fatalf("turn %d: %+v", i, cp)
		}
	}
}
