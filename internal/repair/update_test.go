package repair

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func fileUpdateInstallReceiptsForTest(t *testing.T, tx *UpdateTransaction) []FileUpdateInstallReceipt {
	t.Helper()
	receipts := make([]FileUpdateInstallReceipt, 0, len(tx.Files))
	for _, file := range tx.Files {
		if _, err := os.Lstat(file.TargetPath); err != nil {
			if os.IsNotExist(err) && file.MissingBefore {
				continue
			}
			t.Fatalf("inspect installed test release member %s: %v", file.TargetPath, err)
		}
		receipts = append(receipts, FileUpdateInstallReceipt{
			UpdateTransactionID: UpdateTransactionID(tx),
			TargetPath:          file.TargetPath,
			InstalledStateID:    repairPlanReleaseNodeState(file.TargetPath),
		})
	}
	return receipts
}

func TestFileUpdateRollbackRestoresPreviousBinary(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	target := filepath.Join(t.TempDir(), "reasonix-desktop")
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return filepath.Join(filepath.Dir(target), "reasonix-guard"), nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })
	if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareFileUpdate("v1", "v2", target); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("new"), 0o700); err != nil {
		t.Fatal(err)
	}
	result, err := RollbackPendingUpdate()
	if err != nil {
		t.Fatal(err)
	}
	if !result.RolledBack || result.ToVersion != "v1" {
		t.Fatalf("rollback result = %+v", result)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "old" {
		t.Fatalf("restored binary = %q", got)
	}
}

func TestReconcilePendingFileUpdateCancelsPreparedReleaseUnit(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	target := filepath.Join(t.TempDir(), "reasonix-desktop")
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return filepath.Join(filepath.Dir(target), "reasonix-guard"), nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })
	if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	tx, err := PrepareFileUpdate("v1", "v2", target)
	if err != nil {
		t.Fatal(err)
	}

	result, err := ReconcilePendingUpdate("v2")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Cleared || result.RolledBack || result.AwaitingHealth {
		t.Fatalf("reconcile result = %+v", result)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "old" {
		t.Fatalf("prepared target changed = %q, %v", got, err)
	}
	for _, file := range tx.Files {
		if _, err := os.Stat(file.BackupPath); !os.IsNotExist(err) {
			t.Fatalf("prepared backup survived cancellation: %v", err)
		}
	}
	if PendingUpdateExists() {
		t.Fatal("pending file update survived cancellation")
	}
}

// TestReconcilePendingFileUpdateQuarantinesForeignTarget: a self-describing
// transaction whose launcher and target directories no longer match (install
// layout moved to versions\<version>\, or the old install directory is gone)
// cannot be resumed or rolled back by this installation. Leaving the marker
// blocks every future update permanently, so reconciliation quarantines it the
// same way preparation does, without touching the target or rollback material
// (#7391, #7416).
func TestReconcilePendingFileUpdateQuarantinesForeignTarget(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	target := filepath.Join(t.TempDir(), "reasonix-desktop")
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return filepath.Join(filepath.Dir(target), "reasonix-guard"), nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })
	if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	tx, err := PrepareFileUpdate("v1", "v2", target)
	if err != nil {
		t.Fatal(err)
	}

	// The install layout moved: the current launcher now lives in a different
	// directory from the transaction target.
	repairExecutable = func() (string, error) { return filepath.Join(t.TempDir(), "reasonix-guard"), nil }

	result, err := ReconcilePendingUpdate("v2")
	if err != nil {
		t.Fatalf("reconcile foreign-target transaction: %v", err)
	}
	if !result.Pending || !result.Cleared || result.RolledBack || result.AwaitingHealth {
		t.Fatalf("reconcile result = %+v", result)
	}
	if PendingUpdateExists() {
		t.Fatal("foreign-target marker survived reconciliation")
	}
	// The marker is quarantined, not deleted, and the target plus rollback
	// material are untouched.
	quarantined, err := filepath.Glob(PendingUpdatePath() + ".unusable-*")
	if err != nil || len(quarantined) != 1 {
		t.Fatalf("quarantined markers = %v, %v; want exactly one", quarantined, err)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "old" {
		t.Fatalf("prepared target changed = %q, %v", got, err)
	}
	for _, file := range tx.Files {
		if _, err := os.Stat(file.BackupPath); err != nil {
			t.Fatalf("rollback backup disappeared: %v", err)
		}
	}
}

// TestPrepareFileUpdateQuarantinesForeignTarget: preparation must offer the
// same exit as reconciliation for a self-describing marker that is not valid
// for this installation, so callers that skip reconciliation (or reach prepare
// first) do not dead-end forever on the marker (#7391, #7416).
func TestPrepareFileUpdateQuarantinesForeignTarget(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	target := filepath.Join(t.TempDir(), "reasonix-desktop")
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return filepath.Join(filepath.Dir(target), "reasonix-guard"), nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })
	if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareFileUpdate("v1", "v2", target); err != nil {
		t.Fatal(err)
	}

	// The install layout moved: the launcher now lives in a different
	// directory from the first transaction's target.
	newLauncherDir := t.TempDir()
	repairExecutable = func() (string, error) { return filepath.Join(newLauncherDir, "reasonix-guard"), nil }
	newTarget := filepath.Join(newLauncherDir, "reasonix-desktop")
	if err := os.WriteFile(newTarget, []byte("old2"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareFileUpdate("v2", "v3", newTarget); err != nil {
		t.Fatalf("prepare after quarantining a foreign-target marker: %v", err)
	}
	quarantined, err := filepath.Glob(PendingUpdatePath() + ".unusable-*")
	if err != nil || len(quarantined) != 1 {
		t.Fatalf("quarantined markers = %v, %v; want exactly one", quarantined, err)
	}
}

func TestReconcileForeignQuarantineRejectsMarkerReplacement(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	target := filepath.Join(t.TempDir(), "reasonix-desktop")
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return filepath.Join(filepath.Dir(target), "reasonix-guard"), nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })
	if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareFileUpdate("v1", "v2", target); err != nil {
		t.Fatal(err)
	}
	repairExecutable = func() (string, error) { return filepath.Join(t.TempDir(), "reasonix-guard"), nil }
	digest, err := pendingUpdateMarkerDigest(PendingUpdatePath())
	if err != nil {
		t.Fatal(err)
	}
	replacement := &UpdateTransaction{
		SchemaVersion: updateTransactionVersion,
		FromVersion:   "v2",
		ToVersion:     "v3",
		Platform:      "test/amd64",
		TargetKind:    "file",
		TargetPath:    target,
		CreatedAt:     time.Now().UTC().Format(time.RFC3339Nano),
	}
	if err := overwritePendingUpdateForTest(replacement); err != nil {
		t.Fatal(err)
	}
	if _, err := quarantinePendingUpdateAfterReconcile("test", digest, true); err == nil || !strings.Contains(err.Error(), "changed while waiting") {
		t.Fatalf("quarantine after marker replacement = %v, want a replacement error", err)
	}
	if !PendingUpdateExists() {
		t.Fatal("replacement marker was quarantined")
	}
	if got := quarantinedCopies(t); len(got) != 0 {
		t.Fatalf("quarantined copies = %v, want none", got)
	}
}

func TestSelfDescribingInvalidTransactionRemainsBlocked(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	target := filepath.Join(t.TempDir(), "reasonix-desktop")
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return filepath.Join(filepath.Dir(target), "reasonix-guard"), nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })
	if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	tx, err := PrepareFileUpdate("v1", "v2", target)
	if err != nil {
		t.Fatal(err)
	}
	tx.BackupSHA256 = ""
	if err := overwritePendingUpdateForTest(tx); err != nil {
		t.Fatal(err)
	}
	if _, err := ReconcilePendingUpdate("v1"); err == nil || !strings.Contains(err.Error(), "backup hash is missing") {
		t.Fatalf("reconcile invalid self-describing transaction = %v, want validation failure", err)
	}
	if _, err := PrepareFileUpdate("v1", "v3", target); err == nil || !strings.Contains(err.Error(), "backup hash is missing") {
		t.Fatalf("prepare over invalid self-describing transaction = %v, want validation failure", err)
	}
	if !PendingUpdateExists() {
		t.Fatal("invalid self-describing marker was quarantined or removed")
	}
	if got := quarantinedCopies(t); len(got) != 0 {
		t.Fatalf("quarantined copies = %v, want none", got)
	}
}

func TestReconcilePendingFileUpdateLeavesBoundProbationaryReleaseUnit(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	target := filepath.Join(t.TempDir(), "reasonix-desktop")
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return filepath.Join(filepath.Dir(target), "reasonix-guard"), nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })
	if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	tx, err := PrepareFileUpdate("v1", "v2", target)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := PublishClaimedFileUpdateMemberExact(tx, target, []byte("new"), 0o700)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RecordClaimedFileUpdateInstalled(tx, receipt); err != nil {
		t.Fatal(err)
	}

	result, err := ReconcilePendingUpdate("v2")
	if !errors.Is(err, ErrPendingUpdateAwaitingHealth) {
		t.Fatalf("reconcile error = %v", err)
	}
	if !result.Pending || !result.AwaitingHealth || result.Cleared || result.RolledBack {
		t.Fatalf("reconcile result = %+v", result)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "new" {
		t.Fatalf("probationary target changed = %q, %v", got, err)
	}
	if !PendingUpdateExists() {
		t.Fatal("probationary transaction was removed")
	}
}

func TestUpdateVersionsEqualNormalizesLeadingV(t *testing.T) {
	if !UpdateVersionsEqual("1.19.7", "v1.19.7") {
		t.Fatal("expected 1.19.7 and v1.19.7 to match")
	}
	if !UpdateVersionsEqual("v1.20.0", "V1.20.0") {
		t.Fatal("expected case-insensitive v prefix normalization")
	}
	if UpdateVersionsEqual("v1.19.7", "v1.19.8") {
		t.Fatal("different versions must not match")
	}
	if UpdateVersionsEqual("", "v1") {
		t.Fatal("empty version must not match")
	}
}

func TestReconcilePendingFileUpdateCommitsStaleProbationaryReleaseUnit(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	target := filepath.Join(t.TempDir(), "reasonix-desktop")
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return filepath.Join(filepath.Dir(target), "reasonix-guard"), nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })
	// Do not rewrite CreatedAt: it is part of the transaction identity that
	// install-evidence binding requires. Force the stale decision instead.
	pendingUpdateHealthIsStaleOverride = func(*UpdateTransaction) bool { return true }
	t.Cleanup(func() { pendingUpdateHealthIsStaleOverride = nil })

	if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	tx, err := PrepareFileUpdate("v1", "v2", target)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := PublishClaimedFileUpdateMemberExact(tx, target, []byte("new"), 0o700)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RecordClaimedFileUpdateInstalled(tx, receipt); err != nil {
		t.Fatal(err)
	}

	// Version-prefix mismatch used to skip health commits; reconcile must still heal.
	result, err := ReconcilePendingUpdate("2")
	if err != nil {
		t.Fatalf("reconcile stale probationary update: %v", err)
	}
	if !result.Healthy || !result.Cleared || result.AwaitingHealth || result.Pending {
		t.Fatalf("reconcile result = %+v", result)
	}
	if PendingUpdateExists() {
		t.Fatal("stale probationary transaction survived reconcile")
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "new" {
		t.Fatalf("installed target changed = %q, %v", got, err)
	}
}

func TestAbandonPendingUpdateCommitsProbationaryReleaseUnit(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	target := filepath.Join(t.TempDir(), "reasonix-desktop")
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return filepath.Join(filepath.Dir(target), "reasonix-guard"), nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })
	if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	tx, err := PrepareFileUpdate("v1", "v2", target)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := PublishClaimedFileUpdateMemberExact(tx, target, []byte("new"), 0o700)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RecordClaimedFileUpdateInstalled(tx, receipt); err != nil {
		t.Fatal(err)
	}

	result, err := AbandonPendingUpdate("v2")
	if err != nil {
		t.Fatalf("abandon: %v", err)
	}
	if !result.Healthy || !result.Cleared {
		t.Fatalf("abandon result = %+v", result)
	}
	if PendingUpdateExists() {
		t.Fatal("pending transaction survived abandon")
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "new" {
		t.Fatalf("installed target changed = %q, %v", got, err)
	}
}

func TestAbandonPendingUpdateForceRetiresWhenBackupDigestBroken(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	target := filepath.Join(t.TempDir(), "reasonix-desktop")
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return filepath.Join(filepath.Dir(target), "reasonix-guard"), nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })
	if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	tx, err := PrepareFileUpdate("v1", "v2", target)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := PublishClaimedFileUpdateMemberExact(tx, target, []byte("new"), 0o700)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RecordClaimedFileUpdateInstalled(tx, receipt); err != nil {
		t.Fatal(err)
	}

	// Corrupt backup: MarkUpdateHealthy fails; abandon must still force-retire.
	if strings.TrimSpace(tx.BackupPath) == "" {
		t.Fatal("expected prepared backup path")
	}
	if err := os.WriteFile(tx.BackupPath, []byte("corrupted-backup"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := MarkUpdateHealthy("v2"); err == nil {
		t.Fatal("MarkUpdateHealthy should fail when backup digest drifts")
	}
	if !PendingUpdateExists() {
		t.Fatal("pending transaction was removed by failed MarkUpdateHealthy")
	}

	result, err := AbandonPendingUpdate("v2")
	if err != nil {
		t.Fatalf("abandon with broken backup: %v", err)
	}
	if !result.Healthy || !result.Cleared || result.AwaitingHealth || result.Pending {
		t.Fatalf("abandon result = %+v", result)
	}
	if PendingUpdateExists() {
		t.Fatal("pending transaction survived force-retire abandon")
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "new" {
		t.Fatalf("installed target changed = %q, %v", got, err)
	}
}

func TestReconcilePendingFileUpdateRollsBackPublishedReleaseUnit(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	target := filepath.Join(t.TempDir(), "reasonix-desktop")
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return filepath.Join(filepath.Dir(target), "reasonix-guard"), nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })
	if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareFileUpdate("v1", "v2", target); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("new"), 0o700); err != nil {
		t.Fatal(err)
	}

	result, err := ReconcilePendingUpdate("v1")
	if err != nil {
		t.Fatal(err)
	}
	if !result.RolledBack || result.Cleared || result.AwaitingHealth {
		t.Fatalf("reconcile result = %+v", result)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "old" {
		t.Fatalf("published target was not restored = %q, %v", got, err)
	}
	if PendingUpdateExists() {
		t.Fatal("pending file update survived rollback")
	}
}

