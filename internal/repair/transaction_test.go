package repair

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/config"
)

// TestUndoLastRepairKeepsBackupUntilProgressPersisted pins the crash-window
// contract: a change that was fully restored but whose progress record never
// reached disk (simulated by an unmarked change whose target already matches
// the backup) must remain retryable, and backups are only removed after the
// per-change progress is persisted.
func TestUndoLastRepairKeepsBackupUntilProgressPersisted(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	windowPath := filepath.Join(home, "desktop-window.json")
	quarantine := windowPath + ".reasonix-rebuild-20260714T000000Z"
	// Simulate a crash after the restore copy but before markUndone: the
	// target already holds the restored bytes, the backup still exists, and
	// the change is not marked undone.
	for path, body := range map[string]string{
		windowPath: "old-window",
		quarantine: "old-window",
	} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	tx := newRepairTransaction(time.Now())
	tx.Changes = []RepairChange{{Scope: "derived:window", TargetPath: windowPath, PreviousPath: quarantine}}
	if err := persistRepairTransaction(tx); err != nil {
		t.Fatal(err)
	}
	undone, err := UndoLastRepair()
	if err != nil {
		t.Fatalf("retry after simulated crash failed: %v", err)
	}
	if !undone.Undone {
		t.Fatalf("transaction not marked undone: %+v", undone)
	}
	if got, _ := os.ReadFile(windowPath); string(got) != "old-window" {
		t.Fatalf("window state = %q", got)
	}
	if _, err := os.Stat(quarantine); !os.IsNotExist(err) {
		t.Fatalf("backup not cleaned up after completed undo: %v", err)
	}
}

// TestUndoLastRepairRestoresSymlink pins that undoing a repair of a
// symlink-managed config (dotfiles setups) restores the symlink itself: the
// quarantine rename moved the link, so undo must recreate a link, not
// materialize the followed content as a regular file.
func TestUndoLastRepairRestoresSymlink(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	configPath := config.UserConfigPath()
	linkTarget := filepath.Join(t.TempDir(), "dotfiles-config.toml")
	if err := os.WriteFile(linkTarget, []byte("linked"), 0o600); err != nil {
		t.Fatal(err)
	}
	quarantine := configPath + ".reasonix-quarantine-20260714T000000Z"
	// The repair's os.Rename moves the link itself into quarantine.
	if err := os.Symlink(linkTarget, quarantine); err != nil {
		t.Fatal(err)
	}
	// The repair then materialized a regular replacement config.
	if err := os.WriteFile(configPath, []byte("repaired"), 0o600); err != nil {
		t.Fatal(err)
	}
	tx := newRepairTransaction(time.Now())
	tx.Changes = []RepairChange{{Scope: "global", TargetPath: configPath, PreviousPath: quarantine}}
	if err := persistRepairTransaction(tx); err != nil {
		t.Fatal(err)
	}
	if _, err := UndoLastRepair(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("undo materialized a regular file, want symlink (mode %v)", info.Mode())
	}
	if got, err := os.Readlink(configPath); err != nil || got != linkTarget {
		t.Fatalf("restored link target = %q (%v), want %q", got, err, linkTarget)
	}
	if got, _ := os.ReadFile(configPath); string(got) != "linked" {
		t.Fatalf("config content through link = %q", got)
	}
	if _, err := os.Lstat(quarantine); !os.IsNotExist(err) {
		t.Fatalf("quarantined link not cleaned up: %v", err)
	}
}

// A dangling quarantined symlink must still be restorable: preflight and
// restore must not follow the link when judging its presence.
func TestUndoLastRepairRestoresDanglingSymlink(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	configPath := config.UserConfigPath()
	linkTarget := filepath.Join(t.TempDir(), "missing-config.toml")
	quarantine := configPath + ".reasonix-quarantine-20260714T000000Z"
	if err := os.Symlink(linkTarget, quarantine); err != nil {
		t.Fatal(err)
	}
	tx := newRepairTransaction(time.Now())
	tx.Changes = []RepairChange{{Scope: "global", TargetPath: configPath, PreviousPath: quarantine}}
	if err := persistRepairTransaction(tx); err != nil {
		t.Fatal(err)
	}
	if _, err := UndoLastRepair(); err != nil {
		t.Fatal(err)
	}
	if got, err := os.Readlink(configPath); err != nil || got != linkTarget {
		t.Fatalf("restored dangling link target = %q (%v), want %q", got, err, linkTarget)
	}
}

