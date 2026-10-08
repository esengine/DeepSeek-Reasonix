package checkpoint

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
)

type replacementApplier struct {
	recordingConversationApplier
	store       *Store
	restoreErr  error
	restoreHook func() error
}

func (a *replacementApplier) TruncateCheckpoints(turn int) error { return a.store.TruncateFrom(turn) }
func (a *replacementApplier) RestoreCheckpoints(data []byte) error {
	return a.store.restoreCheckpointBackup(data)
}
func (a *replacementApplier) RestoreConversation(data []byte) error {
	if a.restoreHook != nil {
		if err := a.restoreHook(); err != nil {
			return err
		}
	}
	if a.restoreErr != nil {
		return a.restoreErr
	}
	return a.recordingConversationApplier.RestoreConversation(data)
}

func requireUndoOffer(t *testing.T, s *Store, id string) {
	t.Helper()
	offer, err := s.AvailableUndo()
	if err != nil || offer == nil || offer.TransactionID != id {
		t.Fatalf("undo offer = %+v, err = %v, want %s", offer, err, id)
	}
}

func TestFailedRewindKeepsPreviousUndo(t *testing.T) {
	for _, mode := range []string{"commit", "with_forward", "finalize"} {
		t.Run(mode, func(t *testing.T) {
			s, dir, root, path, previous := committedCodeUndo(t)
			applier := &replacementApplier{store: s}
			plan, err := s.PrepareRewind(0, RewindConversation, 2, 0, true)
			if err != nil {
				t.Fatal(err)
			}
			inject := &InjectFail{Phase: "conversation"}
			var result RewindResult
			if mode == "commit" {
				result, err = s.CommitRewind(plan.PlanID, applier, inject)
			} else {
				if mode == "finalize" {
					inject.Phase = "finalize"
				}
				result, err = s.CommitRewindWithForward(plan.PlanID, []byte("[]"), applier, inject)
			}
			if err == nil || result.OK {
				t.Fatal("injected rewind succeeded")
			}
			requireUndoOffer(t, s, previous.TransactionID)
			if s.NextTurn() != 1 {
				t.Fatal("failed rewind did not restore checkpoints")
			}
			reloaded := New(dir, root)
			requireUndoOffer(t, reloaded, previous.TransactionID)
			if _, err := reloaded.UndoRewind(previous.TransactionID, nil); err != nil {
				t.Fatal(err)
			}
			if got := read(t, path); got != "after" {
				t.Fatalf("undo restored %q", got)
			}
		})
	}
}