func TestAppBundleRollbackRestoresWhenLiveBundleIsMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app := filepath.Join(dir, "Reasonix.app")
	exe := filepath.Join(app, "Contents", "MacOS", "Reasonix")
	if err := os.MkdirAll(filepath.Dir(exe), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return exe, nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })

	backup := app + ".reasonix-update-backup"
	if _, err := PrepareAppBundleUpdate("v1", "v2", app, backup); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(app, backup); err != nil {
		t.Fatal(err)
	}
	result, err := RollbackPendingUpdate()
	if err != nil {
		t.Fatal(err)
	}
	if !result.RolledBack || result.ToVersion != "v1" {
		t.Fatalf("rollback result = %+v", result)
	}
	got, err := os.ReadFile(exe)
	if err != nil || string(got) != "old" {
		t.Fatalf("restored bundle executable = %q (%v)", got, err)
	}
	if _, err := os.Stat(backup); !os.IsNotExist(err) {
		t.Fatalf("backup bundle survived successful rollback: %v", err)
	}
	if HasPendingUpdate() {
		t.Fatal("pending update survived successful rollback")
	}
}

func TestAppBundleRollbackRecognizesAlreadyRestoredBundle(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app := filepath.Join(dir, "Reasonix.app")
	exe := filepath.Join(app, "Contents", "MacOS", "Reasonix")
	if err := os.MkdirAll(filepath.Dir(exe), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return exe, nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })

	backup := app + ".reasonix-update-backup"
	if _, err := PrepareAppBundleUpdate("v1", "v2", app, backup); err != nil {
		t.Fatal(err)
	}
	// This is the durable state after backup -> live succeeded but the process
	// exited before pending-update.json was removed. It is also the safe
	// pre-handoff cancellation state.
	result, err := RollbackPendingUpdate()
	if err != nil || !result.RolledBack {
		t.Fatalf("already-restored rollback = %+v, %v", result, err)
	}
	if got, err := os.ReadFile(exe); err != nil || string(got) != "old" {
		t.Fatalf("recognized live bundle = %q, %v", got, err)
	}
	if HasPendingUpdate() {
		t.Fatal("already-restored app transaction was not cleared")
	}
}

func TestMarkUpdateHealthyRejectsAppBundleDrift(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app := filepath.Join(dir, "Reasonix.app")
	exe := filepath.Join(app, "Contents", "MacOS", "Reasonix")
	if err := os.MkdirAll(filepath.Dir(exe), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return exe, nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })

	backup := app + ".reasonix-update-backup"
	staging, err := os.MkdirTemp("", "reasonix-mac-update-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(staging) })
	stagedApp := filepath.Join(staging, "Reasonix.app")
	stagedExe := filepath.Join(stagedApp, "Contents", "MacOS", "Reasonix")
	if err := os.MkdirAll(filepath.Dir(stagedExe), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stagedExe, []byte("new"), 0o700); err != nil {
		t.Fatal(err)
	}
	tx, err := PrepareAppBundleUpdateHandoff(
		"v1", "v2", app, backup, stagedApp, staging, os.Getpid(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(app, backup); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(exe), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("new"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("drifted-after-launch"), 0o700); err != nil {
		t.Fatal(err)
	}

	if err := MarkUpdateHealthyMatching(tx.ToVersion, tx.CreatedAt); err == nil ||
		!strings.Contains(err.Error(), "installed bundle differs") {
		t.Fatalf("healthy commit error = %v, want installed-bundle drift rejection", err)
	}
	if _, err := ReadPendingUpdate(); err != nil {
		t.Fatalf("rejected healthy commit removed pending transaction: %v", err)
	}
	if _, err := os.Stat(backup); err != nil {
		t.Fatalf("rejected healthy commit removed rollback backup: %v", err)
	}
}

func TestMarkUpdateHealthyRejectsLegacyAppBundleWithoutInstalledDigest(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app := filepath.Join(dir, "Reasonix.app")
	exe := filepath.Join(app, "Contents", "MacOS", "Reasonix")
	if err := os.MkdirAll(filepath.Dir(exe), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return exe, nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })

	backup := app + ".reasonix-update-backup"
	tx, err := PrepareAppBundleUpdate("v1", "v2", app, backup)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(app, backup); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(exe), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("new"), 0o700); err != nil {
		t.Fatal(err)
	}

	if err := MarkUpdateHealthyMatching(tx.ToVersion, tx.CreatedAt); err == nil ||
		!strings.Contains(err.Error(), "installed bundle state is missing") {
		t.Fatalf("legacy healthy commit = %v, want fail-closed rejection", err)
	}
	if !HasPendingUpdate() {
		t.Fatal("legacy healthy rejection removed pending transaction")
	}
	if _, err := os.Stat(backup); err != nil {
		t.Fatalf("legacy healthy rejection removed rollback backup: %v", err)
	}
	if got, err := os.ReadFile(exe); err != nil || string(got) != "new" {
		t.Fatalf("legacy healthy rejection changed live bundle: %q, %v", got, err)
	}
}

func TestAppBundleRollbackRejectsChangedBackupTree(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app := filepath.Join(dir, "Reasonix.app")
	exe := filepath.Join(app, "Contents", "MacOS", "Reasonix")
	if err := os.MkdirAll(filepath.Dir(exe), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return exe, nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })

	backup := app + ".reasonix-update-backup"
	if _, err := PrepareAppBundleUpdate("v1", "v2", app, backup); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(app, backup); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backup, "Contents", "MacOS", "Reasonix"), []byte("tampered"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := RollbackPendingUpdate(); err == nil || !strings.Contains(err.Error(), "digest mismatch") {
		t.Fatalf("rollback error = %v, want backup digest mismatch", err)
	}
	if _, err := os.Stat(backup); err != nil {
		t.Fatalf("changed backup was removed: %v", err)
	}
	if HasPendingUpdate() == false {
		t.Fatal("pending update was lost after rejected rollback")
	}
}

func TestAppBundleRollbackCompensatesBackupChangedDuringPublish(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app := filepath.Join(dir, "Reasonix.app")
	exe := filepath.Join(app, "Contents", "MacOS", "Reasonix")
	if err := os.MkdirAll(filepath.Dir(exe), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return exe, nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })

	backup := app + ".reasonix-update-backup"
	if _, err := PrepareAppBundleUpdate("v1", "v2", app, backup); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(app, backup); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(exe), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("new"), 0o700); err != nil {
		t.Fatal(err)
	}

	originalSwap := rollbackSwapRename
	rollbackSwapRename = func(oldPath, newPath string) error {
		if err := renameRepairNodeNoReplace(oldPath, newPath); err != nil {
			return err
		}
		if oldPath == backup && newPath == app {
			return os.WriteFile(filepath.Join(app, "Contents", "MacOS", "Reasonix"), []byte("tampered-during-publish"), 0o700)
		}
		return nil
	}
	t.Cleanup(func() { rollbackSwapRename = originalSwap })

	result, err := RollbackPendingUpdate()
	if err == nil || !strings.Contains(err.Error(), "restored bundle digest mismatch") {
		t.Fatalf("rollback error = %v, want post-publish digest rejection", err)
	}
	if result.MixedInstall {
		t.Fatalf("rollback reported mixed install after successful compensation: %+v", result)
	}
	if got, err := os.ReadFile(exe); err != nil || string(got) != "new" {
		t.Fatalf("prior live bundle was not restored: %q, %v", got, err)
	}
	rejected, err := filepath.Glob(app + ".reasonix-cleanup-*")
	if err != nil || len(rejected) != 1 {
		t.Fatalf("rejected backup bundle = %v, %v", rejected, err)
	}
	rejectedExe := filepath.Join(rejected[0], "Contents", "MacOS", "Reasonix")
	if got, err := os.ReadFile(rejectedExe); err != nil || string(got) != "tampered-during-publish" {
		t.Fatalf("rejected bundle was not preserved: %q, %v", got, err)
	}
	if !HasPendingUpdate() {
		t.Fatal("failed rollback removed pending recovery state")
	}
}

func TestLegacyAppBundleRollbackWithoutTreeDigest(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app := filepath.Join(dir, "Reasonix.app")
	exe := filepath.Join(app, "Contents", "MacOS", "Reasonix")
	if err := os.MkdirAll(filepath.Dir(exe), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return exe, nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })

	backup := app + ".reasonix-update-backup"
	tx, err := PrepareAppBundleUpdate("v1", "v2", app, backup)
	if err != nil {
		t.Fatal(err)
	}
	tx.BackupTreeID = ""
	if err := overwritePendingUpdateForTest(tx); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(app, backup); err != nil {
		t.Fatal(err)
	}
	result, err := RollbackPendingUpdate()
	if err == nil || result.RolledBack ||
		!strings.Contains(err.Error(), "backup bundle identity is missing") {
		t.Fatalf("legacy rollback result = %+v, %v", result, err)
	}
	if _, err := os.Lstat(app); !os.IsNotExist(err) {
		t.Fatalf("legacy rollback recreated target without authorization: %v", err)
	}
	backupExe := filepath.Join(backup, "Contents", "MacOS", "Reasonix")
	if got, err := os.ReadFile(backupExe); err != nil || string(got) != "old" {
		t.Fatalf("legacy rollback changed backup bundle = %q, %v", got, err)
	}
	if !HasPendingUpdate() {
		t.Fatal("legacy rollback removed pending recovery state")
	}
}

func TestRepairPlanCanConfirmLegacyAppBundleRollbackWithoutTreeDigest(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app := filepath.Join(dir, "Reasonix.app")
	exe := filepath.Join(app, "Contents", "MacOS", "Reasonix")
	if err := os.MkdirAll(filepath.Dir(exe), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return exe, nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })

	backup := app + ".reasonix-update-backup"
	tx, err := PrepareAppBundleUpdate("v1", "v2", app, backup)
	if err != nil {
		t.Fatal(err)
	}
	tx.BackupTreeID = ""
	if err := overwritePendingUpdateForTest(tx); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(app, backup); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(exe), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("new"), 0o700); err != nil {
		t.Fatal(err)
	}
	plan := RepairPlan{
		SchemaVersion: RepairPlanSchemaVersion,
		Summary:       "restore legacy app bundle",
		Actions: []RepairPlanAction{{
			Type:   "rollback_update",
			Reason: "confirmed recovery",
		}},
	}
	preview, err := PreviewRepairPlan(plan, ApplyPlanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := ApplyRepairPlan(plan, ApplyPlanOptions{
		ExpectedPreviewID: RepairPlanPreviewID(plan, preview),
	})
	if err != nil || len(result.Applied) != 1 {
		t.Fatalf("confirmed legacy rollback = %+v, %v", result, err)
	}
	if got, err := os.ReadFile(exe); err != nil || string(got) != "old" {
		t.Fatalf("confirmed legacy restore = %q, %v", got, err)
	}
	if HasPendingUpdate() {
		t.Fatal("confirmed legacy rollback retained pending transaction")
	}
}

func TestRepairPlanRejectsLegacyAppBundleSymlinkBackupBeforeMutation(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app := filepath.Join(dir, "Reasonix.app")
	exe := filepath.Join(app, "Contents", "MacOS", "Reasonix")
	if err := os.MkdirAll(filepath.Dir(exe), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return exe, nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })

	backup := app + ".reasonix-update-backup"
	tx, err := PrepareAppBundleUpdate("v1", "v2", app, backup)
	if err != nil {
		t.Fatal(err)
	}
	tx.BackupTreeID = ""
	if err := overwritePendingUpdateForTest(tx); err != nil {
		t.Fatal(err)
	}
	external := filepath.Join(dir, "external-backup")
	if err := os.Rename(app, external); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, backup); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	plan := RepairPlan{
		SchemaVersion: RepairPlanSchemaVersion,
		Summary:       "restore legacy app backup",
		Actions: []RepairPlanAction{{
			Type:   "rollback_update",
			Reason: "explicitly confirmed recovery",
		}},
	}
	preview, err := PreviewRepairPlan(plan, ApplyPlanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = ApplyRepairPlan(plan, ApplyPlanOptions{
		ExpectedPreviewID: RepairPlanPreviewID(plan, preview),
	})
	if err == nil || !strings.Contains(err.Error(), "backup bundle is not a directory") {
		t.Fatalf("legacy symlink backup rollback = %v", err)
	}
	if got, err := os.Readlink(backup); err != nil || got != external {
		t.Fatalf("backup symlink changed before rejection: %q, %v", got, err)
	}
	if _, err := os.Lstat(app); !os.IsNotExist(err) {
		t.Fatalf("live app path changed before rejection: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(external, "Contents", "MacOS", "Reasonix")); err != nil || string(got) != "old" {
		t.Fatalf("external backup changed: %q, %v", got, err)
	}
	if !HasPendingUpdate() {
		t.Fatal("rejected legacy symlink backup removed pending state")
	}
}

func TestRollbackPendingUpdateRejectsUnexpectedVersion(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "reasonix-desktop")
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return filepath.Join(dir, "reasonix-guard"), nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })
	if err := os.WriteFile(target, []byte("v1"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareFileUpdate("v1", "v2", target); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("v2"), 0o700); err != nil {
		t.Fatal(err)
	}
	result, err := rollbackPendingUpdateInvocation("v3", "", "")
	if err != nil || result.RolledBack {
		t.Fatalf("rollback = %+v, %v", result, err)
	}
	got, err := os.ReadFile(target)
	if err != nil || string(got) != "v2" {
		t.Fatalf("target = %q (%v), want v2", got, err)
	}
	if !HasPendingUpdate() {
		t.Fatal("mismatched rollback removed the pending transaction")
	}
}

func TestRollbackPendingUpdateFailsClosedWhenStateLockIsUnavailable(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "reasonix-desktop")
	guard := filepath.Join(dir, "reasonix-guard")
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return guard, nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })
	if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareFileUpdate("v1", "v2", target); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("new"), 0o700); err != nil {
		t.Fatal(err)
	}

	originalAcquire := acquirePendingUpdateLock
	acquirePendingUpdateLock = func() (func(), error) {
		return nil, errors.New("injected lock failure")
	}
	t.Cleanup(func() { acquirePendingUpdateLock = originalAcquire })

	if _, err := RollbackPendingUpdate(); err == nil || !strings.Contains(err.Error(), "lock pending transaction") {
		t.Fatalf("rollback error = %v, want strict lock failure", err)
	}
	got, err := os.ReadFile(target)
	if err != nil || string(got) != "new" {
		t.Fatalf("target changed without pending lock: %q (%v)", got, err)
	}
	if !HasPendingUpdate() {
		t.Fatal("pending update was removed without pending lock")
	}
}

