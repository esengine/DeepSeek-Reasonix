//go:build darwin

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"reasonix/internal/repair"
)

// duplicateMacHandoffFD gives the in-process helper the same exclusive FD
// ownership it has in production after exec.ExtraFiles. Passing File.Fd()
// directly would leave two os.File wrappers owning one descriptor; the stale
// test wrapper's finalizer could later close an unrelated descriptor that
// reused the same number, corrupting another test's TempDir cleanup.
func duplicateMacHandoffFD(t *testing.T, file *os.File) int {
	t.Helper()
	fd, err := unix.Dup(int(file.Fd()))
	if err != nil {
		t.Fatalf("duplicate handoff fd: %v", err)
	}
	return fd
}

func installMacHandoffTestDeps(
	t *testing.T,
	tx *repair.UpdateTransaction,
	pendingPath string,
	logPath string,
	claim func(string, string, string, time.Duration) (*repair.UpdateTransaction, func(), error),
) {
	t.Helper()
	if tx.HandoffAppTreeID == "" {
		digest, err := repair.AppBundleTreeDigest(tx.HandoffAppPath)
		if err != nil {
			t.Fatal(err)
		}
		tx.HandoffAppTreeID = digest
	}
	if tx.BackupTreeID == "" {
		digest, err := repair.AppBundleTreeDigest(tx.TargetPath)
		if err != nil {
			t.Fatal(err)
		}
		tx.BackupTreeID = digest
	}
	if tx.HandoffStagingTreeID == "" {
		digest, err := repair.AppBundleTreeDigest(tx.HandoffStagingPath)
		if err != nil {
			t.Fatal(err)
		}
		tx.HandoffStagingTreeID = digest
	}
	originalRead := readMacUpdateHandoff
	originalClaim := claimMacUpdateHandoff
	originalCancel := cancelMacUpdateHandoff
	originalClear := clearMacUpdateHandoff
	originalVerify := verifyMacHandoffApp
	originalCleanupStaging := cleanupMacHandoffStaging
	originalCleanupReplacement := cleanupMacHandoffReplacement
	originalCopy := macHandoffCopy
	originalLogPath := macHandoffLogPath
	readMacUpdateHandoff = func() (*repair.UpdateTransaction, error) {
		copy := *tx
		return &copy, nil
	}
	if claim == nil {
		claim = func(string, string, string, time.Duration) (*repair.UpdateTransaction, func(), error) {
			copy := *tx
			return &copy, func() {}, nil
		}
	}
	claimMacUpdateHandoff = claim
	cancelMacUpdateHandoff = func(*repair.UpdateTransaction, time.Duration) (*repair.UpdateTransaction, error) {
		copy := *tx
		_ = os.Remove(pendingPath)
		return &copy, nil
	}
	clearMacUpdateHandoff = func(*repair.UpdateTransaction) error {
		return os.Remove(pendingPath)
	}
	verifyMacHandoffApp = func(string) error { return nil }
	cleanupMacHandoffStaging = func(tx *repair.UpdateTransaction) error {
		return os.RemoveAll(tx.HandoffStagingPath)
	}
	cleanupMacHandoffReplacement = func(_ *repair.UpdateTransaction, path string) error {
		return os.RemoveAll(path)
	}
	macHandoffCopy = func(oldPath, newPath string) error {
		return exec.Command("ditto", oldPath, newPath).Run()
	}
	macHandoffLogPath = func() string { return logPath }
	t.Cleanup(func() {
		readMacUpdateHandoff = originalRead
		claimMacUpdateHandoff = originalClaim
		cancelMacUpdateHandoff = originalCancel
		clearMacUpdateHandoff = originalClear
		verifyMacHandoffApp = originalVerify
		cleanupMacHandoffStaging = originalCleanupStaging
		cleanupMacHandoffReplacement = originalCleanupReplacement
		macHandoffCopy = originalCopy
		macHandoffLogPath = originalLogPath
	})
}

func macHandoffConfigFor(tx *repair.UpdateTransaction) macUpdateHandoffConfig {
	return macUpdateHandoffConfig{
		ToVersion:     tx.ToVersion,
		CreatedAt:     tx.CreatedAt,
		TransactionID: repair.UpdateTransactionID(tx),
	}
}

