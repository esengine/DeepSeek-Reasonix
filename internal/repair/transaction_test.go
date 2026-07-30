package repair

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/config"
)

func TestReadLastRepairAcceptsLegacyTransactionWithoutJournalFields(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	target := filepath.Join(home, "desktop-window.json")
	previous := target + ".reasonix-rebuild-20260729T000000Z"
	legacy := map[string]any{
		"schemaVersion": 1,
		"id":            "repair-legacy",
		"createdAt":     "2026-07-29T00:00:00Z",
		"changes": []map[string]any{{
			"scope":           "derived:window",
			"targetPath":      target,
			"previousPath":    previous,
			"previousStateId": strings.Repeat("a", 64),
		}},
	}
	b, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(repairTransactionPath()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(repairTransactionPath(), b, 0o600); err != nil {
		t.Fatal(err)
	}
	tx, err := ReadLastRepair()
	if err != nil || tx.ID != "repair-legacy" || len(tx.Changes) != 1 {
		t.Fatalf("legacy transaction = %+v, %v", tx, err)
	}
}

func TestReadLastRepairRejectsPendingOnlyJournalFields(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	target := filepath.Join(home, "desktop-window.json")
	tx := newRepairTransaction(time.Now())
	tx.PreparedLastRepairStateID = strings.Repeat("a", 64)
	tx.Changes = []RepairChange{{
		Scope:           "derived:window",
		TargetPath:      target,
		PreviousPath:    target + ".reasonix-rebuild-20260729T000000Z",
		PreviousStateID: strings.Repeat("b", 64),
		Prepared:        true,
	}}
	b, err := json.Marshal(tx)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(repairTransactionPath()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(repairTransactionPath(), b, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadLastRepair(); err == nil ||
		!strings.Contains(err.Error(), "pending-only state") {
		t.Fatalf("ReadLastRepair error = %v", err)
	}
}

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
	tx.Changes = []RepairChange{repairChangeForPrevious("derived:window", windowPath, quarantine)}
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

func TestUndoLastRepairRejectsTransactionReplacedWhileWaitingForLock(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	windowPath := filepath.Join(home, "desktop-window.json")
	windowBackup := windowPath + ".reasonix-rebuild-20260729T000000Z"
	tabsPath := filepath.Join(home, "desktop-tabs.json")
	tabsBackup := tabsPath + ".reasonix-rebuild-20260729T000001Z"
	for path, body := range map[string]string{
		windowPath:   "current-window",
		windowBackup: "previous-window",
		tabsPath:     "current-tabs",
		tabsBackup:   "previous-tabs",
	} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	first := newRepairTransaction(time.Now())
	first.Changes = []RepairChange{repairChangeForPrevious("derived:window", windowPath, windowBackup)}
	if err := persistRepairTransaction(first); err != nil {
		t.Fatal(err)
	}
	second := newRepairTransaction(time.Now().Add(time.Second))
	second.Changes = []RepairChange{repairChangeForPrevious("derived:tabs", tabsPath, tabsBackup)}

	transactionKey := repairMutationTestKey(repairTransactionPath())
	originalHook := repairMutationBeforeLock
	var replaceErr error
	replaced := false
	repairMutationBeforeLock = func(paths []string) {
		if replaced || len(paths) != 1 || paths[0] != transactionKey {
			return
		}
		replaced = true
		replaceErr = persistRepairTransaction(second)
	}
	t.Cleanup(func() { repairMutationBeforeLock = originalHook })

	if _, err := UndoLastRepair(); err == nil ||
		!strings.Contains(err.Error(), "transaction changed while waiting") {
		t.Fatalf("undo replaced transaction = %v", err)
	}
	if replaceErr != nil {
		t.Fatalf("replace last repair: %v", replaceErr)
	}
	if got, err := os.ReadFile(windowPath); err != nil || string(got) != "current-window" {
		t.Fatalf("first target changed: %q, %v", got, err)
	}
	if got, err := os.ReadFile(tabsPath); err != nil || string(got) != "current-tabs" {
		t.Fatalf("second target changed: %q, %v", got, err)
	}
	last, err := ReadLastRepair()
	if err != nil || last.ID != second.ID {
		t.Fatalf("last repair = %+v, %v; want replacement", last, err)
	}
}

func TestUndoLastRepairRejectsLegacyBackupWithoutStateIdentity(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	windowPath := filepath.Join(home, "desktop-window.json")
	backup := windowPath + ".reasonix-rebuild-20260729T000000Z"
	if err := os.WriteFile(windowPath, []byte("current-window"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backup, []byte("previous-window"), 0o600); err != nil {
		t.Fatal(err)
	}
	tx := newRepairTransaction(time.Now())
	tx.Changes = []RepairChange{{
		Scope:        "derived:window",
		TargetPath:   windowPath,
		PreviousPath: backup,
	}}
	if err := persistRepairTransaction(tx); err != nil {
		t.Fatal(err)
	}

	if _, err := UndoLastRepair(); err == nil ||
		!strings.Contains(err.Error(), "legacy transaction cannot be undone safely") {
		t.Fatalf("legacy undo error = %v", err)
	}
	if got, err := os.ReadFile(windowPath); err != nil || string(got) != "current-window" {
		t.Fatalf("legacy target changed: %q, %v", got, err)
	}
	if got, err := os.ReadFile(backup); err != nil || string(got) != "previous-window" {
		t.Fatalf("legacy backup changed: %q, %v", got, err)
	}
}

func TestUndoLastRepairRejectsBytesDifferentFromVerifiedBackup(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	windowPath := filepath.Join(home, "desktop-window.json")
	quarantine := windowPath + ".reasonix-rebuild-20260714T000000Z"
	if err := os.WriteFile(windowPath, []byte("current-window"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(quarantine, []byte("confirmed-window"), 0o600); err != nil {
		t.Fatal(err)
	}
	tx := newRepairTransaction(time.Now())
	tx.Changes = []RepairChange{repairChangeForPrevious("derived:window", windowPath, quarantine)}
	if err := persistRepairTransaction(tx); err != nil {
		t.Fatal(err)
	}

	originalRead := readRepairPreviousFile
	readRepairPreviousFile = func(path string) ([]byte, error) {
		if path == quarantine {
			return []byte("transient-unconfirmed-window"), nil
		}
		return os.ReadFile(path)
	}
	t.Cleanup(func() { readRepairPreviousFile = originalRead })

	if _, err := UndoLastRepair(); err == nil ||
		!strings.Contains(err.Error(), "previous file bytes changed") {
		t.Fatalf("undo error = %v, want exact-read state rejection", err)
	}
	if got, err := os.ReadFile(windowPath); err != nil || string(got) != "current-window" {
		t.Fatalf("current target was not compensated: %q, %v", got, err)
	}
	if got, err := os.ReadFile(quarantine); err != nil || string(got) != "confirmed-window" {
		t.Fatalf("confirmed backup changed: %q, %v", got, err)
	}
	last, err := ReadLastRepair()
	if err != nil || last.Undone || last.Changes[0].Undone {
		t.Fatalf("failed undo advanced transaction: %+v, %v", last, err)
	}
}

func TestUndoLastRepairRejectsLinkDifferentFromVerifiedBackup(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	configPath := config.UserConfigPath()
	linkTarget := filepath.Join(t.TempDir(), "confirmed-config.toml")
	if err := os.WriteFile(linkTarget, []byte("confirmed"), 0o600); err != nil {
		t.Fatal(err)
	}
	quarantine := configPath + ".reasonix-quarantine-20260714T000000Z"
	if err := os.Symlink(linkTarget, quarantine); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("current"), 0o600); err != nil {
		t.Fatal(err)
	}
	tx := newRepairTransaction(time.Now())
	tx.Changes = []RepairChange{repairChangeForPrevious("global", configPath, quarantine)}
	if err := persistRepairTransaction(tx); err != nil {
		t.Fatal(err)
	}

	originalReadlink := readRepairPreviousLink
	readRepairPreviousLink = func(path string) (string, error) {
		if path == quarantine {
			return filepath.Join(t.TempDir(), "transient-config.toml"), nil
		}
		return os.Readlink(path)
	}
	t.Cleanup(func() { readRepairPreviousLink = originalReadlink })

	if _, err := UndoLastRepair(); err == nil ||
		!strings.Contains(err.Error(), "previous link read changed") {
		t.Fatalf("undo error = %v, want exact-link state rejection", err)
	}
	if got, err := os.ReadFile(configPath); err != nil || string(got) != "current" {
		t.Fatalf("current config was not compensated: %q, %v", got, err)
	}
	if got, err := os.Readlink(quarantine); err != nil || got != linkTarget {
		t.Fatalf("confirmed symlink changed: %q, %v", got, err)
	}
}

func TestReadLastRepairRejectsRestoreBackupParentSymlinkEscape(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	restoreRoot := filepath.Join(home, "repair", "restore-backups")
	if err := os.MkdirAll(filepath.Dir(restoreRoot), 0o700); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, restoreRoot); err != nil {
		t.Fatal(err)
	}
	previous := filepath.Join(restoreRoot, "forged.toml")
	if err := os.WriteFile(filepath.Join(outside, "forged.toml"), []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	tx := newRepairTransaction(time.Now())
	tx.Changes = []RepairChange{{
		Scope:        "global",
		TargetPath:   config.UserConfigPath(),
		PreviousPath: previous,
	}}
	if err := persistRepairTransaction(tx); err != nil {
		t.Fatal(err)
	}

	if _, err := ReadLastRepair(); err == nil ||
		!strings.Contains(err.Error(), "previous path is invalid") {
		t.Fatalf("ReadLastRepair error = %v, want parent symlink escape rejection", err)
	}
	if got, err := os.ReadFile(filepath.Join(outside, "forged.toml")); err != nil || string(got) != "outside" {
		t.Fatalf("outside node changed: %q, %v", got, err)
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
	tx.Changes = []RepairChange{repairChangeForPrevious("global", configPath, quarantine)}
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
	tx.Changes = []RepairChange{repairChangeForPrevious("global", configPath, quarantine)}
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
		repairChangeForPrevious("global", configPath, quarantine),
		repairChangeForPrevious("global", configPath, restoreBackup),
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
		restoreBackup:    "old-config",
	} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	tx := newRepairTransaction(time.Now())
	tx.Changes = []RepairChange{
		repairChangeForPrevious("global", configPath, restoreBackup),
		repairChangeForPrevious("derived:window", windowPath, windowQuarantine),
	}
	if err := persistRepairTransaction(tx); err != nil {
		t.Fatal(err)
	}

	originalRead := readRepairPreviousFile
	failConfigRead := true
	readRepairPreviousFile = func(path string) ([]byte, error) {
		if path == restoreBackup && failConfigRead {
			failConfigRead = false
			return nil, os.ErrPermission
		}
		return os.ReadFile(path)
	}
	t.Cleanup(func() { readRepairPreviousFile = originalRead })

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

	// Retry with the transient read failure gone: the already-undone change
	// must be skipped even though its quarantine file was consumed.
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