func TestFileUpdateRollbackRestoresReleaseUnit(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	// Resolve symlinks up front (macOS /var -> /private/var) so the recorded
	// target dir matches the resolved launcher dir in validation.
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "reasonix-desktop")
	guard := filepath.Join(dir, "reasonix-guard")
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return guard, nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })
	if err := os.WriteFile(target, []byte("old-desktop"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(guard, []byte("old-guard"), 0o700); err != nil {
		t.Fatal(err)
	}
	missingSibling := filepath.Join(dir, "reasonix-update-helper.exe")
	tx, err := PrepareFileUpdate("v1", "v2", target, guard, missingSibling)
	if err != nil {
		t.Fatal(err)
	}
	if len(tx.Files) != 3 || !tx.Files[2].MissingBefore {
		t.Fatalf("release unit files = %+v", tx.Files)
	}
	if err := os.WriteFile(target, []byte("new-desktop"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(guard, []byte("new-guard"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(missingSibling, []byte("new-helper"), 0o700); err != nil {
		t.Fatal(err)
	}
	result, err := RollbackPendingUpdate()
	if err != nil {
		t.Fatal(err)
	}
	if !result.RolledBack || result.ToVersion != "v1" {
		t.Fatalf("rollback result = %+v", result)
	}
	for path, want := range map[string]string{target: "old-desktop", guard: "old-guard"} {
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Fatalf("restored %s = %q, want %q", filepath.Base(path), got, want)
		}
	}
	if _, err := os.Stat(missingSibling); !os.IsNotExist(err) {
		t.Fatalf("new-release-only sibling survived rollback: %v", err)
	}
}

func TestCancelPendingUpdateRemovesReleaseUnitBackups(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "reasonix-desktop")
	guard := filepath.Join(dir, "reasonix-guard")
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return guard, nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })
	for path, body := range map[string]string{target: "desktop", guard: "guard"} {
		if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	tx, err := PrepareFileUpdate("v1", "v2", target, guard)
	if err != nil {
		t.Fatal(err)
	}
	if err := CancelPendingUpdate("v2"); err != nil {
		t.Fatal(err)
	}
	if HasPendingUpdate() {
		t.Fatal("pending update remains after cancel")
	}
	for _, f := range tx.Files {
		if _, err := os.Stat(f.BackupPath); !os.IsNotExist(err) {
			t.Fatalf("backup %s still exists: %v", f.BackupPath, err)
		}
	}
}

func TestPrepareFileUpdatePreservesExistingPendingTransaction(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "reasonix-desktop")
	guard := filepath.Join(dir, "reasonix-guard")
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return guard, nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })
	if err := os.WriteFile(target, []byte("v1"), 0o700); err != nil {
		t.Fatal(err)
	}
	first, err := PrepareFileUpdate("v1", "v2", target)
	if err != nil {
		t.Fatal(err)
	}
	pendingBefore, err := os.ReadFile(PendingUpdatePath())
	if err != nil {
		t.Fatal(err)
	}
	backupBefore, err := os.ReadFile(first.BackupPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("v2"), 0o700); err != nil {
		t.Fatal(err)
	}

	if _, err := PrepareFileUpdate("v2", "v3", target); err == nil ||
		!strings.Contains(err.Error(), "pending update already exists") {
		t.Fatalf("second prepare error = %v, want existing-pending rejection", err)
	}
	pendingAfter, err := os.ReadFile(PendingUpdatePath())
	if err != nil {
		t.Fatal(err)
	}
	backupAfter, err := os.ReadFile(first.BackupPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(pendingAfter, pendingBefore) {
		t.Fatal("rejected prepare changed the existing pending transaction")
	}
	if !bytes.Equal(backupAfter, backupBefore) {
		t.Fatalf("rejected prepare changed rollback backup: got %q, want %q", backupAfter, backupBefore)
	}
}

func TestExactUpdateTransitionsIgnoreLaterSameVersionTransaction(t *testing.T) {
	t.Run("cancel", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("REASONIX_HOME", home)
		dir, err := filepath.EvalSymlinks(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(dir, "reasonix-desktop")
		guard := filepath.Join(dir, "reasonix-guard")
		originalExecutable := repairExecutable
		repairExecutable = func() (string, error) { return guard, nil }
		t.Cleanup(func() { repairExecutable = originalExecutable })
		if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
			t.Fatal(err)
		}
		first, err := PrepareFileUpdate("v1", "v2", target)
		if err != nil {
			t.Fatal(err)
		}
		second := *first
		second.CreatedAt = time.Now().UTC().Add(time.Second).Format(time.RFC3339Nano)
		if err := overwritePendingUpdateForTest(&second); err != nil {
			t.Fatal(err)
		}
		if err := CancelPendingUpdateMatching(first.ToVersion, first.CreatedAt); err != nil {
			t.Fatal(err)
		}
		current, err := ReadPendingUpdate()
		if err != nil || current.CreatedAt != second.CreatedAt {
			t.Fatalf("stale cancel changed current transaction: %+v, %v", current, err)
		}
	})

	t.Run("healthy", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("REASONIX_HOME", home)
		dir, err := filepath.EvalSymlinks(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(dir, "reasonix-desktop")
		guard := filepath.Join(dir, "reasonix-guard")
		originalExecutable := repairExecutable
		repairExecutable = func() (string, error) { return guard, nil }
		t.Cleanup(func() { repairExecutable = originalExecutable })
		if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
			t.Fatal(err)
		}
		first, err := PrepareFileUpdate("v1", "v2", target)
		if err != nil {
			t.Fatal(err)
		}
		second := *first
		second.CreatedAt = time.Now().UTC().Add(time.Second).Format(time.RFC3339Nano)
		if err := overwritePendingUpdateForTest(&second); err != nil {
			t.Fatal(err)
		}
		if _, err := RecordClaimedFileUpdateInstalled(&second, fileUpdateInstallReceiptsForTest(t, &second)...); err != nil {
			t.Fatal(err)
		}
		if err := MarkUpdateHealthyMatching("v2", first.CreatedAt); err != nil {
			t.Fatal(err)
		}
		current, err := ReadPendingUpdate()
		if err != nil || current.CreatedAt != second.CreatedAt {
			t.Fatalf("stale health commit changed current transaction: %+v, %v", current, err)
		}
		if err := MarkUpdateHealthyMatching("v2", second.CreatedAt); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadPendingUpdate(); !os.IsNotExist(err) {
			t.Fatalf("matching health commit left pending transaction: %v", err)
		}
	})

	t.Run("rollback", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("REASONIX_HOME", home)
		dir, err := filepath.EvalSymlinks(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(dir, "reasonix-desktop")
		guard := filepath.Join(dir, "reasonix-guard")
		originalExecutable := repairExecutable
		repairExecutable = func() (string, error) { return guard, nil }
		t.Cleanup(func() { repairExecutable = originalExecutable })
		if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
			t.Fatal(err)
		}
		first, err := PrepareFileUpdate("v1", "v2", target)
		if err != nil {
			t.Fatal(err)
		}
		second := *first
		second.CreatedAt = time.Now().UTC().Add(time.Second).Format(time.RFC3339Nano)
		if err := overwritePendingUpdateForTest(&second); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte("new"), 0o700); err != nil {
			t.Fatal(err)
		}
		result, err := RollbackPendingUpdateMatching(first.ToVersion, first.CreatedAt)
		if err != nil || result.RolledBack {
			t.Fatalf("stale rollback = %+v, %v", result, err)
		}
		if got, err := os.ReadFile(target); err != nil || string(got) != "new" {
			t.Fatalf("stale rollback changed target: %q, %v", got, err)
		}
		result, err = RollbackPendingUpdateMatching(second.ToVersion, second.CreatedAt)
		if err != nil || !result.RolledBack {
			t.Fatalf("matching rollback = %+v, %v", result, err)
		}
	})
}

func TestUpdateTransitionsRecheckPendingAfterTargetLock(t *testing.T) {
	t.Run("rollback", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("REASONIX_HOME", home)
		dir, err := filepath.EvalSymlinks(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(dir, "reasonix-desktop")
		guard := filepath.Join(dir, "reasonix-guard")
		originalExecutable := repairExecutable
		repairExecutable = func() (string, error) { return guard, nil }
		t.Cleanup(func() { repairExecutable = originalExecutable })
		if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
			t.Fatal(err)
		}
		tx, err := PrepareFileUpdate("v1", "v2", target)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte("new"), 0o700); err != nil {
			t.Fatal(err)
		}
		changed := *tx
		changed.FromVersion = "rewritten"
		originalHook := repairMutationBeforeLock
		var once sync.Once
		repairMutationBeforeLock = func([]string) {
			once.Do(func() {
				if err := overwritePendingUpdateForTest(&changed); err != nil {
					t.Fatal(err)
				}
			})
		}
		t.Cleanup(func() { repairMutationBeforeLock = originalHook })

		result, err := RollbackPendingUpdateExact(tx)
		if err == nil || result.RolledBack || !strings.Contains(err.Error(), "changed while waiting") {
			t.Fatalf("rollback after pending rewrite = %+v, %v", result, err)
		}
		if got, err := os.ReadFile(target); err != nil || string(got) != "new" {
			t.Fatalf("rollback touched target after pending rewrite: %q, %v", got, err)
		}
		current, err := ReadPendingUpdate()
		if err != nil || current.FromVersion != changed.FromVersion {
			t.Fatalf("rewritten pending transaction = %+v, %v", current, err)
		}
	})

	t.Run("healthy", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("REASONIX_HOME", home)
		dir, err := filepath.EvalSymlinks(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(dir, "reasonix-desktop")
		guard := filepath.Join(dir, "reasonix-guard")
		originalExecutable := repairExecutable
		repairExecutable = func() (string, error) { return guard, nil }
		t.Cleanup(func() { repairExecutable = originalExecutable })
		if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
			t.Fatal(err)
		}
		tx, err := PrepareFileUpdate("v1", "v2", target)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte("new"), 0o700); err != nil {
			t.Fatal(err)
		}
		changed := *tx
		changed.FromVersion = "rewritten"
		originalHook := repairMutationBeforeLock
		var once sync.Once
		repairMutationBeforeLock = func([]string) {
			once.Do(func() {
				if err := overwritePendingUpdateForTest(&changed); err != nil {
					t.Fatal(err)
				}
			})
		}
		t.Cleanup(func() { repairMutationBeforeLock = originalHook })

		if err := MarkUpdateHealthyMatching(tx.ToVersion, tx.CreatedAt); err == nil ||
			!strings.Contains(err.Error(), "changed while waiting") {
			t.Fatalf("healthy commit after pending rewrite = %v", err)
		}
		if _, err := ReadPendingUpdate(); err != nil {
			t.Fatalf("healthy commit removed rewritten transaction: %v", err)
		}
		if _, err := os.Stat(tx.BackupPath); err != nil {
			t.Fatalf("healthy commit removed backup after pending rewrite: %v", err)
		}
	})
}

func TestGenericUpdateTransitionsBindPendingBeforeWaitingForLock(t *testing.T) {
	t.Run("rollback", func(t *testing.T) {
		t.Setenv("REASONIX_HOME", t.TempDir())
		dir := t.TempDir()
		target := filepath.Join(dir, "reasonix-desktop")
		guard := filepath.Join(dir, "reasonix-guard")
		originalExecutable := repairExecutable
		repairExecutable = func() (string, error) { return guard, nil }
		t.Cleanup(func() { repairExecutable = originalExecutable })
		if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
			t.Fatal(err)
		}
		tx, err := PrepareFileUpdate("v1", "v2", target)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte("new"), 0o700); err != nil {
			t.Fatal(err)
		}
		replacement := *tx
		replacement.FromVersion = "replacement"
		originalAcquire := acquirePendingUpdateLock
		acquirePendingUpdateLock = func() (func(), error) {
			if err := overwritePendingUpdateForTest(&replacement); err != nil {
				t.Fatal(err)
			}
			return func() {}, nil
		}
		t.Cleanup(func() { acquirePendingUpdateLock = originalAcquire })

		result, err := RollbackPendingUpdate()
		if err == nil || result.RolledBack ||
			!strings.Contains(err.Error(), "pending transaction changed") {
			t.Fatalf("generic rollback after pending-lock replacement = %+v, %v", result, err)
		}
		if got, err := os.ReadFile(target); err != nil || string(got) != "new" {
			t.Fatalf("replacement transaction target changed: %q, %v", got, err)
		}
	})

	t.Run("healthy", func(t *testing.T) {
		t.Setenv("REASONIX_HOME", t.TempDir())
		dir := t.TempDir()
		target := filepath.Join(dir, "reasonix-desktop")
		guard := filepath.Join(dir, "reasonix-guard")
		originalExecutable := repairExecutable
		repairExecutable = func() (string, error) { return guard, nil }
		t.Cleanup(func() { repairExecutable = originalExecutable })
		if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
			t.Fatal(err)
		}
		tx, err := PrepareFileUpdate("v1", "v2", target)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte("new"), 0o700); err != nil {
			t.Fatal(err)
		}
		if _, err := RecordClaimedFileUpdateInstalled(tx, fileUpdateInstallReceiptsForTest(t, tx)...); err != nil {
			t.Fatal(err)
		}
		replacement := *tx
		replacement.FromVersion = "replacement"
		originalAcquire := acquirePendingUpdateLock
		acquirePendingUpdateLock = func() (func(), error) {
			if err := overwritePendingUpdateForTest(&replacement); err != nil {
				t.Fatal(err)
			}
			return func() {}, nil
		}
		t.Cleanup(func() { acquirePendingUpdateLock = originalAcquire })

		if err := MarkUpdateHealthy("v2"); err == nil ||
			!strings.Contains(err.Error(), "pending transaction changed") {
			t.Fatalf("generic healthy commit after pending-lock replacement = %v", err)
		}
		if _, err := os.Stat(tx.BackupPath); err != nil {
			t.Fatalf("generic healthy commit removed prior backup: %v", err)
		}
	})

	t.Run("cancel", func(t *testing.T) {
		t.Setenv("REASONIX_HOME", t.TempDir())
		dir := t.TempDir()
		target := filepath.Join(dir, "reasonix-desktop")
		guard := filepath.Join(dir, "reasonix-guard")
		originalExecutable := repairExecutable
		repairExecutable = func() (string, error) { return guard, nil }
		t.Cleanup(func() { repairExecutable = originalExecutable })
		if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
			t.Fatal(err)
		}
		tx, err := PrepareFileUpdate("v1", "v2", target)
		if err != nil {
			t.Fatal(err)
		}
		replacement := *tx
		replacement.FromVersion = "replacement"
		originalAcquire := acquirePendingUpdateLock
		acquirePendingUpdateLock = func() (func(), error) {
			if err := overwritePendingUpdateForTest(&replacement); err != nil {
				t.Fatal(err)
			}
			return func() {}, nil
		}
		t.Cleanup(func() { acquirePendingUpdateLock = originalAcquire })

		if err := CancelPendingUpdate("v2"); err == nil ||
			!strings.Contains(err.Error(), "pending transaction changed") {
			t.Fatalf("generic cancel after pending-lock replacement = %v", err)
		}
		if _, err := os.Stat(tx.BackupPath); err != nil {
			t.Fatalf("generic cancel removed prior backup: %v", err)
		}
	})
}

