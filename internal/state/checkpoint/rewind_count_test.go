package checkpoint

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestListRewindFilesMatchesCommit(t *testing.T) {
	root := testenv.TempDir(t)
	a, b, c := filepath.Join(root, "a.txt"), filepath.Join(root, "b.txt"), filepath.Join(root, "c.txt")
	write(t, a, "a0")
	write(t, b, "b0")
	store := New("", root)
	observer := NewMutationObserver(ObserverOptions{Store: store})
	store.Begin(0, "no edits", 0)
	store.Begin(1, "edit a and b", 2)
	for _, p := range []string{a, b} {
		observer.BeforeMutation(p, "edit", CaptureBeforeMutation)
		write(t, p, "middle")
		observer.AfterMutation(p, "edit")
	}
	store.Begin(2, "edit a again and create c", 4)
	for _, p := range []string{"./a.txt", "c.txt"} {
		observer.BeforeMutation(p, "edit", CaptureBeforeMutation)
		write(t, filepath.Join(root, p), "latest")
		observer.AfterMutation(p, "edit")
	}
	store.Begin(3, "no later edits", 6)

	for i, meta := range store.List() {
		want := []int{3, 3, 2, 0}[i]
		plan, err := store.PrepareRewind(meta.Turn, RewindCode, 1, 0, false)
		if err != nil || meta.Turn != i || meta.RewindFiles != want || meta.RewindFiles != plan.FileCount {
			t.Fatalf("turn %d: metadata=%+v plan=%+v err=%v", i, meta, plan, err)
		}
		if i == 0 && len(meta.Paths) != 0 {
			t.Fatalf("empty turn gained local paths: %v", meta.Paths)
		}
	}
	plan, err := store.PrepareRewind(0, RewindCode, 1, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	result, err := store.CommitRewindWithForward(plan.PlanID, nil, nil, nil)
	if err != nil || !result.OK || len(result.Written)+len(result.Deleted) != plan.FileCount {
		t.Fatalf("commit: result=%+v plan=%+v err=%v", result, plan, err)
	}
	if read(t, a) != "a0" || read(t, b) != "b0" {
		t.Fatal("rewind did not restore both existing files")
	}
	if _, err := os.Stat(c); !os.IsNotExist(err) {
		t.Fatalf("created file remains after rewind: %v", err)
	}
}
