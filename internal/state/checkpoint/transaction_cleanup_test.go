package checkpoint

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestRewindCleansCommittedBackupsAndKeepsUndo(t *testing.T) {
	for _, persisted := range []bool{false, true} {
		for _, action := range []string{"create", "modify", "delete"} {
			name := action + "/memory"
			if persisted {
				name = action + "/disk"
			}
			t.Run(name, func(t *testing.T) {
				root := testenv.TempDir(t)
				dir := ""
				if persisted {
					dir = filepath.Join(testenv.TempDir(t), "sess.ckpt")
				}
				path := filepath.Join(root, "a.txt")
				unrelated := filepath.Join(root, "unrelated.bak")
				write(t, unrelated, "keep")
				if action != "create" {
					write(t, path, "before")
				}
				s := New(dir, root)
				s.Begin(0, action, 0)
				s.CaptureBefore(path, CaptureBeforeOpts{Source: CaptureBeforeMutation})
				if action == "delete" {
					if err := os.Remove(path); err != nil {
						t.Fatal(err)
					}
				} else {
					write(t, path, "after")
				}
				s.CaptureAfter(path, CaptureAfterOpts{Seq: 1, Source: CaptureAfterMutation})
				plan, err := s.PrepareRewind(0, RewindCode, 1, 0, false)
				if err != nil {
					t.Fatal(err)
				}
				result, err := s.CommitRewind(plan.PlanID, nil, nil)
				if err != nil || !result.OK {
					t.Fatalf("rewind: result=%+v err=%v", result, err)
				}
				if action == "create" {
					if _, err := os.Stat(path); !os.IsNotExist(err) {
						t.Fatalf("created file remains after rewind: %v", err)
					}
				} else if got := read(t, path); got != "before" {
					t.Fatalf("rewound file = %q, want before", got)
				}
				requireNoTransactionBackups(t, root)
				if persisted {
					s = New(dir, root)
				}
				undo, err := s.UndoRewind(result.TransactionID, nil)
				if err != nil || !undo.OK {
					t.Fatalf("undo: result=%+v err=%v", undo, err)
				}
				if action == "delete" {
					if _, err := os.Stat(path); !os.IsNotExist(err) {
						t.Fatalf("deleted file remains after undo: %v", err)
					}
				} else if got := read(t, path); got != "after" {
					t.Fatalf("undo restored %q, want after", got)
				}
				requireNoTransactionBackups(t, root)
				if got := read(t, unrelated); got != "keep" {
					t.Fatalf("unrelated backup changed: %q", got)
				}
			})
		}
	}
}

func TestRewindKeepsBackupUntilFinalized(t *testing.T) {
	for _, phase := range []string{"finalize", "after_conversation_before_finalize"} {
		t.Run(phase, func(t *testing.T) {
			root := testenv.TempDir(t)
			dir := filepath.Join(testenv.TempDir(t), "sess.ckpt")
			path := filepath.Join(root, "created.txt")
			s := New(dir, root)
			s.Begin(0, "create", 1)
			s.CaptureBefore(path, CaptureBeforeOpts{Source: CaptureBeforeMutation})
			write(t, path, "created")
			s.CaptureAfter(path, CaptureAfterOpts{Seq: 1, Source: CaptureAfterMutation})
			plan, err := s.PrepareRewind(0, RewindBoth, 1, 1, true)
			if err != nil {
				t.Fatal(err)
			}
			applier := &recordingConversationApplier{}
			result, err := s.CommitRewindWithForward(plan.PlanID, []byte(`["conversation"]`), applier, &InjectFail{Phase: phase})
			if err == nil {
				t.Fatal("expected failure before commit")
			}
			if phase == "after_conversation_before_finalize" {
				var tx TransactionManifest
				if err := readJSONFile(s.txManifestPath(result.TransactionID), &tx); err != nil {
					t.Fatal(err)
				}
				if tx.State != TxCommitting || len(tx.Targets) != 1 {
					t.Fatalf("pending transaction = %+v", tx)
				}
				if got := read(t, tx.Targets[0].BackupPath); got != "created" {
					t.Fatalf("pending backup = %q, want created", got)
				}
				New(dir, root).RecoverTransactionsWithApplier(applier)
			}
			if got := read(t, path); got != "created" {
				t.Fatalf("failed rewind did not restore created file: %q", got)
			}
			requireNoTransactionBackups(t, root)
		})
	}
}

func requireNoTransactionBackups(t *testing.T, root string) {
	t.Helper()
	backups, err := filepath.Glob(filepath.Join(root, ".*.reasonix-*.bak"))
	if err != nil || len(backups) != 0 {
		t.Fatalf("transaction backups remain: %v err=%v", backups, err)
	}
}