func TestCancelPendingFileUpdateRejectsLiveDrift(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "reasonix-desktop")
	guard := filepath.Join(dir, "reasonix-guard")
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return guard, nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })
	if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	tx, err := PrepareFileUpdate("v1", "v2", target)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("unconfirmed"), 0o700); err != nil {
		t.Fatal(err)
	}

	if err := CancelPendingUpdateExact(tx); err == nil {
		t.Fatal("cancel accepted a release file changed after prepare")
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "unconfirmed" {
		t.Fatalf("cancel changed drifted release file: %q, %v", got, err)
	}
	if _, err := ReadPendingUpdate(); err != nil {
		t.Fatalf("cancel removed pending recovery state: %v", err)
	}
	if _, err := os.Stat(tx.BackupPath); err != nil {
		t.Fatalf("cancel removed rollback backup: %v", err)
	}
}

func TestRecoverFailedInstallRejectsStaleSameVersionIdentity(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "reasonix-desktop")
	guard := filepath.Join(dir, "reasonix-guard")
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return guard, nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })
	if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	first, err := PrepareFileUpdate("v1", "v2", target)
	if err != nil {
		t.Fatal(err)
	}
	second := *first
	second.CreatedAt = time.Now().UTC().Add(-time.Second).Format(time.RFC3339Nano)
	if err := overwritePendingUpdateForTest(&second); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("new"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := markUpdateApplyFailed(
		first.ToVersion,
		first.CreatedAt,
		UpdateTransactionID(first),
		"stale failure",
	); err != nil {
		t.Fatal(err)
	}
	result, failure, err := RecoverFailedInstall()
	if err != nil || failure == nil || result.RolledBack {
		t.Fatalf("recover stale marker = %+v, %+v, %v", result, failure, err)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "new" {
		t.Fatalf("stale marker rolled back current transaction: %q, %v", got, err)
	}
	current, err := ReadPendingUpdate()
	if err != nil || current.CreatedAt != second.CreatedAt {
		t.Fatalf("current transaction after stale marker = %+v, %v", current, err)
	}
	if _, ok := ReadUpdateApplyFailure(); ok {
		t.Fatal("stale exact marker was not cleared")
	}
}

func TestMarkUpdateHealthyExactRejectsRewrittenTransaction(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "reasonix-desktop")
	guard := filepath.Join(dir, "reasonix-guard")
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return guard, nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })
	if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	tx, err := PrepareFileUpdate("v1", "v2", target)
	if err != nil {
		t.Fatal(err)
	}
	changed := *tx
	changed.FromVersion = "rewritten"
	if err := overwritePendingUpdateForTest(&changed); err != nil {
		t.Fatal(err)
	}

	if err := MarkUpdateHealthyExact(tx.ToVersion, tx.CreatedAt, UpdateTransactionID(tx)); err == nil ||
		!strings.Contains(err.Error(), "pending transaction changed") {
		t.Fatalf("healthy exact error = %v, want full transaction rejection", err)
	}
	current, err := ReadPendingUpdate()
	if err != nil || current.FromVersion != changed.FromVersion {
		t.Fatalf("rewritten transaction = %+v, %v", current, err)
	}
	if _, err := os.Stat(tx.BackupPath); err != nil {
		t.Fatalf("exact health rejection removed backup: %v", err)
	}
}

func TestRecoverFailedInstallRejectsRewrittenExactTransaction(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "reasonix-desktop")
	guard := filepath.Join(dir, "reasonix-guard")
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return guard, nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })
	if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	tx, err := PrepareFileUpdate("v1", "v2", target)
	if err != nil {
		t.Fatal(err)
	}
	if err := MarkUpdateApplyFailedExact(tx, "installer failed"); err != nil {
		t.Fatal(err)
	}
	changed := *tx
	changed.FromVersion = "rewritten"
	if err := overwritePendingUpdateForTest(&changed); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("new"), 0o700); err != nil {
		t.Fatal(err)
	}

	result, failure, err := RecoverFailedInstall()
	if err != nil || failure == nil || result.RolledBack {
		t.Fatalf("recover rewritten marker = %+v, %+v, %v", result, failure, err)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "new" {
		t.Fatalf("stale marker rolled back rewritten transaction: %q, %v", got, err)
	}
	current, err := ReadPendingUpdate()
	if err != nil || current.FromVersion != changed.FromVersion {
		t.Fatalf("rewritten transaction after stale marker = %+v, %v", current, err)
	}
	if _, ok := ReadUpdateApplyFailure(); ok {
		t.Fatal("stale full-identity marker was not cleared")
	}
}

func TestClearUpdateApplyFailureExactPreservesDifferentTransactionMarker(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	first := &UpdateTransaction{
		SchemaVersion: 1,
		ToVersion:     "v2",
		Platform:      "windows/amd64",
		TargetKind:    "file",
		TargetPath:    filepath.Join(t.TempDir(), "reasonix-desktop"),
		CreatedAt:     "2026-07-29T00:00:00Z",
	}
	second := *first
	second.CreatedAt = "2026-07-29T00:00:01Z"
	if err := MarkUpdateApplyFailedExact(first, "publish interrupted"); err != nil {
		t.Fatal(err)
	}
	if err := ClearUpdateApplyFailureExact(&second); err == nil {
		t.Fatal("different transaction cleared update failure marker")
	}
	if failure, ok := ReadUpdateApplyFailure(); !ok ||
		failure.UpdateTransactionID != UpdateTransactionID(first) {
		t.Fatalf("preserved failure marker = %+v, present=%v", failure, ok)
	}
	if err := ClearUpdateApplyFailureExact(first); err != nil {
		t.Fatal(err)
	}
	if _, ok := ReadUpdateApplyFailure(); ok {
		t.Fatal("matching transaction marker was not cleared")
	}
}

// TestRecoverFailedInstallRollsBackAndClearsMarker pins the Windows helper
// handoff contract: an installer failure recorded by the update helper makes
// Guard restore the release unit on its next launch, clearing both the marker
// and the pending transaction.
func TestRecoverFailedInstallRollsBackAndClearsMarker(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "reasonix-desktop")
	guard := filepath.Join(dir, "reasonix-guard")
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return guard, nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })
	for path, body := range map[string]string{target: "old-desktop", guard: "old-guard"} {
		if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := PrepareFileUpdate("v1", "v2", target, guard); err != nil {
		t.Fatal(err)
	}
	// Simulate a partial NSIS run followed by the helper's failure marker.
	if err := os.WriteFile(guard, []byte("new-guard"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := MarkUpdateApplyFailed("v2", "installer exited with 1"); err != nil {
		t.Fatal(err)
	}
	result, failure, err := RecoverFailedInstall()
	if err != nil {
		t.Fatal(err)
	}
	if failure == nil || !result.RolledBack || result.ToVersion != "v1" {
		t.Fatalf("recover result = %+v failure = %+v", result, failure)
	}
	for path, want := range map[string]string{target: "old-desktop", guard: "old-guard"} {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want {
			t.Fatalf("restored %s = %q (%v), want %q", filepath.Base(path), got, err, want)
		}
	}
	if _, ok := ReadUpdateApplyFailure(); ok {
		t.Fatal("failure marker survived a successful rollback")
	}
	if HasPendingUpdate() {
		t.Fatal("pending update survived the rollback")
	}
	// Subsequent launches are a no-op.
	if result, failure, err := RecoverFailedInstall(); err != nil || failure != nil || result.RolledBack {
		t.Fatalf("second recover = %+v %+v %v", result, failure, err)
	}
}

func TestRecoverFailedInstallAfterInstalledSidecarCommit(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "reasonix-desktop")
	guard := filepath.Join(dir, "reasonix-guard")
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return guard, nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })
	for path, body := range map[string]string{target: "old-desktop", guard: "old-guard"} {
		if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	prepared, err := PrepareFileUpdate("v1", "v2", target, guard)
	if err != nil {
		t.Fatal(err)
	}
	for path, body := range map[string]string{target: "new-desktop", guard: "new-guard"} {
		if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := MarkUpdateApplyFailedExact(prepared, "helper crashed after publish"); err != nil {
		t.Fatal(err)
	}
	recorded, err := RecordClaimedFileUpdateInstalled(prepared, fileUpdateInstallReceiptsForTest(t, prepared)...)
	if err != nil {
		t.Fatal(err)
	}
	sidecar := installedFileUpdateStatePath(recorded)
	if _, err := os.Stat(sidecar); err != nil {
		t.Fatalf("installed-state sidecar was not committed: %v", err)
	}
	if _, ok := ReadUpdateApplyFailure(); !ok {
		t.Fatal("exact failure marker was not committed")
	}
	if !HasPendingUpdate() {
		t.Fatal("pending transaction disappeared before recovery")
	}

	result, failure, err := RecoverFailedInstall()
	if err != nil {
		t.Fatal(err)
	}
	if failure == nil || !result.RolledBack || result.ToVersion != "v1" {
		t.Fatalf("recover result = %+v failure = %+v", result, failure)
	}
	for path, want := range map[string]string{target: "old-desktop", guard: "old-guard"} {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want {
			t.Fatalf("restored %s = %q (%v), want %q", filepath.Base(path), got, err, want)
		}
	}
	if HasPendingUpdate() {
		t.Fatal("pending transaction survived sidecar-backed recovery")
	}
	if _, ok := ReadUpdateApplyFailure(); ok {
		t.Fatal("failure marker survived sidecar-backed recovery")
	}
	if _, err := os.Stat(sidecar); !os.IsNotExist(err) {
		t.Fatalf("installed-state sidecar survived recovery: %v", err)
	}
}

// A stale marker with nothing to roll back must be cleared, not retried
// forever.
func TestRecoverFailedInstallClearsStaleMarker(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	if err := MarkUpdateApplyFailed("v2", "installer exited with 1"); err != nil {
		t.Fatal(err)
	}
	result, failure, err := RecoverFailedInstall()
	if err != nil || failure == nil || result.RolledBack {
		t.Fatalf("recover = %+v %+v %v", result, failure, err)
	}
	if _, ok := ReadUpdateApplyFailure(); ok {
		t.Fatal("stale marker was not cleared")
	}
}

func TestRecoverFailedInstallPreservesMarkerRecreatedDuringCleanup(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	if err := MarkUpdateApplyFailed("v2", "old failure"); err != nil {
		t.Fatal(err)
	}
	originalHook := updateCleanupAfterRename
	injected := false
	updateCleanupAfterRename = func(oldpath, _ string) {
		if !injected && oldpath == updateApplyFailurePath() {
			injected = true
			if err := markUpdateApplyFailed("v3", "", "", "new failure"); err != nil {
				t.Errorf("recreate failure marker: %v", err)
			}
		}
	}
	t.Cleanup(func() { updateCleanupAfterRename = originalHook })

	if _, failure, err := RecoverFailedInstall(); err != nil || failure == nil {
		t.Fatalf("recover = %+v, %v", failure, err)
	}
	current, ok := ReadUpdateApplyFailure()
	if !ok || current.ToVersion != "v3" || current.Reason != "new failure" {
		t.Fatalf("concurrent failure marker = %+v, present=%v", current, ok)
	}
}

func TestRecoverFailedInstallIgnoresMarkerForAnotherVersion(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "reasonix-desktop")
	guard := filepath.Join(dir, "reasonix-guard")
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return guard, nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })
	if err := os.WriteFile(target, []byte("v2"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareFileUpdate("v2", "v3", target); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("v3"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := markUpdateApplyFailed("v2", "", "", "stale installer failure"); err != nil {
		t.Fatal(err)
	}
	result, failure, err := RecoverFailedInstall()
	if err != nil || failure == nil || result.RolledBack {
		t.Fatalf("recover = %+v %+v %v", result, failure, err)
	}
	got, err := os.ReadFile(target)
	if err != nil || string(got) != "v3" {
		t.Fatalf("unrelated update was rolled back: %q (%v)", got, err)
	}
	if !HasPendingUpdate() {
		t.Fatal("unrelated pending update was removed")
	}
	if _, ok := ReadUpdateApplyFailure(); ok {
		t.Fatal("stale failure marker was not cleared")
	}
}

func TestMarkUpdateApplyFailedPreservesNewMarkerAfterPendingReplacement(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	dir := t.TempDir()
	target := filepath.Join(dir, "reasonix-desktop")
	guard := filepath.Join(dir, "reasonix-guard")
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return guard, nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })
	if err := os.WriteFile(target, []byte("v1"), 0o700); err != nil {
		t.Fatal(err)
	}
	first, err := PrepareFileUpdate("v1", "v2", target)
	if err != nil {
		t.Fatal(err)
	}
	second := *first
	second.CreatedAt = time.Now().UTC().Add(time.Second).Format(time.RFC3339Nano)

	originalLock := acquirePendingUpdateLock
	var hookErr error
	acquirePendingUpdateLock = func() (func(), error) {
		if hookErr = overwritePendingUpdateForTest(&second); hookErr == nil {
			hookErr = MarkUpdateApplyFailedExact(&second, "new transaction failed")
		}
		return func() {}, nil
	}
	t.Cleanup(func() { acquirePendingUpdateLock = originalLock })

	if err := MarkUpdateApplyFailed(first.ToVersion, "old transaction failed"); err == nil ||
		!strings.Contains(err.Error(), "changed while waiting") {
		t.Fatalf("stale failure marker creation = %v", err)
	}
	if hookErr != nil {
		t.Fatalf("replace pending transaction and marker: %v", hookErr)
	}
	failure, ok := ReadUpdateApplyFailure()
	if !ok || failure.UpdateTransactionID != UpdateTransactionID(&second) ||
		failure.Reason != "new transaction failed" {
		t.Fatalf("new transaction marker = %+v, present=%v", failure, ok)
	}
	current, err := ReadPendingUpdate()
	if err != nil || UpdateTransactionID(current) != UpdateTransactionID(&second) {
		t.Fatalf("pending transaction = %+v, %v", current, err)
	}
}