func TestPreparationFailureKeepsPreviousUndo(t *testing.T) {
	s, dir, root, path, previous := committedCodeUndo(t)
	plan, err := s.PrepareRewind(0, RewindConversation, 2, 0, true)
	if err != nil {
		t.Fatal(err)
	}
	transactions := filepath.Join(dir, "transactions")
	backup := transactions + "-backup"
	if err := os.Rename(transactions, backup); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(transactions, []byte("blocked"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CommitRewindWithForward(plan.PlanID, []byte("[]"), &replacementApplier{store: s}, nil); err == nil {
		t.Fatal("preparation succeeded despite blocked transaction storage")
	}
	requireUndoOffer(t, s, previous.TransactionID)
	if got := read(t, path); got != "before" {
		t.Fatalf("preparation changed file to %q", got)
	}
	if err := os.Remove(transactions); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(backup, transactions); err != nil {
		t.Fatal(err)
	}
	requireUndoOffer(t, New(dir, root), previous.TransactionID)
}

func TestPendingRewindRecoveryBlocksUndoAndNewWrites(t *testing.T) {
	s, dir, root, _, previous := committedCodeUndo(t)
	applier := &replacementApplier{store: s, restoreErr: errors.New("conversation storage unavailable")}
	plan, err := s.PrepareRewind(0, RewindConversation, 2, 0, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CommitRewindWithForward(plan.PlanID, []byte("[]"), applier, &InjectFail{Phase: "finalize"}); err == nil {
		t.Fatal("rewind succeeded despite failed compensation")
	}
	if offer, err := s.AvailableUndo(); err != nil || offer != nil {
		t.Fatalf("offer while recovery pending = %+v, %v", offer, err)
	}
	if _, err := s.UndoRewind(previous.TransactionID, applier); err == nil {
		t.Fatal("undo accepted pending recovery")
	}
	if err := s.Begin(1, "new turn", 1); err == nil {
		t.Fatal("new turn accepted pending recovery")
	}
	observer := NewMutationObserver(ObserverOptions{Store: s})
	release, err := observer.BeginWrite()
	if release != nil {
		release()
	}
	if err == nil {
		t.Fatal("writer accepted pending recovery")
	}
	reloaded := New(dir, root)
	if offer, err := reloaded.AvailableUndo(); err != nil || offer != nil {
		t.Fatalf("recovered unsafe offer = %+v, %v", offer, err)
	}
	applier.store = reloaded
	reloaded.RecoverTransactionsWithApplier(applier)
	if offer, err := reloaded.AvailableUndo(); err != nil || offer != nil {
		t.Fatalf("offer after failed recovery = %+v, %v", offer, err)
	}
	applier.restoreErr = nil
	reloaded.RecoverTransactionsWithApplier(applier)
	requireUndoOffer(t, reloaded, previous.TransactionID)
}

func TestInterruptedReplacementRecoversPreviousUndo(t *testing.T) {
	s, dir, root, _, previous := committedCodeUndo(t)
	plan, err := s.PrepareRewind(0, RewindConversation, 2, 0, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CommitRewindWithForward(plan.PlanID, []byte("[]"), &replacementApplier{store: s}, &InjectFail{Phase: "after_conversation_before_finalize"}); err == nil {
		t.Fatal("injected interruption succeeded")
	}
	reloaded := New(dir, root)
	if offer, err := reloaded.AvailableUndo(); err != nil || offer != nil {
		t.Fatalf("offer before conversation recovery = %+v, %v", offer, err)
	}
	reloaded.RecoverTransactionsWithApplier(&replacementApplier{store: reloaded})
	requireUndoOffer(t, reloaded, previous.TransactionID)
}

func TestSuccessfulReplacementDoesNotRevivePreviousUndo(t *testing.T) {
	s, dir, root, _, previous := committedCodeUndo(t)
	plan, err := s.PrepareRewind(0, RewindConversation, 2, 0, true)
	if err != nil {
		t.Fatal(err)
	}
	next, err := s.CommitRewindWithForward(plan.PlanID, []byte("[]"), &replacementApplier{store: s}, nil)
	if err != nil {
		t.Fatal(err)
	}
	requireUndoOffer(t, s, next.TransactionID)
	if _, err := s.UndoRewind(previous.TransactionID, nil); err == nil {
		t.Fatal("previous undo remained executable")
	}
	reloaded := New(dir, root)
	requireUndoOffer(t, reloaded, next.TransactionID)
	if _, err := reloaded.UndoRewind(next.TransactionID, &replacementApplier{store: reloaded}); err != nil {
		t.Fatal(err)
	}
	if offer, err := New(dir, root).AvailableUndo(); err != nil || offer != nil {
		t.Fatalf("superseded undo revived = %+v, %v", offer, err)
	}
}

func TestCodeRewindFailureRestoresFilesAndPreviousUndo(t *testing.T) {
	for _, phase := range []string{"finalize", "after_publish_before_progress"} {
		t.Run(phase, func(t *testing.T) {
			root, dir := testenv.TempDir(t), testenv.TempDir(t)
			path := filepath.Join(root, "note.txt")
			write(t, path, "before")
			s := New(dir, root)
			if err := s.Begin(0, "edit", 0); err != nil {
				t.Fatal(err)
			}
			s.CaptureBefore(path, CaptureBeforeOpts{})
			write(t, path, "after")
			if err := s.CaptureAfter(path, CaptureAfterOpts{Seq: 1}); err != nil {
				t.Fatal(err)
			}
			if err := s.Begin(1, "message", 2); err != nil {
				t.Fatal(err)
			}
			applier := &replacementApplier{store: s}
			firstPlan, err := s.PrepareRewind(1, RewindConversation, 1, 2, true)
			if err != nil {
				t.Fatal(err)
			}
			previous, err := s.CommitRewindWithForward(firstPlan.PlanID, []byte("[]"), applier, nil)
			if err != nil {
				t.Fatal(err)
			}
			plan, err := s.PrepareRewind(0, RewindCode, 2, 0, true)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.CommitRewindWithForward(plan.PlanID, applier.conversation, applier, &InjectFail{Phase: phase}); err == nil {
				t.Fatal("injected code rewind succeeded")
			}
			reloaded := New(dir, root)
			requireUndoOffer(t, reloaded, previous.TransactionID)
			if got := read(t, path); got != "after" {
				t.Fatalf("failed code rewind left file %q", got)
			}
			if _, err := reloaded.UndoRewind(previous.TransactionID, &replacementApplier{store: reloaded}); err != nil {
				t.Fatal(err)
			}
			if reloaded.NextTurn() != 2 {
				t.Fatal("previous undo did not restore its checkpoint")
			}
		})
	}
}

func TestInMemoryReplacementRecoveryKeepsPreviousUndo(t *testing.T) {
	root := testenv.TempDir(t)
	path := filepath.Join(root, "note.txt")
	write(t, path, "before")
	s := New("", root)
	if err := s.Begin(0, "edit", 0); err != nil {
		t.Fatal(err)
	}
	s.CaptureBefore(path, CaptureBeforeOpts{})
	write(t, path, "after")
	if err := s.CaptureAfter(path, CaptureAfterOpts{Seq: 1}); err != nil {
		t.Fatal(err)
	}
	firstPlan, err := s.PrepareRewind(0, RewindCode, 1, 0, true)
	if err != nil {
		t.Fatal(err)
	}
	previous, err := s.CommitRewindWithForward(firstPlan.PlanID, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := s.PrepareRewind(0, RewindConversation, 2, 0, true)
	if err != nil {
		t.Fatal(err)
	}
	applier := &replacementApplier{store: s}
	if _, err := s.CommitRewindWithForward(plan.PlanID, []byte("[]"), applier, &InjectFail{Phase: "after_conversation_before_finalize"}); err == nil {
		t.Fatal("injected interruption succeeded")
	}
	s.RecoverTransactionsWithApplier(applier)
	requireUndoOffer(t, s, previous.TransactionID)
	if _, err := s.UndoRewind(previous.TransactionID, nil); err != nil {
		t.Fatal(err)
	}
	if got := read(t, path); got != "after" {
		t.Fatalf("undo restored %q", got)
	}
}

func TestUnrecordedCompensationKeepsUndoBlocked(t *testing.T) {
	s, dir, _, _, previous := committedCodeUndo(t)
	transactions := filepath.Join(dir, "transactions")
	backup := transactions + "-backup"
	applier := &replacementApplier{store: s, restoreHook: func() error {
		if err := os.Rename(transactions, backup); err != nil {
			return err
		}
		return os.WriteFile(transactions, []byte("blocked"), 0o644)
	}}
	plan, err := s.PrepareRewind(0, RewindConversation, 2, 0, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CommitRewindWithForward(plan.PlanID, []byte("[]"), applier, &InjectFail{Phase: "finalize"}); err == nil {
		t.Fatal("rewind succeeded despite unrecorded compensation")
	}
	if offer, err := s.AvailableUndo(); err != nil || offer != nil {
		t.Fatalf("offer after failed abort persistence = %+v, %v", offer, err)
	}
	if err := s.Begin(1, "new turn", 1); !errors.Is(err, ErrUndoRecoveryPending) {
		t.Fatalf("new turn error = %v", err)
	}
	if err := os.Remove(transactions); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(backup, transactions); err != nil {
		t.Fatal(err)
	}
	applier.restoreHook = nil
	s.RecoverTransactionsWithApplier(applier)
	requireUndoOffer(t, s, previous.TransactionID)
}

func TestUnreadableRecoveryRecordsBlockNewActivity(t *testing.T) {
	for _, mode := range []string{"directory", "manifest"} {
		t.Run(mode, func(t *testing.T) {
			s, dir, root, _, previous := committedCodeUndo(t)
			plan, err := s.PrepareRewind(0, RewindConversation, 2, 0, true)
			if err != nil {
				t.Fatal(err)
			}
			result, err := s.CommitRewindWithForward(plan.PlanID, []byte("[]"), &replacementApplier{store: s}, &InjectFail{Phase: "after_conversation_before_finalize"})
			if err == nil {
				t.Fatal("injected interruption succeeded")
			}
			transactions := filepath.Join(dir, "transactions")
			manifest := filepath.Join(transactions, result.TransactionID+".json")
			var saved []byte
			if mode == "directory" {
				if err := os.Rename(transactions, transactions+"-backup"); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(transactions, []byte("blocked"), 0o644); err != nil {
					t.Fatal(err)
				}
			} else {
				saved, err = os.ReadFile(manifest)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(manifest, []byte("invalid JSON"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			reloaded := New(dir, root)
			if err := reloaded.Begin(1, "new turn", 1); !errors.Is(err, ErrUndoRecoveryPending) {
				t.Fatalf("new turn error = %v", err)
			}
			if offer, err := reloaded.AvailableUndo(); err != nil || offer != nil {
				t.Fatalf("unsafe offer = %+v, %v", offer, err)
			}
			if mode == "directory" {
				if err := os.Remove(transactions); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(transactions+"-backup", transactions); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(manifest, saved, 0o644); err != nil {
				t.Fatal(err)
			}
			reloaded.RecoverTransactionsWithApplier(&replacementApplier{store: reloaded})
			requireUndoOffer(t, reloaded, previous.TransactionID)
		})
	}
}
