package checkpoint

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
)

func committedCodeUndo(t *testing.T) (*Store, string, string, string, RewindResult) {
	t.Helper()
	root := testenv.TempDir(t)
	dir := testenv.TempDir(t)
	path := filepath.Join(root, "note.txt")
	write(t, path, "before")
	s := New(dir, root)
	s.Begin(0, "edit", 0)
	s.CaptureBefore(path, CaptureBeforeOpts{})
	write(t, path, "after")
	s.CaptureAfter(path, CaptureAfterOpts{Seq: 1})
	plan, err := s.PrepareRewind(0, RewindCode, 1, 0, true)
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.CommitRewindWithForward(plan.PlanID, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return s, dir, root, path, result
}

func TestAvailableUndoSurvivesReloadAndRestoresFile(t *testing.T) {
	_, dir, root, path, result := committedCodeUndo(t)
	s := New(dir, root)
	undo, err := s.AvailableUndo()
	if err != nil || undo == nil || undo.TransactionID != result.TransactionID || undo.Turn != 0 || undo.Files != 1 {
		t.Fatalf("undo = %+v, err = %v", undo, err)
	}
	if _, err := s.UndoRewind(undo.TransactionID, nil); err != nil {
		t.Fatal(err)
	}
	if got := read(t, path); got != "after" {
		t.Fatalf("file = %q", got)
	}
	reloaded := New(dir, root)
	if undo, err := reloaded.AvailableUndo(); err != nil || undo != nil {
		t.Fatalf("undone operation recovered: %+v, %v", undo, err)
	}
}

func TestUndoInvalidationSurvivesReload(t *testing.T) {
	for _, cause := range []string{"new_turn", "captured_mutation", "explicit"} {
		t.Run(cause, func(t *testing.T) {
			s, dir, root, path, result := committedCodeUndo(t)
			switch cause {
			case "new_turn":
				s.Begin(1, "next", 1)
			case "captured_mutation":
				observer := NewMutationObserver(ObserverOptions{Store: s})
				release, err := observer.BeginWrite()
				if err != nil {
					t.Fatal(err)
				}
				defer release()
				write(t, path, "new work")
				s.CaptureAfter(path, CaptureAfterOpts{Seq: 2})
				// Matching file contents must not revive invalidated eligibility.
				write(t, path, "before")
			case "explicit":
				s.InvalidateUndo()
			}
			reloaded := New(dir, root)
			undo, err := reloaded.AvailableUndo()
			if err != nil || undo != nil {
				t.Fatalf("undo recovered after %s: %+v, %v", cause, undo, err)
			}
			if _, err := reloaded.UndoRewind(result.TransactionID, nil); err == nil {
				t.Fatal("invalidated operation was accepted")
			}
			if got := read(t, path); got != "before" {
				t.Fatalf("file changed to %q", got)
			}
		})
	}
}

func TestAvailableUndoLeavesExternalEditValidationToExecution(t *testing.T) {
	s, _, _, path, result := committedCodeUndo(t)
	generation := s.Barrier().Generation()
	if undo, err := s.AvailableUndo(); err != nil || undo == nil {
		t.Fatalf("undo = %+v, %v", undo, err)
	}
	if s.Barrier().Generation() != generation {
		t.Fatal("reading undo invalidated prepared plans")
	}
	write(t, path, "external edit")
	if undo, err := s.AvailableUndo(); err != nil || undo == nil || undo.TransactionID != result.TransactionID {
		t.Fatalf("saved offer after external edit = %+v, %v", undo, err)
	}
	if _, err := s.UndoRewind(result.TransactionID, nil); err == nil {
		t.Fatal("undo accepted an external edit")
	}
	if got := read(t, path); got != "external edit" {
		t.Fatalf("undo changed file to %q", got)
	}
}

func TestRecoveryDoesNotFallBackToSupersededUndo(t *testing.T) {
	s, dir, root, _, first := committedCodeUndo(t)
	// A conversation-only rewind can supersede a code rewind without touching files.
	plan, err := s.PrepareRewind(0, RewindConversation, 2, 0, true)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.CommitRewindWithForward(plan.PlanID, []byte("[]"), &recordingConversationApplier{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	reloaded := New(dir, root)
	undo, err := reloaded.AvailableUndo()
	if err != nil || undo == nil || undo.TransactionID != second.TransactionID {
		t.Fatalf("latest undo = %+v, %v", undo, err)
	}
	reloaded.InvalidateUndo()
	final := New(dir, root)
	if undo, err := final.AvailableUndo(); err != nil || undo != nil {
		t.Fatalf("older operation revived: %+v, %v", undo, err)
	}
	if _, err := final.UndoRewind(first.TransactionID, nil); err == nil {
		t.Fatal("superseded operation was accepted")
	}
}

func TestBeginStopsWhenUndoInvalidationCannotPersist(t *testing.T) {
	s, dir, _, _, result := committedCodeUndo(t)
	txDir := filepath.Join(dir, "transactions")
	backupDir := filepath.Join(dir, "transactions-backup")
	if err := os.Rename(txDir, backupDir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(txDir, []byte("blocked"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := s.Begin(1, "next", 1); err == nil {
		t.Fatal("Begin succeeded after undo invalidation persistence failed")
	}
	if got := s.NextTurn(); got != 1 {
		t.Fatalf("next turn = %d, want 1 after rejected begin", got)
	}
	undo, err := s.AvailableUndo()
	if err != nil || undo == nil || undo.TransactionID != result.TransactionID {
		t.Fatalf("undo after rejected begin = %+v, err = %v", undo, err)
	}

	var manifest TransactionManifest
	if err := readJSONFile(filepath.Join(backupDir, result.TransactionID+".json"), &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.State != TxCommitted {
		t.Fatalf("manifest state = %q, want committed after failed invalidation", manifest.State)
	}
}

func TestBeginWriteStopsWhenUndoInvalidationCannotPersist(t *testing.T) {
	for _, entry := range []string{"tool", "registered_writer"} {
		t.Run(entry, func(t *testing.T) {
			s, dir, _, path, _ := committedCodeUndo(t)
			txDir := filepath.Join(dir, "transactions")
			if err := os.Rename(txDir, txDir+"-backup"); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(txDir, []byte("blocked"), 0o644); err != nil {
				t.Fatal(err)
			}
			observer := NewMutationObserver(ObserverOptions{Store: s})
			var err error
			if entry == "tool" {
				var release func()
				release, err = observer.BeginWrite()
				if release != nil {
					release()
					t.Fatal("failed preparation returned a write permit")
				}
			} else {
				err = observer.RegisterWriter("writer", "background_writer", 0)
			}
			if err == nil {
				t.Fatal("writer admitted despite failed undo invalidation")
			}
			if s.Barrier().Busy() || len(observer.ActiveWriters()) != 0 {
				t.Fatal("failed preparation retained writer state")
			}
			if got := read(t, path); got != "before" {
				t.Fatalf("file changed to %q", got)
			}
		})
	}
}