func TestRecoverFailedInstallRejectsLegacyMarkerRegardlessVersionAndTime(t *testing.T) {
	for _, tc := range []struct {
		name          string
		markerOlder   bool
		wantContents  string
		fromVersion   string
		targetVersion string
		markerVersion string
	}{
		{name: "older marker is stale", markerOlder: true, wantContents: "v3", fromVersion: "v2", targetVersion: "v3"},
		{name: "newer matching marker is still unbound", wantContents: "v2", fromVersion: "v1", targetVersion: "v2", markerVersion: "v2"},
		{name: "newer versionless marker is incomplete", wantContents: "v2", fromVersion: "v1", targetVersion: "v2"},
		{name: "older same-version marker is stale", markerOlder: true, wantContents: "v3", fromVersion: "v2", targetVersion: "v3", markerVersion: "v3"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("REASONIX_HOME", home)
			dir, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(dir, "reasonix-desktop")
			guard := filepath.Join(dir, "reasonix-guard")
			originalExecutable := repairExecutable
			repairExecutable = func() (string, error) { return guard, nil }
			t.Cleanup(func() { repairExecutable = originalExecutable })
			if err := os.WriteFile(target, []byte(tc.fromVersion), 0o700); err != nil {
				t.Fatal(err)
			}
			tx, err := PrepareFileUpdate(tc.fromVersion, tc.targetVersion, target)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(target, []byte(tc.targetVersion), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := markUpdateApplyFailed(tc.markerVersion, "", "", "installer failure"); err != nil {
				t.Fatal(err)
			}
			failure, ok := ReadUpdateApplyFailure()
			if !ok {
				t.Fatal("legacy failure marker missing")
			}
			txAt, err := time.Parse(time.RFC3339Nano, tx.CreatedAt)
			if err != nil {
				t.Fatal(err)
			}
			if tc.markerOlder {
				failure.RecordedAt = txAt.Add(-time.Second).Format(time.RFC3339Nano)
			} else {
				failure.RecordedAt = txAt.Add(time.Second).Format(time.RFC3339Nano)
			}
			b, err := json.Marshal(failure)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(updateApplyFailurePath(), append(b, '\n'), 0o600); err != nil {
				t.Fatal(err)
			}
			result, failure, err := RecoverFailedInstall()
			if err != nil || failure == nil || result.RolledBack {
				t.Fatalf("recover = %+v %+v %v", result, failure, err)
			}
			got, err := os.ReadFile(target)
			if err != nil || string(got) != tc.wantContents {
				t.Fatalf("target = %q (%v), want %q", got, err, tc.wantContents)
			}
		})
	}
}

func TestHealthyUpdateRemovesBackup(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	target := filepath.Join(t.TempDir(), "reasonix-desktop")
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return filepath.Join(filepath.Dir(target), "reasonix-guard"), nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })
	if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	tx, err := PrepareFileUpdate("v1", "v2", target)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RecordClaimedFileUpdateInstalled(tx, fileUpdateInstallReceiptsForTest(t, tx)...); err != nil {
		t.Fatal(err)
	}
	if err := MarkUpdateHealthy("v1"); err != nil {
		t.Fatal(err)
	}
	if !HasPendingUpdate() {
		t.Fatal("mismatched version committed pending update")
	}
	if err := MarkUpdateHealthy("v2"); err != nil {
		t.Fatal(err)
	}
	if HasPendingUpdate() {
		t.Fatal("pending update remains after health confirmation")
	}
	if _, err := os.Stat(tx.BackupPath); !os.IsNotExist(err) {
		t.Fatalf("backup still exists: %v", err)
	}
}

func TestMarkUpdateHealthyRejectsFileTransactionWithoutInstalledBinding(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	dir := t.TempDir()
	target := filepath.Join(dir, "reasonix-desktop")
	guard := filepath.Join(dir, "reasonix-guard")
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return guard, nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })
	if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	tx, err := PrepareFileUpdate("v1", "v2", target)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("new"), 0o700); err != nil {
		t.Fatal(err)
	}

	if err := MarkUpdateHealthyExact(tx.ToVersion, tx.CreatedAt, UpdateTransactionID(tx)); err == nil ||
		!strings.Contains(err.Error(), "installed release-unit state is missing") {
		t.Fatalf("healthy commit without installed binding = %v", err)
	}
	if _, err := ReadPendingUpdate(); err != nil {
		t.Fatalf("healthy rejection removed pending state: %v", err)
	}
	if _, err := os.Stat(tx.BackupPath); err != nil {
		t.Fatalf("healthy rejection removed backup: %v", err)
	}
}

func TestRecordClaimedFileUpdateInstalledBindsCompleteReleaseUnit(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "reasonix-desktop")
	guard := filepath.Join(dir, "reasonix-guard")
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return guard, nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })
	for path, body := range map[string]string{target: "old-desktop", guard: "old-guard"} {
		if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	prepared, err := PrepareFileUpdate("v1", "v2", target, guard)
	if err != nil {
		t.Fatal(err)
	}
	for path, body := range map[string]string{target: "new-desktop", guard: "new-guard"} {
		if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	recorded, err := RecordClaimedFileUpdateInstalled(prepared, fileUpdateInstallReceiptsForTest(t, prepared)...)
	if err != nil {
		t.Fatal(err)
	}
	if len(recorded.Files) != 2 {
		t.Fatalf("recorded release unit = %+v", recorded.Files)
	}
	record, err := readInstalledFileUpdateState(recorded)
	if err != nil {
		t.Fatal(err)
	}
	if len(record.InstalledStateIDs) != len(recorded.Files) {
		t.Fatalf("installed sidecar = %+v", record)
	}
	for i, stateID := range record.InstalledStateIDs {
		if stateID == "" {
			t.Fatalf("installed state missing for %s", recorded.Files[i].TargetPath)
		}
	}
	persisted, err := ReadPendingUpdate()
	if err != nil || UpdateTransactionID(persisted) != UpdateTransactionID(recorded) {
		t.Fatalf("persisted installed release unit = %+v, %v", persisted, err)
	}

	if err := os.WriteFile(guard, []byte("drifted-guard"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := MarkUpdateHealthyExact(recorded.ToVersion, recorded.CreatedAt, UpdateTransactionID(recorded)); err == nil ||
		!strings.Contains(err.Error(), "installed release file") {
		t.Fatalf("healthy error = %v, want installed sibling drift rejection", err)
	}
	if !HasPendingUpdate() {
		t.Fatal("drifted installed release unit removed pending state")
	}
	if _, err := os.Stat(prepared.BackupPath); err != nil {
		t.Fatalf("drifted installed release unit removed rollback backup: %v", err)
	}

	if err := os.WriteFile(guard, []byte("new-guard"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := MarkUpdateHealthyExact(recorded.ToVersion, recorded.CreatedAt, UpdateTransactionID(recorded)); err != nil {
		t.Fatalf("healthy exact after restoring installed state: %v", err)
	}
	if HasPendingUpdate() {
		t.Fatal("verified installed release unit retained pending state")
	}
	if _, err := os.Stat(installedFileUpdateStatePath(recorded)); !os.IsNotExist(err) {
		t.Fatalf("installed-state sidecar survived healthy commit: %v", err)
	}
}

func TestRecordClaimedFileUpdateInstalledBindsOptionalMissingSibling(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "reasonix-desktop")
	guard := filepath.Join(dir, "reasonix-guard")
	optionalCLI := filepath.Join(dir, "reasonix")
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return guard, nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })
	for path, body := range map[string]string{target: "old-desktop", guard: "old-guard"} {
		if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	prepared, err := PrepareFileUpdate("v1", "v2", target, guard, optionalCLI)
	if err != nil {
		t.Fatal(err)
	}
	for path, body := range map[string]string{target: "new-desktop", guard: "new-guard"} {
		if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	recorded, err := RecordClaimedFileUpdateInstalled(prepared, fileUpdateInstallReceiptsForTest(t, prepared)...)
	if err != nil {
		t.Fatal(err)
	}
	record, err := readInstalledFileUpdateState(recorded)
	if err != nil {
		t.Fatal(err)
	}
	if len(recorded.Files) != 3 || !recorded.Files[2].MissingBefore ||
		len(record.InstalledStateIDs) != 3 || record.InstalledStateIDs[2] == "" {
		t.Fatalf("optional installed release state = %+v", recorded.Files)
	}
	if err := MarkUpdateHealthyExact(recorded.ToVersion, recorded.CreatedAt, UpdateTransactionID(recorded)); err != nil {
		t.Fatalf("healthy exact with still-missing optional sibling: %v", err)
	}
}

func TestReadPendingUpdateRejectsPartialInstalledReleaseUnitState(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "reasonix-desktop")
	guard := filepath.Join(dir, "reasonix-guard")
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return guard, nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })
	for _, path := range []string{target, guard} {
		if err := os.WriteFile(path, []byte("old"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	tx, err := PrepareFileUpdate("v1", "v2", target, guard)
	if err != nil {
		t.Fatal(err)
	}
	tx.Files[0].InstalledStateID = strings.Repeat("a", 64)
	if err := overwritePendingUpdateForTest(tx); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadPendingUpdate(); err == nil ||
		!strings.Contains(err.Error(), "installed release-unit state is incomplete") {
		t.Fatalf("ReadPendingUpdate error = %v, want partial installed-state rejection", err)
	}
}

func TestHealthyFileUpdateRejectsBackupDrift(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	target := filepath.Join(t.TempDir(), "reasonix-desktop")
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return filepath.Join(filepath.Dir(target), "reasonix-guard"), nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })
	if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	tx, err := PrepareFileUpdate("v1", "v2", target)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("new"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := RecordClaimedFileUpdateInstalled(tx, fileUpdateInstallReceiptsForTest(t, tx)...); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tx.BackupPath, []byte("unrelated"), 0o700); err != nil {
		t.Fatal(err)
	}

	if err := MarkUpdateHealthyMatching(tx.ToVersion, tx.CreatedAt); err == nil {
		t.Fatal("healthy commit accepted a changed file backup")
	}
	if _, err := ReadPendingUpdate(); err != nil {
		t.Fatalf("rejected healthy commit removed pending transaction: %v", err)
	}
	if got, err := os.ReadFile(tx.BackupPath); err != nil || string(got) != "unrelated" {
		t.Fatalf("changed backup was removed: %q, %v", got, err)
	}
}

func TestHealthyAppUpdateRejectsBackupDrift(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app := filepath.Join(dir, "Reasonix.app")
	exe := filepath.Join(app, "Contents", "MacOS", "Reasonix")
	if err := os.MkdirAll(filepath.Dir(exe), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return exe, nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })

	backup := app + ".reasonix-update-backup"
	tx, err := PrepareAppBundleUpdate("v1", "v2", app, backup)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(app, backup); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(exe), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("new"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backup, "Contents", "MacOS", "Reasonix"), []byte("unrelated"), 0o700); err != nil {
		t.Fatal(err)
	}

	if err := MarkUpdateHealthyMatching(tx.ToVersion, tx.CreatedAt); err == nil {
		t.Fatal("healthy commit accepted a changed app backup")
	}
	if _, err := ReadPendingUpdate(); err != nil {
		t.Fatalf("rejected healthy commit removed pending transaction: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(backup, "Contents", "MacOS", "Reasonix")); err != nil || string(got) != "unrelated" {
		t.Fatalf("changed app backup was removed: %q, %v", got, err)
	}
}

func TestMarkUpdateHealthyPreservesPendingRecreatedDuringCleanup(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "reasonix-desktop")
	guard := filepath.Join(dir, "reasonix-guard")
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return guard, nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })
	if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	tx, err := PrepareFileUpdate("v1", "v2", target)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RecordClaimedFileUpdateInstalled(tx, fileUpdateInstallReceiptsForTest(t, tx)...); err != nil {
		t.Fatal(err)
	}
	replacement := *tx
	replacement.CreatedAt = time.Now().UTC().Add(time.Second).Format(time.RFC3339Nano)

	originalHook := updateCleanupAfterRename
	injected := false
	updateCleanupAfterRename = func(oldpath, _ string) {
		if !injected && oldpath == PendingUpdatePath() {
			injected = true
			if err := overwritePendingUpdateForTest(&replacement); err != nil {
				t.Errorf("recreate pending transaction: %v", err)
			}
		}
	}
	t.Cleanup(func() { updateCleanupAfterRename = originalHook })

	if err := MarkUpdateHealthyExact(tx.ToVersion, tx.CreatedAt, UpdateTransactionID(tx)); err != nil {
		t.Fatal(err)
	}
	current, err := readPendingUpdateUnchecked()
	if err != nil || current.CreatedAt != replacement.CreatedAt {
		t.Fatalf("concurrent pending transaction = %+v, %v", current, err)
	}
}

func TestMarkUpdateHealthyPreservesBackupRecreatedDuringCleanup(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "reasonix-desktop")
	guard := filepath.Join(dir, "reasonix-guard")
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return guard, nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })
	if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	tx, err := PrepareFileUpdate("v1", "v2", target)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RecordClaimedFileUpdateInstalled(tx, fileUpdateInstallReceiptsForTest(t, tx)...); err != nil {
		t.Fatal(err)
	}

	originalHook := updateCleanupAfterRename
	injected := false
	updateCleanupAfterRename = func(oldpath, _ string) {
		if !injected && oldpath == tx.BackupPath {
			injected = true
			if err := os.WriteFile(oldpath, []byte("concurrent"), 0o700); err != nil {
				t.Errorf("recreate backup path: %v", err)
			}
		}
	}
	t.Cleanup(func() { updateCleanupAfterRename = originalHook })

	if err := MarkUpdateHealthyExact(tx.ToVersion, tx.CreatedAt, UpdateTransactionID(tx)); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(tx.BackupPath); err != nil || string(got) != "concurrent" {
		t.Fatalf("concurrent backup path = %q, %v", got, err)
	}
}