func TestMacUpdateHandoffWaitsForExactProcessAndRollsBackLaunchFailure(t *testing.T) {
	root := t.TempDir()
	oldApp := filepath.Join(root, "Reasonix.app")
	newApp := filepath.Join(root, "staging", "Reasonix.app")
	backupApp := oldApp + ".reasonix-update-backup"
	pending := filepath.Join(root, "pending.json")
	logPath := filepath.Join(root, "update.log")
	for _, dir := range []string{oldApp, newApp} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(oldApp, "marker"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(newApp, "marker"), []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pending, []byte("pending"), 0o600); err != nil {
		t.Fatal(err)
	}
	tx := &repair.UpdateTransaction{
		ToVersion:          "v2",
		CreatedAt:          "2026-07-28T00:00:00Z",
		TargetKind:         "app-bundle",
		TargetPath:         oldApp,
		BackupPath:         backupApp,
		HandoffAppPath:     newApp,
		HandoffStagingPath: filepath.Dir(newApp),
		HandoffOwnerPID:    99999999,
	}
	installMacHandoffTestDeps(t, tx, pending, logPath, nil)

	originalOpen := openCommand
	openCommand = func(args ...string) *exec.Cmd {
		// Force LaunchServices rejection so handoff rolls back under the lock.
		return exec.Command("/bin/sh", "-c", "exit 1")
	}
	t.Cleanup(func() { openCommand = originalOpen })

	code := runMacUpdateHandoff(macHandoffConfigFor(tx))
	if code == 0 {
		t.Fatal("handoff should fail when LaunchServices rejects the replacement")
	}

	marker, err := os.ReadFile(filepath.Join(oldApp, "marker"))
	if err != nil {
		t.Fatalf("read restored marker: %v", err)
	}
	if string(marker) != "old" {
		t.Fatalf("restored marker = %q, want old", marker)
	}
	if _, err := os.Stat(pending); !os.IsNotExist(err) {
		t.Fatalf("pending transaction was not cleared: %v", err)
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	logText := string(logData)
	if !strings.Contains(logText, "PID 99999999") || !strings.Contains(logText, "rolling back") {
		t.Fatalf("handoff log lacks PID/rollback diagnostics: %s", logText)
	}
}

func TestMacUpdateHandoffRetainsRecoveryStateWhenRollbackRestoreFails(t *testing.T) {
	root := t.TempDir()
	oldApp := filepath.Join(root, "Reasonix.app")
	newApp := filepath.Join(root, "staging", "Reasonix.app")
	backupApp := oldApp + ".reasonix-update-backup"
	pending := filepath.Join(root, "pending.json")
	logPath := filepath.Join(root, "update.log")
	for _, dir := range []string{oldApp, newApp} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(oldApp, "marker"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(newApp, "marker"), []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pending, []byte("pending"), 0o600); err != nil {
		t.Fatal(err)
	}
	tx := &repair.UpdateTransaction{
		ToVersion:          "v2",
		CreatedAt:          "2026-07-28T00:00:00Z",
		TargetKind:         "app-bundle",
		TargetPath:         oldApp,
		BackupPath:         backupApp,
		HandoffAppPath:     newApp,
		HandoffStagingPath: filepath.Dir(newApp),
		HandoffOwnerPID:    99999999,
	}
	installMacHandoffTestDeps(t, tx, pending, logPath, nil)

	originalOpen := openCommand
	openCalls := 0
	openCommand = func(args ...string) *exec.Cmd {
		openCalls++
		return exec.Command("/bin/sh", "-c", "exit 1")
	}
	t.Cleanup(func() { openCommand = originalOpen })
	originalRename := macHandoffRename
	macHandoffRename = func(oldPath, newPath string) error {
		if oldPath == backupApp && newPath == oldApp {
			return fmt.Errorf("injected restore failure")
		}
		return os.Rename(oldPath, newPath)
	}
	t.Cleanup(func() { macHandoffRename = originalRename })

	code := runMacUpdateHandoff(macHandoffConfigFor(tx))
	if code == 0 {
		t.Fatal("handoff should fail when rollback cannot restore the backup")
	}
	if _, err := os.Stat(pending); err != nil {
		t.Fatalf("failed rollback removed pending recovery state: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(backupApp, "marker")); err != nil || string(got) != "old" {
		t.Fatalf("backup recovery bundle = %q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Dir(newApp)); err != nil {
		t.Fatalf("failed rollback removed staging recovery state: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(oldApp, "marker")); err != nil || string(got) != "new" {
		t.Fatalf("failed replacement was not compensated back to live path: %q, %v", got, err)
	}
	if openCalls != 1 {
		t.Fatalf("open calls = %d, want only the failed replacement launch", openCalls)
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(logData), "failed to restore backup bundle") {
		t.Fatalf("handoff log lacks restore failure: %s", logData)
	}
}

func TestMacUpdateHandoffRestoresOriginalWhenReplacementChangesDuringRollback(t *testing.T) {
	root := t.TempDir()
	oldApp := filepath.Join(root, "Reasonix.app")
	newApp := filepath.Join(root, "staging", "Reasonix.app")
	backupApp := oldApp + ".reasonix-update-backup"
	failedApp := ""
	pending := filepath.Join(root, "pending.json")
	for _, dir := range []string{oldApp, newApp} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(oldApp, "marker"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(newApp, "marker"), []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pending, []byte("pending"), 0o600); err != nil {
		t.Fatal(err)
	}
	tx := &repair.UpdateTransaction{
		ToVersion:          "v2",
		CreatedAt:          "2026-07-28T00:00:00Z",
		TargetKind:         "app-bundle",
		TargetPath:         oldApp,
		BackupPath:         backupApp,
		HandoffAppPath:     newApp,
		HandoffStagingPath: filepath.Dir(newApp),
		HandoffOwnerPID:    99999999,
	}
	installMacHandoffTestDeps(t, tx, pending, filepath.Join(root, "update.log"), nil)

	originalOpen := openCommand
	openCommand = func(args ...string) *exec.Cmd {
		return exec.Command("/bin/sh", "-c", "exit 1")
	}
	t.Cleanup(func() { openCommand = originalOpen })
	originalRename := macHandoffRename
	macHandoffRename = func(oldPath, newPath string) error {
		if err := originalRename(oldPath, newPath); err != nil {
			return err
		}
		if oldPath == oldApp && strings.Contains(newPath, ".reasonix-update-failed-") {
			failedApp = newPath
			return os.WriteFile(filepath.Join(newPath, "marker"), []byte("changed-after-publish"), 0o600)
		}
		return nil
	}
	t.Cleanup(func() { macHandoffRename = originalRename })

	if code := runMacUpdateHandoff(macHandoffConfigFor(tx)); code == 0 {
		t.Fatal("handoff should report the failed replacement launch")
	}
	if got, err := os.ReadFile(filepath.Join(oldApp, "marker")); err != nil || string(got) != "old" {
		t.Fatalf("verified original bundle was not restored: %q, %v", got, err)
	}
	if failedApp == "" {
		t.Fatal("failed replacement bundle was not retained")
	}
	if got, err := os.ReadFile(filepath.Join(failedApp, "marker")); err != nil || string(got) != "changed-after-publish" {
		t.Fatalf("changed replacement was not preserved: %q, %v", got, err)
	}
	if _, err := os.Stat(pending); !os.IsNotExist(err) {
		t.Fatalf("verified rollback did not clear pending state: %v", err)
	}
}

func TestMacUpdateHandoffRejectsBackupChangedDuringRollbackPublish(t *testing.T) {
	root := t.TempDir()
	oldApp := filepath.Join(root, "Reasonix.app")
	newApp := filepath.Join(root, "staging", "Reasonix.app")
	backupApp := oldApp + ".reasonix-update-backup"
	pending := filepath.Join(root, "pending.json")
	for _, dir := range []string{oldApp, newApp} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(oldApp, "marker"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(newApp, "marker"), []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pending, []byte("pending"), 0o600); err != nil {
		t.Fatal(err)
	}
	tx := &repair.UpdateTransaction{
		ToVersion:          "v2",
		CreatedAt:          "2026-07-28T00:00:00Z",
		TargetKind:         "app-bundle",
		TargetPath:         oldApp,
		BackupPath:         backupApp,
		HandoffAppPath:     newApp,
		HandoffStagingPath: filepath.Dir(newApp),
		HandoffOwnerPID:    99999999,
	}
	installMacHandoffTestDeps(t, tx, pending, filepath.Join(root, "update.log"), nil)

	originalOpen := openCommand
	openCommand = func(args ...string) *exec.Cmd {
		return exec.Command("/bin/sh", "-c", "exit 1")
	}
	t.Cleanup(func() { openCommand = originalOpen })
	originalRename := macHandoffRename
	macHandoffRename = func(oldPath, newPath string) error {
		if err := originalRename(oldPath, newPath); err != nil {
			return err
		}
		if oldPath == backupApp && newPath == oldApp {
			return os.WriteFile(filepath.Join(oldApp, "marker"), []byte("tampered-backup"), 0o600)
		}
		return nil
	}
	t.Cleanup(func() { macHandoffRename = originalRename })

	if code := runMacUpdateHandoff(macHandoffConfigFor(tx)); code == 0 {
		t.Fatal("handoff accepted a backup changed during rollback publish")
	}
	if got, err := os.ReadFile(filepath.Join(oldApp, "marker")); err != nil || string(got) != "new" {
		t.Fatalf("verified prior live bundle was not compensated: %q, %v", got, err)
	}
	rejected, err := filepath.Glob(oldApp + ".reasonix-update-rejected-*")
	if err != nil || len(rejected) != 1 {
		t.Fatalf("rejected backup bundle = %v, %v", rejected, err)
	}
	if got, err := os.ReadFile(filepath.Join(rejected[0], "marker")); err != nil || string(got) != "tampered-backup" {
		t.Fatalf("changed backup was not preserved: %q, %v", got, err)
	}
	if _, err := os.Stat(pending); err != nil {
		t.Fatalf("failed rollback removed pending recovery state: %v", err)
	}
}

func TestMacUpdateHandoffHoldsMutationLockDuringSwap(t *testing.T) {
	root := t.TempDir()
	oldApp := filepath.Join(root, "Reasonix.app")
	newApp := filepath.Join(root, "staging", "Reasonix.app")
	backupApp := oldApp + ".reasonix-update-backup"
	pending := filepath.Join(root, "pending.json")
	for _, dir := range []string{oldApp, newApp} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(oldApp, "marker"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(newApp, "marker"), []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pending, []byte("pending"), 0o600); err != nil {
		t.Fatal(err)
	}
	tx := &repair.UpdateTransaction{
		ToVersion:          "v2",
		CreatedAt:          "2026-07-28T00:00:00Z",
		TargetKind:         "app-bundle",
		TargetPath:         oldApp,
		BackupPath:         backupApp,
		HandoffAppPath:     newApp,
		HandoffStagingPath: filepath.Dir(newApp),
		HandoffOwnerPID:    99999999,
	}
	installMacHandoffTestDeps(
		t,
		tx,
		pending,
		filepath.Join(root, "update.log"),
		func(string, string, string, time.Duration) (*repair.UpdateTransaction, func(), error) {
			unlock, err := repair.LockRepairMutationsTimeout(2*time.Second, oldApp, backupApp)
			if err != nil {
				return nil, nil, err
			}
			copy := *tx
			return &copy, unlock, nil
		},
	)

	// Hold the target lock first: handoff must wait, not race past Guard.
	holder, err := repair.LockRepairMutations(oldApp)
	if err != nil {
		t.Fatal(err)
	}

	originalOpen := openCommand
	openCommand = func(args ...string) *exec.Cmd {
		return exec.Command("/bin/sh", "-c", "exit 0")
	}
	t.Cleanup(func() { openCommand = originalOpen })

	started := make(chan struct{})
	done := make(chan int, 1)
	go func() {
		close(started)
		done <- runMacUpdateHandoff(macHandoffConfigFor(tx))
	}()
	<-started
	select {
	case code := <-done:
		holder()
		t.Fatalf("handoff completed while mutation lock held: code=%d", code)
	case <-time.After(300 * time.Millisecond):
	}

	// Bundle must still be the original while the lock is held.
	if got, err := os.ReadFile(filepath.Join(oldApp, "marker")); err != nil || string(got) != "old" {
		holder()
		t.Fatalf("bundle changed while locked: %q, %v", got, err)
	}
	holder()
	code := <-done
	if code != 0 {
		t.Fatalf("handoff after unlock: exit %d", code)
	}
	if got, err := os.ReadFile(filepath.Join(oldApp, "marker")); err != nil || string(got) != "new" {
		t.Fatalf("bundle after handoff = %q, %v", got, err)
	}
}

func TestMacUpdateHandoffReverifiesStagedBundleBeforeSwap(t *testing.T) {
	root := t.TempDir()
	oldApp := filepath.Join(root, "Reasonix.app")
	newApp := filepath.Join(root, "staging", "Reasonix.app")
	backupApp := oldApp + ".reasonix-update-backup"
	pending := filepath.Join(root, "pending.json")
	for _, dir := range []string{oldApp, newApp} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(oldApp, "marker"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(newApp, "marker"), []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pending, []byte("pending"), 0o600); err != nil {
		t.Fatal(err)
	}
	tx := &repair.UpdateTransaction{
		ToVersion:          "v2",
		CreatedAt:          "2026-07-28T00:00:00Z",
		TargetKind:         "app-bundle",
		TargetPath:         oldApp,
		BackupPath:         backupApp,
		HandoffAppPath:     newApp,
		HandoffStagingPath: filepath.Dir(newApp),
		HandoffOwnerPID:    99999999,
	}
	installMacHandoffTestDeps(t, tx, pending, filepath.Join(root, "update.log"), nil)
	verifyMacHandoffApp = func(string) error { return fmt.Errorf("signature changed") }

	code := runMacUpdateHandoff(macHandoffConfigFor(tx))
	if code == 0 {
		t.Fatal("handoff accepted a staged bundle that failed re-verification")
	}
	if got, err := os.ReadFile(filepath.Join(oldApp, "marker")); err != nil || string(got) != "old" {
		t.Fatalf("installed bundle changed after verification failure: %q, %v", got, err)
	}
	if _, err := os.Stat(backupApp); !os.IsNotExist(err) {
		t.Fatalf("backup was created after verification failure: %v", err)
	}
	if _, err := os.Stat(pending); !os.IsNotExist(err) {
		t.Fatalf("pending transaction was not cleared: %v", err)
	}
}

func TestMacUpdateHandoffPreservesStateWhenSafeClearFails(t *testing.T) {
	root := t.TempDir()
	oldApp := filepath.Join(root, "Reasonix.app")
	newApp := filepath.Join(root, "staging", "Reasonix.app")
	backupApp := oldApp + ".reasonix-update-backup"
	pending := filepath.Join(root, "pending.json")
	for _, dir := range []string{oldApp, newApp} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(oldApp, "marker"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(newApp, "marker"), []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pending, []byte("pending"), 0o600); err != nil {
		t.Fatal(err)
	}
	tx := &repair.UpdateTransaction{
		ToVersion:          "v2",
		CreatedAt:          "2026-07-28T00:00:00Z",
		TargetKind:         "app-bundle",
		TargetPath:         oldApp,
		BackupPath:         backupApp,
		HandoffAppPath:     newApp,
		HandoffStagingPath: filepath.Dir(newApp),
		HandoffOwnerPID:    99999999,
	}
	installMacHandoffTestDeps(t, tx, pending, filepath.Join(root, "update.log"), nil)
	verifyMacHandoffApp = func(path string) error {
		if path == newApp {
			return fmt.Errorf("signature changed")
		}
		return nil
	}
	clearMacUpdateHandoff = func(*repair.UpdateTransaction) error {
		return fmt.Errorf("original bundle changed")
	}
	opened := false
	originalOpen := openCommand
	openCommand = func(args ...string) *exec.Cmd {
		opened = true
		return exec.Command("/bin/sh", "-c", "exit 0")
	}
	t.Cleanup(func() { openCommand = originalOpen })

	if code := runMacUpdateHandoff(macHandoffConfigFor(tx)); code == 0 {
		t.Fatal("handoff returned success after safe clear failed")
	}
	if opened {
		t.Fatal("handoff launched an app after safe clear failed")
	}
	if _, err := os.Stat(pending); err != nil {
		t.Fatalf("failed safe clear lost pending transaction: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(newApp)); err != nil {
		t.Fatalf("failed safe clear lost staging: %v", err)
	}
}

func TestMacUpdateHandoffRestartsOriginalAfterRejectedClaim(t *testing.T) {
	root := t.TempDir()
	oldApp := filepath.Join(root, "Reasonix.app")
	newApp := filepath.Join(root, "staging", "Reasonix.app")
	backupApp := oldApp + ".reasonix-update-backup"
	pending := filepath.Join(root, "pending.json")
	for _, dir := range []string{oldApp, newApp} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(oldApp, "marker"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(newApp, "marker"), []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pending, []byte("pending"), 0o600); err != nil {
		t.Fatal(err)
	}
	tx := &repair.UpdateTransaction{
		ToVersion:          "v2",
		CreatedAt:          "2026-07-28T00:00:00Z",
		TargetKind:         "app-bundle",
		TargetPath:         oldApp,
		BackupPath:         backupApp,
		HandoffAppPath:     newApp,
		HandoffStagingPath: filepath.Dir(newApp),
		HandoffOwnerPID:    99999999,
	}
	installMacHandoffTestDeps(
		t,
		tx,
		pending,
		filepath.Join(root, "update.log"),
		func(string, string, string, time.Duration) (*repair.UpdateTransaction, func(), error) {
			return nil, nil, fmt.Errorf("staged bundle changed")
		},
	)
	opened := make(chan string, 1)
	originalOpen := openCommand
	openCommand = func(args ...string) *exec.Cmd {
		if len(args) > 0 {
			opened <- args[len(args)-1]
		}
		return exec.Command("/bin/sh", "-c", "exit 0")
	}
	t.Cleanup(func() { openCommand = originalOpen })

	if code := runMacUpdateHandoff(macHandoffConfigFor(tx)); code == 0 {
		t.Fatal("rejected handoff returned success")
	}
	select {
	case got := <-opened:
		if got != oldApp {
			t.Fatalf("reopened app = %q, want %q", got, oldApp)
		}
	default:
		t.Fatal("original app was not restarted after safe cancellation")
	}
	if _, err := os.Stat(pending); !os.IsNotExist(err) {
		t.Fatalf("pending transaction survived safe cancellation: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(newApp)); !os.IsNotExist(err) {
		t.Fatalf("staging survived safe cancellation: %v", err)
	}
}

func TestMacUpdateHandoffRollsBackWhenInstalledBundleFailsVerification(t *testing.T) {
	root := t.TempDir()
	oldApp := filepath.Join(root, "Reasonix.app")
	newApp := filepath.Join(root, "staging", "Reasonix.app")
	backupApp := oldApp + ".reasonix-update-backup"
	pending := filepath.Join(root, "pending.json")
	for _, dir := range []string{oldApp, newApp} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(oldApp, "marker"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(newApp, "marker"), []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pending, []byte("pending"), 0o600); err != nil {
		t.Fatal(err)
	}
	tx := &repair.UpdateTransaction{
		ToVersion:          "v2",
		CreatedAt:          "2026-07-28T00:00:00Z",
		TargetKind:         "app-bundle",
		TargetPath:         oldApp,
		BackupPath:         backupApp,
		HandoffAppPath:     newApp,
		HandoffStagingPath: filepath.Dir(newApp),
		HandoffOwnerPID:    99999999,
	}
	installMacHandoffTestDeps(t, tx, pending, filepath.Join(root, "update.log"), nil)
	verifyCalls := 0
	verifyMacHandoffApp = func(path string) error {
		verifyCalls++
		if path == oldApp {
			return fmt.Errorf("installed signature changed")
		}
		return nil
	}
	originalOpen := openCommand
	openCommand = func(args ...string) *exec.Cmd {
		return exec.Command("/bin/sh", "-c", "exit 0")
	}
	t.Cleanup(func() { openCommand = originalOpen })

	code := runMacUpdateHandoff(macHandoffConfigFor(tx))
	if code == 0 {
		t.Fatal("handoff accepted an installed bundle that failed verification")
	}
	if verifyCalls != 3 {
		t.Fatalf("bundle verification calls = %d, want source, sibling staging, and installed target", verifyCalls)
	}
	if got, err := os.ReadFile(filepath.Join(oldApp, "marker")); err != nil || string(got) != "old" {
		t.Fatalf("rollback after installed verification failure = %q, %v", got, err)
	}
	if _, err := os.Stat(pending); !os.IsNotExist(err) {
		t.Fatalf("pending transaction survived completed rollback: %v", err)
	}
}

func TestMacUpdateHandoffRestoresOriginalWhenItChangesDuringRename(t *testing.T) {
	root := t.TempDir()
	oldApp := filepath.Join(root, "Reasonix.app")
	newApp := filepath.Join(root, "staging", "Reasonix.app")
	backupApp := oldApp + ".reasonix-update-backup"
	pending := filepath.Join(root, "pending.json")
	for _, dir := range []string{oldApp, newApp} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	oldMarker := filepath.Join(oldApp, "marker")
	if err := os.WriteFile(oldMarker, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(newApp, "marker"), []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pending, []byte("pending"), 0o600); err != nil {
		t.Fatal(err)
	}
	tx := &repair.UpdateTransaction{
		ToVersion:          "v2",
		CreatedAt:          "2026-07-28T00:00:00Z",
		TargetKind:         "app-bundle",
		TargetPath:         oldApp,
		BackupPath:         backupApp,
		HandoffAppPath:     newApp,
		HandoffStagingPath: filepath.Dir(newApp),
		HandoffOwnerPID:    99999999,
	}
	installMacHandoffTestDeps(t, tx, pending, filepath.Join(root, "update.log"), nil)
	originalRename := macHandoffRename
	changed := false
	macHandoffRename = func(from, to string) error {
		if !changed && from == oldApp && to == backupApp {
			changed = true
			if err := os.WriteFile(oldMarker, []byte("concurrent"), 0o600); err != nil {
				return err
			}
		}
		return originalRename(from, to)
	}
	t.Cleanup(func() { macHandoffRename = originalRename })
	originalOpen := openCommand
	openCommand = func(args ...string) *exec.Cmd {
		return exec.Command("/bin/sh", "-c", "exit 0")
	}
	t.Cleanup(func() { openCommand = originalOpen })

	code := runMacUpdateHandoff(macHandoffConfigFor(tx))
	if code == 0 {
		t.Fatal("handoff accepted an original bundle that changed during rename")
	}
	if _, err := os.Stat(oldApp); !os.IsNotExist(err) {
		t.Fatalf("unverified original was restored to the live path: %v", err)
	}
	if _, err := os.Stat(backupApp); !os.IsNotExist(err) {
		t.Fatalf("changed backup remained at the rollback path: %v", err)
	}
	rejected, err := filepath.Glob(oldApp + ".reasonix-update-rejected-*")
	if err != nil || len(rejected) != 1 {
		t.Fatalf("rejected original bundle = %v, %v", rejected, err)
	}
	if got, err := os.ReadFile(filepath.Join(rejected[0], "marker")); err != nil || string(got) != "concurrent" {
		t.Fatalf("changed original was not preserved: %q, %v", got, err)
	}
	if _, err := os.Stat(pending); err != nil {
		t.Fatalf("failed rollback removed pending recovery state: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(newApp, "marker")); err != nil || string(got) != "new" {
		t.Fatalf("failed rollback removed replacement recovery state: %q, %v", got, err)
	}
}

func TestMacUpdateHandoffRejectsTransactionChangedDuringPIDWait(t *testing.T) {
	root := t.TempDir()
	oldApp := filepath.Join(root, "Reasonix.app")
	newApp := filepath.Join(root, "staging", "Reasonix.app")
	backupApp := oldApp + ".reasonix-update-backup"
	pending := filepath.Join(root, "pending.json")
	for _, dir := range []string{oldApp, newApp} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(oldApp, "marker"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(newApp, "marker"), []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pending, []byte("pending"), 0o600); err != nil {
		t.Fatal(err)
	}
	tx := &repair.UpdateTransaction{
		ToVersion:          "v2",
		CreatedAt:          "2026-07-28T00:00:00Z",
		TargetKind:         "app-bundle",
		TargetPath:         oldApp,
		BackupPath:         backupApp,
		HandoffAppPath:     newApp,
		HandoffStagingPath: filepath.Dir(newApp),
		HandoffOwnerPID:    99999999,
	}
	released := false
	installMacHandoffTestDeps(
		t,
		tx,
		pending,
		filepath.Join(root, "update.log"),
		func(string, string, string, time.Duration) (*repair.UpdateTransaction, func(), error) {
			changed := *tx
			changed.HandoffOwnerPID++
			return &changed, func() { released = true }, nil
		},
	)

	if code := runMacUpdateHandoff(macHandoffConfigFor(tx)); code == 0 {
		t.Fatal("handoff accepted a transaction changed during PID wait")
	}
	if !released {
		t.Fatal("rejected changed transaction did not release its claim")
	}
	if got, err := os.ReadFile(filepath.Join(oldApp, "marker")); err != nil || string(got) != "old" {
		t.Fatalf("installed bundle changed after transaction drift: %q, %v", got, err)
	}
	if _, err := os.Stat(backupApp); !os.IsNotExist(err) {
		t.Fatalf("backup was created after transaction drift: %v", err)
	}
	if _, err := os.Stat(pending); err != nil {
		t.Fatalf("changed transaction was unsafely cancelled: %v", err)
	}
}

func TestMacUpdateHandoffRejectsTransactionRewrittenBeforeFirstRead(t *testing.T) {
	root := t.TempDir()
	oldApp := filepath.Join(root, "Reasonix.app")
	newApp := filepath.Join(root, "staging", "Reasonix.app")
	backupApp := oldApp + ".reasonix-update-backup"
	pending := filepath.Join(root, "pending.json")
	for _, dir := range []string{oldApp, newApp} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for path, content := range map[string]string{
		filepath.Join(oldApp, "marker"): "old",
		filepath.Join(newApp, "marker"): "new",
		pending:                         "pending",
	} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	tx := &repair.UpdateTransaction{
		ToVersion:          "v2",
		CreatedAt:          "2026-07-28T00:00:00Z",
		TargetKind:         "app-bundle",
		TargetPath:         oldApp,
		BackupPath:         backupApp,
		HandoffAppPath:     newApp,
		HandoffStagingPath: filepath.Dir(newApp),
		HandoffOwnerPID:    99999999,
	}
	claimed := false
	installMacHandoffTestDeps(
		t,
		tx,
		pending,
		filepath.Join(root, "update.log"),
		func(string, string, string, time.Duration) (*repair.UpdateTransaction, func(), error) {
			claimed = true
			copy := *tx
			return &copy, func() {}, nil
		},
	)
	cfg := macHandoffConfigFor(tx)
	tx.FromVersion = "rewritten"

	if code := runMacUpdateHandoff(cfg); code == 0 {
		t.Fatal("handoff accepted a transaction rewritten before its first read")
	}
	if claimed {
		t.Fatal("rewritten transaction reached the claim path")
	}
	if got, err := os.ReadFile(filepath.Join(oldApp, "marker")); err != nil || string(got) != "old" {
		t.Fatalf("installed bundle changed after rewritten transaction: %q, %v", got, err)
	}
	if _, err := os.Stat(pending); err != nil {
		t.Fatalf("rewritten pending transaction was removed: %v", err)
	}
}

func TestMacUpdateHandoffPreservesConcurrentCreateBeforePublish(t *testing.T) {
	root := t.TempDir()
	oldApp := filepath.Join(root, "Reasonix.app")
	newApp := filepath.Join(root, "staging", "Reasonix.app")
	backupApp := oldApp + ".reasonix-update-backup"
	pending := filepath.Join(root, "pending.json")
	for _, dir := range []string{oldApp, newApp} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(oldApp, "marker"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(newApp, "marker"), []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pending, []byte("pending"), 0o600); err != nil {
		t.Fatal(err)
	}
	tx := &repair.UpdateTransaction{
		ToVersion:          "v2",
		CreatedAt:          "2026-07-28T00:00:00Z",
		TargetKind:         "app-bundle",
		TargetPath:         oldApp,
		BackupPath:         backupApp,
		HandoffAppPath:     newApp,
		HandoffStagingPath: filepath.Dir(newApp),
		HandoffOwnerPID:    99999999,
	}
	installMacHandoffTestDeps(t, tx, pending, filepath.Join(root, "update.log"), nil)

	originalRename := macHandoffRename
	recreated := false
	macHandoffRename = func(from, to string) error {
		if !recreated && to == oldApp && from != backupApp {
			recreated = true
			if err := os.MkdirAll(oldApp, 0o700); err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(oldApp, "marker"), []byte("concurrent"), 0o600); err != nil {
				return err
			}
		}
		return originalRename(from, to)
	}
	t.Cleanup(func() { macHandoffRename = originalRename })
	opened := false
	originalOpen := openCommand
	openCommand = func(args ...string) *exec.Cmd {
		opened = true
		return exec.Command("/bin/sh", "-c", "exit 0")
	}
	t.Cleanup(func() { openCommand = originalOpen })

	if code := runMacUpdateHandoff(macHandoffConfigFor(tx)); code == 0 {
		t.Fatal("handoff replaced a bundle recreated before atomic publish")
	}
	if !recreated {
		t.Fatal("test did not inject a concurrent destination")
	}
	if opened {
		t.Fatal("handoff launched an app after publish conflict")
	}
	if got, err := os.ReadFile(filepath.Join(oldApp, "marker")); err != nil || string(got) != "concurrent" {
		t.Fatalf("concurrent bundle was not preserved: %q, %v", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(backupApp, "marker")); err != nil || string(got) != "old" {
		t.Fatalf("rollback backup was not preserved: %q, %v", got, err)
	}
	if _, err := os.Stat(pending); err != nil {
		t.Fatalf("pending recovery state was removed after publish conflict: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(newApp)); err != nil {
		t.Fatalf("source staging was removed after publish conflict: %v", err)
	}
}

func TestMacUpdateHandoffParserRejectsFilesystemPaths(t *testing.T) {
	_, err := parseMacUpdateHandoffArgs([]string{
		"-to-version", "v2",
		"-created-at", "2026-07-28T00:00:00Z",
		"-transaction-id", strings.Repeat("a", 64),
		"-old-app", "/tmp/Unrelated.app",
	})
	if err == nil {
		t.Fatal("legacy filesystem path argument was accepted")
	}
}

func TestMacUpdateHandoffParserRequiresCompletePipePair(t *testing.T) {
	_, err := parseMacUpdateHandoffArgs([]string{
		"-to-version", "v2",
		"-created-at", "2026-07-28T00:00:00Z",
		"-transaction-id", strings.Repeat("a", 64),
		"-ready-fd", "3",
	})
	if err == nil || !strings.Contains(err.Error(), "pipe arguments") {
		t.Fatalf("partial pipe pair error = %v", err)
	}
}

func TestMacUpdateHandoffReadinessSurfacesChildStartupFailure(t *testing.T) {
	readyReader, readyWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer readyReader.Close()
	defer readyWriter.Close()
	originalRead := readMacUpdateHandoff
	originalLogPath := macHandoffLogPath
	readMacUpdateHandoff = func() (*repair.UpdateTransaction, error) {
		return nil, fmt.Errorf("pending transaction is unreadable")
	}
	macHandoffLogPath = func() string { return filepath.Join(t.TempDir(), "update-helper.log") }
	t.Cleanup(func() {
		readMacUpdateHandoff = originalRead
		macHandoffLogPath = originalLogPath
	})
	readyFD := duplicateMacHandoffFD(t, readyWriter)
	done := make(chan int, 1)
	go func() {
		done <- runMacUpdateHandoff(macUpdateHandoffConfig{
			ToVersion:     "v2",
			CreatedAt:     "2026-07-28T00:00:00Z",
			TransactionID: strings.Repeat("a", 64),
			ReadyFD:       readyFD,
			ProceedFD:     int(readyWriter.Fd()) + 1,
		})
	}()
	err = waitForMacHandoffReady(readyReader, time.Second)
	if err == nil || !strings.Contains(err.Error(), "read-pending-transaction") ||
		!strings.Contains(err.Error(), "pending transaction is unreadable") {
		t.Fatalf("readiness error = %v", err)
	}
	if code := <-done; code == 0 {
		t.Fatal("helper startup failure returned success")
	}
}

func TestMacUpdateHandoffHandshakeWaitsForParentRelease(t *testing.T) {
	readyReader, readyWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	proceedReader, proceedWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer readyReader.Close()
	defer readyWriter.Close()
	defer proceedReader.Close()
	defer proceedWriter.Close()

	readyFD := duplicateMacHandoffFD(t, readyWriter)
	proceedFD := duplicateMacHandoffFD(t, proceedReader)
	done := make(chan error, 1)
	go func() {
		done <- completeMacHandoffHandshake(macUpdateHandoffConfig{
			ReadyFD:   readyFD,
			ProceedFD: proceedFD,
		})
	}()
	if err := waitForMacHandoffReady(readyReader, time.Second); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		t.Fatalf("handshake completed before parent release: %v", err)
	default:
	}
	if _, err := proceedWriter.Write([]byte("go")); err != nil {
		t.Fatal(err)
	}
	if err := proceedWriter.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("handshake did not finish after parent release")
	}
}

func TestMacUpdateHandoffHandshakeRejectsParentExit(t *testing.T) {
	readyReader, readyWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	proceedReader, proceedWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer readyReader.Close()
	defer readyWriter.Close()
	defer proceedReader.Close()
	if err := proceedWriter.Close(); err != nil {
		t.Fatal(err)
	}

	readyFD := duplicateMacHandoffFD(t, readyWriter)
	proceedFD := duplicateMacHandoffFD(t, proceedReader)
	done := make(chan error, 1)
	go func() {
		done <- completeMacHandoffHandshake(macUpdateHandoffConfig{
			ReadyFD:   readyFD,
			ProceedFD: proceedFD,
		})
	}()
	if err := waitForMacHandoffReady(readyReader, time.Second); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "parent release") {
			t.Fatalf("parent exit error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("handshake did not observe parent exit")
	}
}

func TestMacUpdateHandoffParentExitCancelsPreparedTransaction(t *testing.T) {
	root := t.TempDir()
	oldApp := filepath.Join(root, "Reasonix.app")
	newApp := filepath.Join(root, "staging", "Reasonix.app")
	pending := filepath.Join(root, "pending.json")
	for _, dir := range []string{oldApp, newApp} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(oldApp, "marker"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(newApp, "marker"), []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pending, []byte("pending"), 0o600); err != nil {
		t.Fatal(err)
	}
	tx := &repair.UpdateTransaction{
		ToVersion:          "v2",
		CreatedAt:          "2026-07-28T00:00:00Z",
		TargetKind:         "app-bundle",
		TargetPath:         oldApp,
		BackupPath:         oldApp + ".reasonix-update-backup",
		HandoffAppPath:     newApp,
		HandoffStagingPath: filepath.Dir(newApp),
		HandoffOwnerPID:    os.Getpid(),
	}
	installMacHandoffTestDeps(t, tx, pending, filepath.Join(root, "update.log"), nil)
	readyReader, readyWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	proceedReader, proceedWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer readyReader.Close()
	defer readyWriter.Close()
	defer proceedReader.Close()
	if err := proceedWriter.Close(); err != nil {
		t.Fatal(err)
	}
	cfg := macHandoffConfigFor(tx)
	cfg.ReadyFD = duplicateMacHandoffFD(t, readyWriter)
	cfg.ProceedFD = duplicateMacHandoffFD(t, proceedReader)

	if code := runMacUpdateHandoff(cfg); code == 0 {
		t.Fatal("handoff accepted a parent that exited before release")
	}
	if _, err := os.Stat(pending); !os.IsNotExist(err) {
		t.Fatalf("abandoned pending transaction survived: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(oldApp, "marker")); err != nil || string(got) != "old" {
		t.Fatalf("original app changed = %q, %v", got, err)
	}
}

func TestMacHandoffRenameDoesNotReplaceExistingDestination(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	destination := filepath.Join(dir, "destination")
	if err := os.WriteFile(source, []byte("source"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("destination"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := macHandoffRename(source, destination); err == nil {
		t.Fatal("macOS handoff rename overwrote an existing destination")
	}
	for path, want := range map[string]string{source: "source", destination: "destination"} {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want {
			t.Fatalf("%s = %q, %v; want %q", filepath.Base(path), got, err, want)
		}
	}
}

func TestRetainMacHandoffNodeRetriesNameCollision(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Reasonix.app")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	originalRename := macHandoffRename
	calls := 0
	macHandoffRename = func(oldPath, newPath string) error {
		calls++
		if calls == 1 {
			return os.ErrExist
		}
		return originalRename(oldPath, newPath)
	}
	t.Cleanup(func() { macHandoffRename = originalRename })

	retained, err := retainMacHandoffNode(path, "reasonix-update-failed")
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || !strings.Contains(retained, ".reasonix-update-failed-") {
		t.Fatalf("retain calls=%d path=%q", calls, retained)
	}
	if _, err := os.Stat(retained); err != nil {
		t.Fatalf("retained bundle: %v", err)
	}
}

func TestCleanupOwnedMacUpdateDirectoryPreservesConcurrentRecreate(t *testing.T) {
	parent := t.TempDir()
	path, err := os.MkdirTemp(parent, ".reasonix-update-install-*")
	if err != nil {
		t.Fatal(err)
	}
	owner, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	originalHook := macUpdateCleanupAfterRename
	macUpdateCleanupAfterRename = func(original, _ string) {
		if original != path {
			return
		}
		if err := os.Mkdir(original, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(original, "concurrent"), []byte("keep"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { macUpdateCleanupAfterRename = originalHook })

	if err := cleanupOwnedMacUpdateDirectory(path, owner); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(path, "concurrent")); err != nil || string(got) != "keep" {
		t.Fatalf("concurrent directory = %q, %v; want preserved", got, err)
	}
}

func TestCleanupOwnedMacUpdateDirectoryRejectsReplacedRoot(t *testing.T) {
	parent := t.TempDir()
	path, err := os.MkdirTemp(parent, ".reasonix-update-install-*")
	if err != nil {
		t.Fatal(err)
	}
	owner, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "replacement"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := cleanupOwnedMacUpdateDirectory(path, owner); err == nil ||
		!strings.Contains(err.Error(), "changed before cleanup") {
		t.Fatalf("cleanup error = %v, want ownership rejection", err)
	}
	if got, err := os.ReadFile(filepath.Join(path, "replacement")); err != nil || string(got) != "keep" {
		t.Fatalf("replacement directory = %q, %v; want preserved", got, err)
	}
}

func TestMacUpdateHandoffSerializesWithConcurrentRollback(t *testing.T) {
	// Ensure LockRepairMutations used by handoff and repair share identity.
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	oldApp := filepath.Join(root, "Reasonix.app")
	if err := os.MkdirAll(oldApp, 0o700); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	order := make(chan string, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		unlock, err := repair.LockRepairMutationsTimeout(2*time.Second, oldApp)
		if err != nil {
			t.Errorf("first lock: %v", err)
			return
		}
		order <- "a"
		time.Sleep(200 * time.Millisecond)
		unlock()
	}()
	go func() {
		defer wg.Done()
		time.Sleep(20 * time.Millisecond)
		unlock, err := repair.LockRepairMutationsTimeout(2*time.Second, oldApp)
		if err != nil {
			t.Errorf("second lock: %v", err)
			return
		}
		order <- "b"
		unlock()
	}()
	wg.Wait()
	first := <-order
	second := <-order
	if first != "a" || second != "b" {
		t.Fatalf("lock order = %s then %s, want a then b", first, second)
	}
}
