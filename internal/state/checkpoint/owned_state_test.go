package checkpoint

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
)

func twoOwnedTurns(t *testing.T) (*Store, string, string, string) {
	t.Helper()
	root, dir := testenv.TempDir(t), testenv.TempDir(t)
	path := filepath.Join(root, "note.txt")
	write(t, path, "initial")
	s := New(dir, root)
	for turn, text := range []string{"middle", "latest"} {
		s.Begin(turn, "edit", turn)
		s.CaptureBefore(path, CaptureBeforeOpts{})
		write(t, path, text)
		s.CaptureAfter(path, CaptureAfterOpts{Seq: int64(turn + 1)})
	}
	return s, dir, root, path
}

func prepareOwnedRewind(t *testing.T, s *Store, turn int) RewindPlan {
	t.Helper()
	plan, err := s.PrepareRewind(turn, RewindCode, 1, turn, true)
	if err != nil || !plan.CanFiles {
		t.Fatalf("prepare turn %d: plan=%+v, err=%v", turn, plan, err)
	}
	return plan
}

func commitOwnedRewind(t *testing.T, s *Store, turn int) RewindResult {
	t.Helper()
	plan := prepareOwnedRewind(t, s, turn)
	result, err := s.CommitRewindWithForward(plan.PlanID, nil, nil, nil)
	if err != nil || !result.OK {
		t.Fatalf("commit turn %d: result=%+v, err=%v", turn, result, err)
	}
	return result
}

func TestCodeRewindOwnedStatePersistsForEarlierTurnAndUndo(t *testing.T) {
	s, dir, root, path := twoOwnedTurns(t)
	first := commitOwnedRewind(t, s, 1)
	s = New(dir, root)
	if id := s.LastUndoTransactionID(); id != first.TransactionID {
		t.Fatalf("undo after reload: %q, want %q", id, first.TransactionID)
	}
	commitOwnedRewind(t, s, 1)
	s = New(dir, root)
	earlier := commitOwnedRewind(t, s, 0)
	if got := read(t, path); got != "initial" {
		t.Fatalf("earlier rewind restored %q", got)
	}
	s = New(dir, root)
	if _, err := s.UndoRewind(earlier.TransactionID, nil); err != nil {
		t.Fatal(err)
	}
	if got := read(t, path); got != "middle" {
		t.Fatalf("undo restored %q", got)
	}
	s = New(dir, root)
	prepareOwnedRewind(t, s, 0)
	metas := s.List()
	state, ok := s.FileState(path)
	if len(metas) != 2 || len(metas[0].Paths) != 1 || len(metas[1].Paths) != 1 || !ok || state.Content == nil || *state.Content != "initial" {
		t.Fatalf("historical snapshots changed: metas=%+v, state=%+v", metas, state)
	}
}

func TestCodeRewindOwnedStateStillRejectsExternalChanges(t *testing.T) {
	s, dir, root, path := twoOwnedTurns(t)
	commitOwnedRewind(t, s, 1)
	write(t, path, "external edit")
	s = New(dir, root)
	plan, err := s.PrepareRewind(0, RewindCode, 1, 0, true)
	if err != nil || plan.CanFiles || len(plan.Conflicts) == 0 {
		t.Fatalf("external edit accepted: %+v, %v", plan, err)
	}
	if got := read(t, path); got != "external edit" {
		t.Fatalf("external edit changed to %q", got)
	}
}

func TestCodeRewindOwnedStateTracksExistence(t *testing.T) {
	for _, action := range []string{"create", "delete"} {
		t.Run(action, func(t *testing.T) {
			root, dir := testenv.TempDir(t), testenv.TempDir(t)
			path := filepath.Join(root, "note.txt")
			if action == "delete" {
				write(t, path, "original")
			}
			s := New(dir, root)
			s.Begin(0, action, 0)
			s.CaptureBefore(path, CaptureBeforeOpts{})
			if action == "create" {
				write(t, path, "created")
			} else if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			s.CaptureAfter(path, CaptureAfterOpts{Seq: 1})
			result := commitOwnedRewind(t, s, 0)
			s = New(dir, root)
			prepareOwnedRewind(t, s, 0)
			if _, err := s.UndoRewind(result.TransactionID, nil); err != nil {
				t.Fatal(err)
			}
			s = New(dir, root)
			prepareOwnedRewind(t, s, 0)
			if action == "create" {
				if got := read(t, path); got != "created" {
					t.Fatalf("undo restored %q", got)
				}
			} else if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("deleted file returned: %v", err)
			}
		})
	}
}

func TestCodeRewindOwnedStateCompensationAndCrashRecovery(t *testing.T) {
	for _, phase := range []string{"finalize", "after_conversation_before_finalize"} {
		t.Run(phase, func(t *testing.T) {
			s, dir, root, path := twoOwnedTurns(t)
			first := commitOwnedRewind(t, s, 1)
			plan := prepareOwnedRewind(t, s, 0)
			if _, err := s.CommitRewindWithForward(plan.PlanID, nil, nil, &InjectFail{Phase: phase}); err == nil {
				t.Fatal("injected failure succeeded")
			}
			s = New(dir, root)
			if got := read(t, path); got != "middle" {
				t.Fatalf("recovery restored %q", got)
			}
			if id := s.LastUndoTransactionID(); id != first.TransactionID {
				t.Fatalf("previous undo lost: %q, want %q", id, first.TransactionID)
			}
			commitOwnedRewind(t, s, 0)
		})
	}
}

func TestCodeRewindOwnedStateRepairsPartialMetadataSave(t *testing.T) {
	root, dir := testenv.TempDir(t), testenv.TempDir(t)
	s := New(dir, root)
	for turn, name := range []string{"first.txt", "second.txt"} {
		path := filepath.Join(root, name)
		write(t, path, "before")
		s.Begin(turn, "edit", turn)
		s.CaptureBefore(path, CaptureBeforeOpts{})
		write(t, path, "after")
		s.CaptureAfter(path, CaptureAfterOpts{Seq: int64(turn + 1)})
	}
	plan := prepareOwnedRewind(t, s, 0)
	blocked := filepath.Join(dir, "turn-1.json")
	backup := blocked + ".held"
	if err := os.Rename(blocked, backup); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(blocked, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CommitRewindWithForward(plan.PlanID, nil, nil, nil); err == nil {
		t.Fatal("metadata write failure was ignored")
	}
	if err := os.Remove(blocked); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(backup, blocked); err != nil {
		t.Fatal(err)
	}
	s = New(dir, root)
	for _, name := range []string{"first.txt", "second.txt"} {
		if got := read(t, filepath.Join(root, name)); got != "after" {
			t.Fatalf("compensation of %s left %q", name, got)
		}
	}
	prepareOwnedRewind(t, s, 0)
}

func TestCodeRewindOwnedStateTracksPermissions(t *testing.T) {
	root, dir := testenv.TempDir(t), testenv.TempDir(t)
	path := filepath.Join(root, "note.txt")
	write(t, path, "same contents")
	if err := os.Chmod(path, 0o444); err != nil {
		t.Fatal(err)
	}
	s := New(dir, root)
	s.Begin(0, "change permissions", 0)
	s.CaptureBefore(path, CaptureBeforeOpts{})
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	s.CaptureAfter(path, CaptureAfterOpts{Seq: 1})
	result := commitOwnedRewind(t, s, 0)
	s = New(dir, root)
	prepareOwnedRewind(t, s, 0)
	if _, err := s.UndoRewind(result.TransactionID, nil); err != nil {
		t.Fatal(err)
	}
	prepareOwnedRewind(t, New(dir, root), 0)
}