// TestFileUpdateRollbackCompensatesOnPartialFailure pins the release-unit
// contract: when restoring a later binary fails, the already-restored ones are
// renamed back so the install stays a coherent new-version unit (never mixed),
// the pending transaction survives, and a later rollback attempt succeeds.
func TestFileUpdateRollbackCompensatesOnPartialFailure(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "reasonix-desktop")
	guard := filepath.Join(dir, "reasonix-guard")
	added := filepath.Join(dir, "reasonix-update-helper.exe")
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return guard, nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })
	for path, content := range map[string]string{target: "old-desktop", guard: "old-guard"} {
		if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := PrepareFileUpdate("v1", "v2", target, added, guard); err != nil {
		t.Fatal(err)
	}
	for path, content := range map[string]string{target: "new-desktop", guard: "new-guard", added: "new-helper"} {
		if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
			t.Fatal(err)
		}
	}

	originalPublish := rollbackPublishStage
	rollbackPublishStage = func(oldpath, newpath string) error {
		if strings.HasPrefix(oldpath, guard+".reasonix-rollback-stage-") && newpath == guard {
			return errors.New("injected rename failure")
		}
		return renameRepairNodeNoReplace(oldpath, newpath)
	}
	t.Cleanup(func() { rollbackPublishStage = originalPublish })

	result, err := RollbackPendingUpdate()
	if err == nil {
		t.Fatal("rollback with injected failure should error")
	}
	if result.RolledBack {
		t.Fatalf("rollback result = %+v", result)
	}
	if result.MixedInstall {
		t.Fatalf("compensated rollback must not report a mixed install: %+v", result)
	}
	for path, want := range map[string]string{target: "new-desktop", guard: "new-guard", added: "new-helper"} {
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Fatalf("compensated %s = %q, want %q", filepath.Base(path), got, want)
		}
	}
	leftovers, err := filepath.Glob(filepath.Join(dir, "*.reasonix-rollback-*"))
	if err != nil || len(leftovers) != 0 {
		t.Fatalf("rollback left staging files behind: %v (err=%v)", leftovers, err)
	}
	if !HasPendingUpdate() {
		t.Fatal("pending update must survive a failed rollback for a retry")
	}

	rollbackPublishStage = originalPublish
	retry, err := RollbackPendingUpdate()
	if err != nil {
		t.Fatal(err)
	}
	if !retry.RolledBack {
		t.Fatalf("retry result = %+v", retry)
	}
	for path, want := range map[string]string{target: "old-desktop", guard: "old-guard"} {
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Fatalf("retried %s = %q, want %q", filepath.Base(path), got, want)
		}
	}
	if _, err := os.Stat(added); !os.IsNotExist(err) {
		t.Fatalf("new-release-only helper survived retried rollback: %v", err)
	}
}

func TestFileUpdateRollbackRejectsStageChangedDuringPublish(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "reasonix-desktop")
	guard := filepath.Join(dir, "reasonix-guard")
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return guard, nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })
	if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareFileUpdate("v1", "v2", target); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("new"), 0o700); err != nil {
		t.Fatal(err)
	}

	originalPublish := rollbackPublishStage
	rollbackPublishStage = func(stage, destination string) error {
		if err := os.WriteFile(stage, []byte("tampered"), 0o700); err != nil {
			return err
		}
		return renameRepairNodeNoReplace(stage, destination)
	}
	t.Cleanup(func() { rollbackPublishStage = originalPublish })

	result, err := RollbackPendingUpdate()
	if err == nil || result.RolledBack || result.MixedInstall {
		t.Fatalf("rollback = %+v, %v; want compensated hash rejection", result, err)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "new" {
		t.Fatalf("compensated target = %q, %v; want pre-rollback bytes", got, err)
	}
	if !HasPendingUpdate() {
		t.Fatal("failed rollback removed pending transaction")
	}
}

func TestFileUpdateRollbackPreservesAsideReplacedBeforeCleanup(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "reasonix-desktop")
	guard := filepath.Join(dir, "reasonix-guard")
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return guard, nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })
	if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareFileUpdate("v1", "v2", target); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("new"), 0o700); err != nil {
		t.Fatal(err)
	}

	originalPublish := rollbackPublishStage
	rollbackPublishStage = func(oldpath, newpath string) error {
		if err := renameRepairNodeNoReplace(oldpath, newpath); err != nil {
			return err
		}
		return os.WriteFile(newpath+".reasonix-rollback-aside", []byte("concurrent-aside"), 0o700)
	}
	t.Cleanup(func() { rollbackPublishStage = originalPublish })

	result, err := RollbackPendingUpdate()
	if err != nil || !result.RolledBack {
		t.Fatalf("rollback = %+v, %v", result, err)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "old" {
		t.Fatalf("restored target = %q, %v", got, err)
	}
	aside := target + ".reasonix-rollback-aside"
	if got, err := os.ReadFile(aside); err != nil || string(got) != "concurrent-aside" {
		t.Fatalf("concurrent aside = %q, %v", got, err)
	}
}

func TestFileUpdateRollbackCompensationPreservesRecreatedTarget(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "reasonix-desktop")
	guard := filepath.Join(dir, "reasonix-guard")
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return guard, nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })
	for path, content := range map[string]string{target: "old-desktop", guard: "old-guard"} {
		if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := PrepareFileUpdate("v1", "v2", target, guard); err != nil {
		t.Fatal(err)
	}
	for path, content := range map[string]string{target: "new-desktop", guard: "new-guard"} {
		if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
			t.Fatal(err)
		}
	}

	originalPublish := rollbackPublishStage
	rollbackPublishStage = func(oldpath, newpath string) error {
		if newpath == guard {
			if err := os.WriteFile(target, []byte("concurrent"), 0o700); err != nil {
				return err
			}
			return errors.New("injected publish failure")
		}
		return renameRepairNodeNoReplace(oldpath, newpath)
	}
	t.Cleanup(func() { rollbackPublishStage = originalPublish })

	result, err := RollbackPendingUpdate()
	if err == nil || !result.MixedInstall || result.RolledBack {
		t.Fatalf("rollback = %+v, %v", result, err)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "concurrent" {
		t.Fatalf("concurrent target = %q, %v", got, err)
	}
	aside := target + ".reasonix-rollback-aside"
	if got, err := os.ReadFile(aside); err != nil || string(got) != "new-desktop" {
		t.Fatalf("retained pre-rollback target = %q, %v", got, err)
	}
	if !HasPendingUpdate() {
		t.Fatal("failed compensation removed pending transaction")
	}
}

func TestFileUpdateRollbackRetryPreservesRetainedNewBinary(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "reasonix-desktop")
	guard := filepath.Join(dir, "reasonix-guard")
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return guard, nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })
	for path, content := range map[string]string{target: "old-desktop", guard: "old-guard"} {
		if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	tx, err := PrepareFileUpdate("v1", "v2", target, guard)
	if err != nil {
		t.Fatal(err)
	}
	for path, content := range map[string]string{target: "new-desktop", guard: "new-guard"} {
		if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
			t.Fatal(err)
		}
	}

	// Simulate a process crash after the first old binary was installed but
	// before its retained new-version aside was removed.
	targetAside := target + ".reasonix-rollback-aside"
	if err := os.Rename(target, targetAside); err != nil {
		t.Fatal(err)
	}
	if _, err := copyFileWithHash(tx.Files[0].BackupPath, target, 0o700); err != nil {
		t.Fatal(err)
	}

	originalPublish := rollbackPublishStage
	rollbackPublishStage = func(oldpath, newpath string) error {
		if strings.HasPrefix(oldpath, guard+".reasonix-rollback-stage-") && newpath == guard {
			return errors.New("injected rename failure")
		}
		return renameRepairNodeNoReplace(oldpath, newpath)
	}
	t.Cleanup(func() { rollbackPublishStage = originalPublish })

	result, err := RollbackPendingUpdate()
	if err == nil || !result.MixedInstall || result.RolledBack {
		t.Fatalf("compensated crash retry = %+v, %v", result, err)
	}
	for path, want := range map[string]string{target: "old-desktop", guard: "new-guard"} {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want {
			t.Fatalf("compensated %s = %q (%v), want %q", filepath.Base(path), got, err, want)
		}
	}
	if got, err := os.ReadFile(targetAside); err != nil || string(got) != "new-desktop" {
		t.Fatalf("untrusted inherited aside was consumed: %q, %v", got, err)
	}
	if !HasPendingUpdate() {
		t.Fatal("failed crash retry removed pending transaction")
	}

	rollbackPublishStage = originalPublish
	retry, err := RollbackPendingUpdate()
	if err != nil || !retry.RolledBack {
		t.Fatalf("retry after compensation = %+v, %v", retry, err)
	}
	for path, want := range map[string]string{target: "old-desktop", guard: "old-guard"} {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want {
			t.Fatalf("retried %s = %q (%v), want %q", filepath.Base(path), got, err, want)
		}
	}
}

func TestFileUpdateRollbackRechecksWholeReleaseUnitBeforeCommit(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "reasonix-desktop")
	guard := filepath.Join(dir, "reasonix-guard")
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return guard, nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })
	for path, body := range map[string]string{target: "old-desktop", guard: "old-guard"} {
		if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := PrepareFileUpdate("v1", "v2", target, guard); err != nil {
		t.Fatal(err)
	}
	for path, body := range map[string]string{target: "new-desktop", guard: "new-guard"} {
		if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
			t.Fatal(err)
		}
	}

	originalSwap := rollbackSwapRename
	injected := false
	rollbackSwapRename = func(oldPath, newPath string) error {
		if !injected && oldPath == guard && newPath == guard+".reasonix-rollback-aside" {
			injected = true
			if err := os.WriteFile(target, []byte("uncooperative-after-first-publish"), 0o700); err != nil {
				return err
			}
		}
		return renameRepairNodeNoReplace(oldPath, newPath)
	}
	t.Cleanup(func() { rollbackSwapRename = originalSwap })

	result, err := RollbackPendingUpdate()
	if err == nil || !strings.Contains(err.Error(), "verify restored release unit") {
		t.Fatalf("rollback error = %v, want final release-unit verification", err)
	}
	if !injected {
		t.Fatal("test did not mutate the first restored member")
	}
	if !result.MixedInstall {
		t.Fatalf("uncooperative drift was not reported as mixed: %+v", result)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "uncooperative-after-first-publish" {
		t.Fatalf("uncooperative target was overwritten: %q, %v", got, err)
	}
	if got, err := os.ReadFile(guard); err != nil || string(got) != "new-guard" {
		t.Fatalf("cooperative sibling was not compensated: %q, %v", got, err)
	}
	if got, err := os.ReadFile(target + ".reasonix-rollback-aside"); err != nil || string(got) != "new-desktop" {
		t.Fatalf("prior target recovery material was not preserved: %q, %v", got, err)
	}
	if !HasPendingUpdate() {
		t.Fatal("failed release-unit verification removed pending state")
	}
}

func TestRollbackPendingUpdateStateRechecksLiveUnitAfterStaging(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "reasonix-desktop")
	guard := filepath.Join(dir, "reasonix-guard")
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return guard, nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })
	if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	tx, err := PrepareFileUpdate("v1", "v2", target)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("confirmed-new"), 0o700); err != nil {
		t.Fatal(err)
	}
	expectedState, expectedFiles := pendingUpdateBoundPreview(tx)

	originalCopy := rollbackStageCopy
	changed := false
	rollbackStageCopy = func(src, dst string, mode os.FileMode) (string, error) {
		hash, err := copyFileWithHash(src, dst, mode)
		if err == nil && !changed {
			changed = true
			err = os.WriteFile(target, []byte("concurrent-new"), 0o700)
		}
		return hash, err
	}
	t.Cleanup(func() { rollbackStageCopy = originalCopy })

	result, err := rollbackPendingUpdateState(expectedState, expectedFiles)
	if err == nil || result.RolledBack || !strings.Contains(err.Error(), "preview changed since confirmation") {
		t.Fatalf("rollback after live drift = %+v, %v", result, err)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "concurrent-new" {
		t.Fatalf("rollback overwrote unconfirmed live target: %q, %v", got, err)
	}
	if !HasPendingUpdate() {
		t.Fatal("rejected confirmed rollback removed pending transaction")
	}
}

func TestRollbackPendingUpdateStateRejectsDriftInsideRetainRename(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "reasonix-desktop")
	guard := filepath.Join(dir, "reasonix-guard")
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return guard, nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })
	if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	tx, err := PrepareFileUpdate("v1", "v2", target)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("confirmed-new"), 0o700); err != nil {
		t.Fatal(err)
	}
	expectedState, expectedFiles := pendingUpdateBoundPreview(tx)

	originalRename := rollbackSwapRename
	injected := false
	rollbackSwapRename = func(oldpath, newpath string) error {
		if !injected && oldpath == target && newpath == target+".reasonix-rollback-aside" {
			injected = true
			if err := os.WriteFile(target, []byte("concurrent-new"), 0o700); err != nil {
				return err
			}
		}
		return os.Rename(oldpath, newpath)
	}
	t.Cleanup(func() { rollbackSwapRename = originalRename })

	result, err := rollbackPendingUpdateState(expectedState, expectedFiles)
	if err == nil || result.RolledBack || !strings.Contains(err.Error(), "preview changed since confirmation") {
		t.Fatalf("rollback after retain-window drift = %+v, %v", result, err)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "concurrent-new" {
		t.Fatalf("rollback lost concurrent live target: %q, %v", got, err)
	}
	if !HasPendingUpdate() {
		t.Fatal("rejected retain-window drift removed pending transaction")
	}
}

func TestRollbackPendingUpdateStatePreservesRecreateBeforeStagePublish(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "reasonix-desktop")
	guard := filepath.Join(dir, "reasonix-guard")
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return guard, nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })
	if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	tx, err := PrepareFileUpdate("v1", "v2", target)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("confirmed-new"), 0o700); err != nil {
		t.Fatal(err)
	}
	expectedState, expectedFiles := pendingUpdateBoundPreview(tx)

	originalPublish := rollbackPublishStage
	injected := false
	rollbackPublishStage = func(oldpath, newpath string) error {
		if !injected && newpath == target {
			injected = true
			if err := os.WriteFile(target, []byte("concurrent-new"), 0o700); err != nil {
				return err
			}
		}
		return renameRepairNodeNoReplace(oldpath, newpath)
	}
	t.Cleanup(func() { rollbackPublishStage = originalPublish })

	result, err := rollbackPendingUpdateState(expectedState, expectedFiles)
	if err == nil || result.RolledBack || !result.MixedInstall {
		t.Fatalf("rollback after pre-publish recreate = %+v, %v", result, err)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "concurrent-new" {
		t.Fatalf("rollback overwrote concurrent recreation: %q, %v", got, err)
	}
	aside := target + ".reasonix-rollback-aside"
	if got, err := os.ReadFile(aside); err != nil || string(got) != "confirmed-new" {
		t.Fatalf("rollback lost confirmed live target: %q, %v", got, err)
	}
	if !HasPendingUpdate() {
		t.Fatal("failed pre-publish recreate removed pending transaction")
	}
}