// TestUndoLastRepairKeepsDistinctRedoCopiesForSharedTarget pins that one undo
// touching the same target twice (quarantine + snapshot restore) retains a
// separate redo copy per change instead of silently overwriting the first.
func TestUndoLastRepairKeepsDistinctRedoCopiesForSharedTarget(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	configPath := config.UserConfigPath()
	quarantine := configPath + ".reasonix-quarantine-20260714T000000Z"
	restoreBackup := filepath.Join(home, "repair", "restore-backups", "repair-2.toml")
	if err := os.MkdirAll(filepath.Dir(restoreBackup), 0o700); err != nil {
		t.Fatal(err)
	}
	for path, body := range map[string]string{
		configPath:    "current",
		quarantine:    "original",
		restoreBackup: "pre-restore",
	} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	tx := newRepairTransaction(time.Now())
	tx.Changes = []RepairChange{
		{Scope: "global", TargetPath: configPath, PreviousPath: quarantine},
		{Scope: "global", TargetPath: configPath, PreviousPath: restoreBackup},
	}
	if err := persistRepairTransaction(tx); err != nil {
		t.Fatal(err)
	}
	if _, err := UndoLastRepair(); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(configPath); string(got) != "original" {
		t.Fatalf("config after undo = %q", got)
	}
	redos, err := filepath.Glob(configPath + ".reasonix-redo-*")
	if err != nil {
		t.Fatal(err)
	}
	if len(redos) != 2 {
		t.Fatalf("redo copies = %v, want one per change", redos)
	}
}

// TestUndoLastRepairResumesAfterPartialFailure pins the recoverable-undo
// contract: a multi-change undo that fails partway persists per-change
// progress, and a retry finishes the remaining changes instead of failing the
// preflight on the consumed backups of the changes already restored.
func TestUndoLastRepairResumesAfterPartialFailure(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	configPath := config.UserConfigPath()
	windowPath := filepath.Join(home, "desktop-window.json")
	windowQuarantine := windowPath + ".reasonix-rebuild-20260714T000000Z"
	restoreBackup := filepath.Join(home, "repair", "restore-backups", "repair-1.toml")
	if err := os.MkdirAll(filepath.Dir(restoreBackup), 0o700); err != nil {
		t.Fatal(err)
	}
	for path, body := range map[string]string{
		configPath:       "current-config",
		windowPath:       "current-window",
		windowQuarantine: "old-window",
	} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// A directory at the restore-backup path passes the Stat preflight but
	// fails the read during restore, aborting the undo after the derived-state
	// change has already been reverted.
	if err := os.MkdirAll(restoreBackup, 0o700); err != nil {
		t.Fatal(err)
	}
	tx := newRepairTransaction(time.Now())
	tx.Changes = []RepairChange{
		{Scope: "global", TargetPath: configPath, PreviousPath: restoreBackup},
		{Scope: "derived:window", TargetPath: windowPath, PreviousPath: windowQuarantine},
	}
	if err := persistRepairTransaction(tx); err != nil {
		t.Fatal(err)
	}

	if _, err := UndoLastRepair(); err == nil {
		t.Fatal("undo succeeded despite unreadable restore backup")
	}
	partial, err := ReadLastRepair()
	if err != nil {
		t.Fatal(err)
	}
	if partial.Undone || !partial.Changes[1].Undone || partial.Changes[0].Undone {
		t.Fatalf("partial undo progress not persisted: %+v", partial)
	}
	if got, _ := os.ReadFile(windowPath); string(got) != "old-window" {
		t.Fatalf("derived state not restored before failure: %q", got)
	}

	// Repair the backup and retry: the already-undone change must be skipped
	// even though its quarantine file was consumed by the first attempt.
	if err := os.Remove(restoreBackup); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(restoreBackup, []byte("old-config"), 0o600); err != nil {
		t.Fatal(err)
	}
	undone, err := UndoLastRepair()
	if err != nil {
		t.Fatal(err)
	}
	if !undone.Undone {
		t.Fatalf("transaction not marked undone: %+v", undone)
	}
	if got, _ := os.ReadFile(configPath); string(got) != "old-config" {
		t.Fatalf("config not restored on retry: %q", got)
	}
	if got, _ := os.ReadFile(windowPath); string(got) != "old-window" {
		t.Fatalf("derived state clobbered by retry: %q", got)
	}
}