func TestRollbackPendingAppBundleStateRejectsDriftInsideRetainRename(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app := filepath.Join(dir, "Reasonix.app")
	exe := filepath.Join(app, "Contents", "MacOS", "Reasonix")
	if err := os.MkdirAll(filepath.Dir(exe), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return exe, nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })

	backup := app + ".reasonix-update-backup"
	tx, err := PrepareAppBundleUpdate("v1", "v2", app, backup)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(app, backup); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(exe), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("confirmed-new"), 0o700); err != nil {
		t.Fatal(err)
	}
	expectedState, expectedFiles := pendingUpdateBoundPreview(tx)

	originalRename := rollbackSwapRename
	injected := false
	rollbackSwapRename = func(oldpath, newpath string) error {
		if !injected && oldpath == app && strings.Contains(newpath, ".reasonix-failed-") {
			injected = true
			if err := os.WriteFile(exe, []byte("concurrent-new"), 0o700); err != nil {
				return err
			}
		}
		return os.Rename(oldpath, newpath)
	}
	t.Cleanup(func() { rollbackSwapRename = originalRename })

	result, err := rollbackPendingUpdateState(expectedState, expectedFiles)
	if err == nil || result.RolledBack || !strings.Contains(err.Error(), "preview changed since confirmation") {
		t.Fatalf("bundle rollback after retain-window drift = %+v, %v", result, err)
	}
	if got, err := os.ReadFile(exe); err != nil || string(got) != "concurrent-new" {
		t.Fatalf("bundle rollback lost concurrent live tree: %q, %v", got, err)
	}
	if _, err := os.Stat(backup); err != nil {
		t.Fatalf("rejected bundle rollback lost backup: %v", err)
	}
	if !HasPendingUpdate() {
		t.Fatal("rejected bundle retain-window drift removed pending transaction")
	}
}

func TestRollbackReportsPendingCleanupFailureAndRemainsRetryable(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "reasonix-desktop")
	guard := filepath.Join(dir, "reasonix-guard")
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return guard, nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })
	if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareFileUpdate("v1", "v2", target); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("new"), 0o700); err != nil {
		t.Fatal(err)
	}

	originalRemove := removePendingUpdateFile
	removePendingUpdateFile = func(string) error { return errors.New("injected remove failure") }
	t.Cleanup(func() { removePendingUpdateFile = originalRemove })
	result, err := RollbackPendingUpdate()
	if err == nil || !result.RolledBack || !strings.Contains(err.Error(), "clear pending transaction") {
		t.Fatalf("rollback cleanup failure = %+v, %v", result, err)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "old" {
		t.Fatalf("restored target after cleanup failure = %q, %v", got, err)
	}
	if !HasPendingUpdate() {
		t.Fatal("injected cleanup failure unexpectedly removed pending transaction")
	}

	removePendingUpdateFile = originalRemove
	retry, err := RollbackPendingUpdate()
	if err != nil || !retry.RolledBack {
		t.Fatalf("retry after cleanup failure = %+v, %v", retry, err)
	}
	if HasPendingUpdate() {
		t.Fatal("retry left pending transaction")
	}
}

// TestFileUpdateRollbackStageFailureLeavesInstallUntouched pins that a failure
// while staging (before any binary is swapped) leaves the live release unit
// exactly as it was.
func TestFileUpdateRollbackStageFailureLeavesInstallUntouched(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "reasonix-desktop")
	guard := filepath.Join(dir, "reasonix-guard")
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return guard, nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })
	for path, content := range map[string]string{target: "old-desktop", guard: "old-guard"} {
		if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := PrepareFileUpdate("v1", "v2", target, guard); err != nil {
		t.Fatal(err)
	}
	for path, content := range map[string]string{target: "new-desktop", guard: "new-guard"} {
		if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
			t.Fatal(err)
		}
	}

	originalCopy := rollbackStageCopy
	rollbackStageCopy = func(src, dst string, mode os.FileMode) (string, error) {
		if strings.HasPrefix(dst, guard+".reasonix-rollback-stage-") {
			return "", errors.New("injected copy failure")
		}
		return copyFileWithHashCreate(src, dst, mode)
	}
	t.Cleanup(func() { rollbackStageCopy = originalCopy })

	result, err := RollbackPendingUpdate()
	if err == nil {
		t.Fatal("rollback with injected stage failure should error")
	}
	if result.RolledBack || result.MixedInstall {
		t.Fatalf("rollback result = %+v", result)
	}
	for path, want := range map[string]string{target: "new-desktop", guard: "new-guard"} {
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Fatalf("%s = %q, want untouched %q", filepath.Base(path), got, want)
		}
	}
	leftovers, err := filepath.Glob(filepath.Join(dir, "*.reasonix-rollback-*"))
	if err != nil || len(leftovers) != 0 {
		t.Fatalf("stage failure left staging files behind: %v (err=%v)", leftovers, err)
	}
}

func TestPrepareFileUpdateDoesNotOverwriteLegacyBackupName(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	dir := t.TempDir()
	target := filepath.Join(dir, "reasonix-desktop")
	guard := filepath.Join(dir, "reasonix-guard")
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return guard, nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })
	if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	backupDir := filepath.Join(home, "repair", "updates")
	if err := os.MkdirAll(backupDir, 0o700); err != nil {
		t.Fatal(err)
	}
	legacyBackup := filepath.Join(backupDir, "reasonix-desktop.previous")
	if err := os.WriteFile(legacyBackup, []byte("unowned"), 0o600); err != nil {
		t.Fatal(err)
	}

	tx, err := PrepareFileUpdate("v1", "v2", target)
	if err != nil {
		t.Fatal(err)
	}
	if tx.BackupPath == legacyBackup {
		t.Fatal("new transaction reused the legacy fixed backup path")
	}
	if got, err := os.ReadFile(legacyBackup); err != nil || string(got) != "unowned" {
		t.Fatalf("legacy backup was overwritten: %q, %v", got, err)
	}
	if got, err := os.ReadFile(tx.BackupPath); err != nil || string(got) != "old" {
		t.Fatalf("transaction backup = %q, %v", got, err)
	}
}

func TestFileUpdateRollbackBypassesAndPreservesCrashedStage(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	dir := t.TempDir()
	target := filepath.Join(dir, "reasonix-desktop")
	guard := filepath.Join(dir, "reasonix-guard")
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return guard, nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })
	if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareFileUpdate("v1", "v2", target); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("new"), 0o700); err != nil {
		t.Fatal(err)
	}
	stage := target + ".reasonix-rollback-stage-crashed"
	if err := os.WriteFile(stage, []byte("unowned-stage"), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := RollbackPendingUpdate()
	if err != nil || !result.RolledBack || result.MixedInstall {
		t.Fatalf("rollback with crashed stage = %+v, %v", result, err)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "old" {
		t.Fatalf("restored target = %q, %v", got, err)
	}
	if got, err := os.ReadFile(stage); err != nil || string(got) != "unowned-stage" {
		t.Fatalf("preexisting stage was overwritten: %q, %v", got, err)
	}
	if HasPendingUpdate() {
		t.Fatal("successful rollback left pending recovery state")
	}
}

func TestRecordClaimedFileUpdateInstalledKeepsPendingPublicDuringSidecarCommit(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	dir := t.TempDir()
	target := filepath.Join(dir, "reasonix-desktop")
	guard := filepath.Join(dir, "reasonix-guard")
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return guard, nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })
	if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	prepared, err := PrepareFileUpdate("v1", "v2", target)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("new"), 0o700); err != nil {
		t.Fatal(err)
	}
	concurrent := *prepared
	concurrent.FromVersion = "concurrent"
	originalHook := installedUpdateAfterCreate
	var hookErr error
	var pendingBeforeRewrite *UpdateTransaction
	installedUpdateAfterCreate = func(string) {
		pendingBeforeRewrite, hookErr = ReadPendingUpdate()
		if hookErr == nil {
			hookErr = overwritePendingUpdateForTest(&concurrent)
		}
	}
	t.Cleanup(func() { installedUpdateAfterCreate = originalHook })

	if _, err := RecordClaimedFileUpdateInstalled(prepared, fileUpdateInstallReceiptsForTest(t, prepared)...); err == nil {
		t.Fatal("record installed update overwrote a concurrent pending rewrite")
	}
	if hookErr != nil {
		t.Fatalf("write concurrent pending transaction: %v", hookErr)
	}
	if pendingBeforeRewrite == nil || UpdateTransactionID(pendingBeforeRewrite) != UpdateTransactionID(prepared) {
		t.Fatalf("public pending transaction disappeared during sidecar commit: %+v", pendingBeforeRewrite)
	}
	current, err := ReadPendingUpdate()
	if err != nil {
		t.Fatal(err)
	}
	if current.FromVersion != "concurrent" {
		t.Fatalf("public pending transaction = %+v", current)
	}
	if _, err := os.Stat(installedFileUpdateStatePath(prepared)); err != nil {
		t.Fatalf("transaction recovery sidecar was not retained: %v", err)
	}
}

func TestMarkUpdateHealthyRechecksReleaseUnitAfterPendingDisplacement(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	dir := t.TempDir()
	target := filepath.Join(dir, "reasonix-desktop")
	guard := filepath.Join(dir, "reasonix-guard")
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return guard, nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })
	if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	prepared, err := PrepareFileUpdate("v1", "v2", target)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("new"), 0o700); err != nil {
		t.Fatal(err)
	}
	recorded, err := RecordClaimedFileUpdateInstalled(prepared, fileUpdateInstallReceiptsForTest(t, prepared)...)
	if err != nil {
		t.Fatal(err)
	}
	originalHook := updateCleanupAfterRename
	var hookErr error
	updateCleanupAfterRename = func(path, _ string) {
		if path == PendingUpdatePath() {
			hookErr = os.WriteFile(target, []byte("concurrent"), 0o700)
		}
	}
	t.Cleanup(func() { updateCleanupAfterRename = originalHook })

	if err := MarkUpdateHealthyExact(recorded.ToVersion, recorded.CreatedAt, UpdateTransactionID(recorded)); err == nil {
		t.Fatal("healthy commit accepted release-unit drift after pending displacement")
	}
	if hookErr != nil {
		t.Fatalf("write concurrent release file: %v", hookErr)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "concurrent" {
		t.Fatalf("concurrent release file = %q, %v", got, err)
	}
	current, err := ReadPendingUpdate()
	if err != nil {
		t.Fatal(err)
	}
	if UpdateTransactionID(current) != UpdateTransactionID(recorded) {
		t.Fatalf("restored pending transaction = %+v", current)
	}
	if _, err := os.Stat(recorded.BackupPath); err != nil {
		t.Fatalf("healthy rejection removed rollback backup: %v", err)
	}
}

func TestMarkUpdateHealthyFailsClosedWhenPendingDisappearsBeforeCommit(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	dir := t.TempDir()
	target := filepath.Join(dir, "reasonix-desktop")
	guard := filepath.Join(dir, "reasonix-guard")
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return guard, nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })
	if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	tx, err := PrepareFileUpdate("v1", "v2", target)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RecordClaimedFileUpdateInstalled(tx, fileUpdateInstallReceiptsForTest(t, tx)...); err != nil {
		t.Fatal(err)
	}

	originalHook := pendingUpdateBeforeCleanup
	var removeErr error
	pendingUpdateBeforeCleanup = func(path string) {
		removeErr = os.Remove(path)
	}
	t.Cleanup(func() { pendingUpdateBeforeCleanup = originalHook })

	if err := MarkUpdateHealthy("v2"); err == nil ||
		!strings.Contains(err.Error(), "disappeared before commit") {
		t.Fatalf("healthy commit after pending disappearance = %v", err)
	}
	if removeErr != nil {
		t.Fatalf("remove pending transaction: %v", removeErr)
	}
	if _, err := os.Stat(tx.BackupPath); err != nil {
		t.Fatalf("failed healthy commit removed rollback backup: %v", err)
	}
}

func TestFileUpdateRollbackRechecksReleaseUnitAfterPendingDisplacement(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	dir := t.TempDir()
	target := filepath.Join(dir, "reasonix-desktop")
	guard := filepath.Join(dir, "reasonix-guard")
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return guard, nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })
	if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareFileUpdate("v1", "v2", target); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("new"), 0o700); err != nil {
		t.Fatal(err)
	}
	originalHook := updateCleanupAfterRename
	var hookErr error
	updateCleanupAfterRename = func(path, _ string) {
		if path == PendingUpdatePath() {
			hookErr = os.WriteFile(target, []byte("concurrent"), 0o700)
		}
	}
	t.Cleanup(func() { updateCleanupAfterRename = originalHook })

	result, err := RollbackPendingUpdate()
	if err == nil || !result.RolledBack {
		t.Fatalf("rollback commit with late release drift = %+v, %v", result, err)
	}
	if hookErr != nil {
		t.Fatalf("write concurrent release file: %v", hookErr)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "concurrent" {
		t.Fatalf("concurrent release file = %q, %v", got, err)
	}
	if !HasPendingUpdate() {
		t.Fatal("late rollback drift removed pending recovery state")
	}
}

func TestRollbackPendingUpdateDirectRejectsDriftWhileWaitingForTargetLock(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	dir := t.TempDir()
	target := filepath.Join(dir, "reasonix-desktop")
	guard := filepath.Join(dir, "reasonix-guard")
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return guard, nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })
	if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareFileUpdate("v1", "v2", target); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("initial-new"), 0o700); err != nil {
		t.Fatal(err)
	}
	holder, err := lockRepairMutations(target)
	if err != nil {
		t.Fatal(err)
	}
	reachedTargetLock := make(chan struct{})
	originalHook := repairMutationBeforeLock
	targetKey := canonicalRepairPath(target)
	repairMutationBeforeLock = func(paths []string) {
		if len(paths) == 1 && paths[0] == targetKey {
			select {
			case <-reachedTargetLock:
			default:
				close(reachedTargetLock)
			}
		}
	}
	t.Cleanup(func() { repairMutationBeforeLock = originalHook })
	type rollbackResult struct {
		result UpdateRollbackResult
		err    error
	}
	done := make(chan rollbackResult, 1)
	go func() {
		result, err := RollbackPendingUpdate()
		done <- rollbackResult{result: result, err: err}
	}()
	<-reachedTargetLock
	if err := os.WriteFile(target, []byte("changed-while-waiting"), 0o700); err != nil {
		t.Fatal(err)
	}
	holder()
	got := <-done
	if got.err == nil || got.result.RolledBack ||
		!strings.Contains(got.err.Error(), "preview changed since confirmation") {
		t.Fatalf("direct rollback after drift = %+v, %v", got.result, got.err)
	}
	if body, err := os.ReadFile(target); err != nil || string(body) != "changed-while-waiting" {
		t.Fatalf("drifted live target = %q, %v", body, err)
	}
	if !HasPendingUpdate() {
		t.Fatal("rejected direct rollback removed pending transaction")
	}
}

func TestRetainUpdateRollbackNodeRetriesNameCollision(t *testing.T) {
	target := filepath.Join(t.TempDir(), "Reasonix.app")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	originalRename := rollbackSwapRename
	calls := 0
	rollbackSwapRename = func(oldPath, newPath string) error {
		calls++
		if calls == 1 {
			return os.ErrExist
		}
		return os.Rename(oldPath, newPath)
	}
	t.Cleanup(func() { rollbackSwapRename = originalRename })

	retained, err := retainUpdateRollbackNode(target, "reasonix-failed")
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || !strings.Contains(retained, ".reasonix-failed-") {
		t.Fatalf("retain calls=%d path=%q", calls, retained)
	}
	if _, err := os.Stat(retained); err != nil {
		t.Fatalf("retained app bundle: %v", err)
	}
}

func TestFileUpdateRollbackPreservesLiveNodeWithoutInstalledOwnership(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	dir := t.TempDir()
	target := filepath.Join(dir, "reasonix-desktop")
	guard := filepath.Join(dir, "reasonix-guard")
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return guard, nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })
	if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareFileUpdate("v1", "v2", target); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("unbound-live"), 0o700); err != nil {
		t.Fatal(err)
	}

	result, err := RollbackPendingUpdate()
	if err != nil || !result.RolledBack {
		t.Fatalf("rollback = %+v, %v", result, err)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "old" {
		t.Fatalf("restored target = %q, %v", got, err)
	}
	aside := target + ".reasonix-rollback-aside"
	if got, err := os.ReadFile(aside); err != nil || string(got) != "unbound-live" {
		t.Fatalf("unbound live node was not preserved: %q, %v", got, err)
	}
}

func TestPublishClaimedFileUpdateMemberUsesCompareAndPublish(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	dir := t.TempDir()
	target := filepath.Join(dir, "reasonix-desktop")
	guard := filepath.Join(dir, "reasonix-guard")
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return guard, nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })
	if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	claimed, err := PrepareFileUpdate("v1", "v2", target)
	if err != nil {
		t.Fatal(err)
	}

	if err := PublishClaimedFileUpdateMember(claimed, target, []byte("new"), 0o700); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "new" {
		t.Fatalf("published target = %q, %v", got, err)
	}
	aside := target + ".reasonix-update-aside-" + UpdateTransactionID(claimed)[:16]
	if _, err := os.Stat(aside); !os.IsNotExist(err) {
		t.Fatalf("verified prior target survived successful publish: %v", err)
	}
	current, err := ReadPendingUpdate()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(current, claimed) {
		t.Fatalf("member publish rewrote pending transaction: %+v", current)
	}
}

func TestRecordClaimedFileUpdateInstalledRejectsDriftAfterPublish(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	dir := t.TempDir()
	target := filepath.Join(dir, "reasonix-desktop")
	guard := filepath.Join(dir, "reasonix-guard")
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return guard, nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })
	if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	claimed, err := PrepareFileUpdate("v1", "v2", target)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := PublishClaimedFileUpdateMemberExact(claimed, target, []byte("new"), 0o700)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(target, []byte("concurrent"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := RecordClaimedFileUpdateInstalled(claimed, receipt); err == nil ||
		!strings.Contains(err.Error(), "release unit changed while recording") {
		t.Fatalf("record after post-publish drift = %v", err)
	}
	if _, err := os.Lstat(installedFileUpdateStatePath(claimed)); !os.IsNotExist(err) {
		t.Fatalf("post-publish drift created installed sidecar: %v", err)
	}
	if !HasPendingUpdate() {
		t.Fatal("post-publish drift removed pending recovery state")
	}
}

func TestRecordClaimedFileUpdateInstalledRequiresCompletePublishReceipts(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	dir := t.TempDir()
	target := filepath.Join(dir, "reasonix-desktop")
	guard := filepath.Join(dir, "reasonix-guard")
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return guard, nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })
	if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	claimed, err := PrepareFileUpdate("v1", "v2", target)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("new"), 0o700); err != nil {
		t.Fatal(err)
	}

	if _, err := RecordClaimedFileUpdateInstalled(claimed); err == nil ||
		!strings.Contains(err.Error(), "publish receipt is missing") {
		t.Fatalf("record without publish receipt = %v", err)
	}
	receipt := fileUpdateInstallReceiptsForTest(t, claimed)[0]
	if _, err := RecordClaimedFileUpdateInstalled(claimed, receipt, receipt); err == nil ||
		!strings.Contains(err.Error(), "duplicate publish receipt") {
		t.Fatalf("record with duplicate publish receipt = %v", err)
	}
	foreign := receipt
	foreign.UpdateTransactionID = strings.Repeat("0", 64)
	if _, err := RecordClaimedFileUpdateInstalled(claimed, foreign); err == nil ||
		!strings.Contains(err.Error(), "different transaction") {
		t.Fatalf("record with foreign publish receipt = %v", err)
	}
}

func TestPublishClaimedFileUpdateMemberPreservesConcurrentRecreate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	dir := t.TempDir()
	target := filepath.Join(dir, "reasonix-desktop")
	guard := filepath.Join(dir, "reasonix-guard")
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return guard, nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })
	if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	claimed, err := PrepareFileUpdate("v1", "v2", target)
	if err != nil {
		t.Fatal(err)
	}
	originalHook := fileUpdateAfterRetain
	var hookErr error
	fileUpdateAfterRetain = func(publicPath, _ string) {
		hookErr = os.WriteFile(publicPath, []byte("concurrent"), 0o700)
	}
	t.Cleanup(func() { fileUpdateAfterRetain = originalHook })

	if err := PublishClaimedFileUpdateMember(claimed, target, []byte("new"), 0o700); err == nil {
		t.Fatal("member publish overwrote a concurrent target recreation")
	}
	if hookErr != nil {
		t.Fatalf("write concurrent target: %v", hookErr)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "concurrent" {
		t.Fatalf("concurrent target = %q, %v", got, err)
	}
	aside := target + ".reasonix-update-aside-" + UpdateTransactionID(claimed)[:16]
	if got, err := os.ReadFile(aside); err != nil || string(got) != "old" {
		t.Fatalf("verified prior target recovery material = %q, %v", got, err)
	}
	if !HasPendingUpdate() {
		t.Fatal("failed member publish removed pending recovery state")
	}
}

func TestPublishClaimedFileUpdateMemberRestoresPreparedFileWhenStageSwapped(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	dir := t.TempDir()
	target := filepath.Join(dir, "reasonix-desktop")
	guard := filepath.Join(dir, "reasonix-guard")
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return guard, nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })
	if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	claimed, err := PrepareFileUpdate("v1", "v2", target)
	if err != nil {
		t.Fatal(err)
	}

	originalHook := fileUpdateAfterRetain
	var hookErr error
	fileUpdateAfterRetain = func(_, _ string) {
		stages, err := filepath.Glob(filepath.Join(dir, ".reasonix-desktop.reasonix-update-stage-*"))
		if err != nil {
			hookErr = err
			return
		}
		if len(stages) != 1 {
			hookErr = errors.New("unexpected staged update file count")
			return
		}
		if err := os.Remove(stages[0]); err != nil {
			hookErr = err
			return
		}
		hookErr = os.WriteFile(stages[0], []byte("tampered-stage"), 0o700)
	}
	t.Cleanup(func() { fileUpdateAfterRetain = originalHook })

	err = PublishClaimedFileUpdateMember(claimed, target, []byte("new"), 0o700)
	if err == nil || !strings.Contains(err.Error(), "installed reasonix-desktop changed") {
		t.Fatalf("stage-swapped member publish = %v", err)
	}
	if hookErr != nil {
		t.Fatalf("replace staged update: %v", hookErr)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "old" {
		t.Fatalf("restored target = %q, %v", got, err)
	}
	rejected, err := filepath.Glob(target + ".reasonix-cleanup-*")
	if err != nil {
		t.Fatal(err)
	}
	if len(rejected) != 1 {
		t.Fatalf("retained rejected files = %v", rejected)
	}
	if got, err := os.ReadFile(rejected[0]); err != nil || string(got) != "tampered-stage" {
		t.Fatalf("retained rejected file = %q, %v", got, err)
	}
	aside := target + ".reasonix-update-aside-" + UpdateTransactionID(claimed)[:16]
	if _, err := os.Lstat(aside); !os.IsNotExist(err) {
		t.Fatalf("restored prior target remained aside: %v", err)
	}
	if !HasPendingUpdate() {
		t.Fatal("stage-swapped member publish removed pending recovery state")
	}
}

func TestPublishClaimedFileUpdateMemberRejectsSameContentStageSymlink(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	dir := t.TempDir()
	target := filepath.Join(dir, "reasonix-desktop")
	guard := filepath.Join(dir, "reasonix-guard")
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return guard, nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })
	if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	claimed, err := PrepareFileUpdate("v1", "v2", target)
	if err != nil {
		t.Fatal(err)
	}
	external := filepath.Join(t.TempDir(), "external-new")
	if err := os.WriteFile(external, []byte("new"), 0o700); err != nil {
		t.Fatal(err)
	}

	originalHook := fileUpdateAfterRetain
	var hookErr error
	fileUpdateAfterRetain = func(_, _ string) {
		stages, err := filepath.Glob(filepath.Join(dir, ".reasonix-desktop.reasonix-update-stage-*"))
		if err != nil {
			hookErr = err
			return
		}
		if len(stages) != 1 {
			hookErr = errors.New("unexpected staged update file count")
			return
		}
		if err := os.Remove(stages[0]); err != nil {
			hookErr = err
			return
		}
		hookErr = os.Symlink(external, stages[0])
	}
	t.Cleanup(func() { fileUpdateAfterRetain = originalHook })

	err = PublishClaimedFileUpdateMember(claimed, target, []byte("new"), 0o700)
	if hookErr != nil {
		t.Skipf("replace staged update with symlink: %v", hookErr)
	}
	if err == nil || !strings.Contains(err.Error(), "installed reasonix-desktop changed") {
		t.Fatalf("symlink-swapped member publish = %v", err)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "old" {
		t.Fatalf("restored target = %q, %v", got, err)
	}
	rejected, err := filepath.Glob(target + ".reasonix-cleanup-*")
	if err != nil || len(rejected) != 1 {
		t.Fatalf("retained rejected symlinks = %v, %v", rejected, err)
	}
	if got, err := os.Readlink(rejected[0]); err != nil || got != external {
		t.Fatalf("retained rejected symlink = %q, %v", got, err)
	}
	if got, err := os.ReadFile(external); err != nil || string(got) != "new" {
		t.Fatalf("external file changed = %q, %v", got, err)
	}
	if !HasPendingUpdate() {
		t.Fatal("symlink-swapped member publish removed pending recovery state")
	}
}

func TestFileUpdateRollbackCleansInstalledNodeBoundToTransaction(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	dir := t.TempDir()
	target := filepath.Join(dir, "reasonix-desktop")
	guard := filepath.Join(dir, "reasonix-guard")
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return guard, nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })
	if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	prepared, err := PrepareFileUpdate("v1", "v2", target)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("installed"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := RecordClaimedFileUpdateInstalled(prepared, fileUpdateInstallReceiptsForTest(t, prepared)...); err != nil {
		t.Fatal(err)
	}

	result, err := RollbackPendingUpdate()
	if err != nil || !result.RolledBack {
		t.Fatalf("rollback = %+v, %v", result, err)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "old" {
		t.Fatalf("restored target = %q, %v", got, err)
	}
	if _, err := os.Stat(target + ".reasonix-rollback-aside"); !os.IsNotExist(err) {
		t.Fatalf("transaction-owned installed node survived cleanup: %v", err)
	}
}

func TestAppBundleRollbackPreservesLiveBundleWithoutReplacementOwnership(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app := filepath.Join(dir, "Reasonix.app")
	exe := filepath.Join(app, "Contents", "MacOS", "Reasonix")
	if err := os.MkdirAll(filepath.Dir(exe), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return exe, nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })
	backup := app + ".reasonix-update-backup"
	if _, err := PrepareAppBundleUpdate("v1", "v2", app, backup); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(app, backup); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(exe), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("unbound-live"), 0o700); err != nil {
		t.Fatal(err)
	}

	result, err := RollbackPendingUpdate()
	if err != nil || !result.RolledBack {
		t.Fatalf("rollback = %+v, %v", result, err)
	}
	if got, err := os.ReadFile(exe); err != nil || string(got) != "old" {
		t.Fatalf("restored bundle = %q, %v", got, err)
	}
	failed, err := filepath.Glob(app + ".reasonix-failed-*")
	if err != nil || len(failed) != 1 {
		t.Fatalf("retained unbound bundle = %v, %v", failed, err)
	}
	if got, err := os.ReadFile(filepath.Join(failed[0], "Contents", "MacOS", "Reasonix")); err != nil || string(got) != "unbound-live" {
		t.Fatalf("unbound bundle was not preserved: %q, %v", got, err)
	}
}

func TestAppBundleRollbackCleansReplacementBoundToStagingTree(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app := filepath.Join(dir, "Reasonix.app")
	exe := filepath.Join(app, "Contents", "MacOS", "Reasonix")
	if err := os.MkdirAll(filepath.Dir(exe), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return exe, nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })
	staging, err := os.MkdirTemp("", "reasonix-mac-update-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(staging) })
	stagedApp := filepath.Join(staging, "Reasonix.app")
	stagedExe := filepath.Join(stagedApp, "Contents", "MacOS", "Reasonix")
	if err := os.MkdirAll(filepath.Dir(stagedExe), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stagedExe, []byte("installed"), 0o700); err != nil {
		t.Fatal(err)
	}
	backup := app + ".reasonix-update-backup"
	if _, err := PrepareAppBundleUpdateHandoff("v1", "v2", app, backup, stagedApp, staging, os.Getpid()); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(app, backup); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(exe), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("installed"), 0o700); err != nil {
		t.Fatal(err)
	}

	result, err := RollbackPendingUpdate()
	if err != nil || !result.RolledBack {
		t.Fatalf("rollback = %+v, %v", result, err)
	}
	if got, err := os.ReadFile(exe); err != nil || string(got) != "old" {
		t.Fatalf("restored bundle = %q, %v", got, err)
	}
	failed, err := filepath.Glob(app + ".reasonix-failed-*")
	if err != nil || len(failed) != 0 {
		t.Fatalf("transaction-owned replacement survived cleanup: %v, %v", failed, err)
	}
}
