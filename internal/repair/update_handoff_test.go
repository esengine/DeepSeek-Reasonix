package repair

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func prepareTestAppBundleHandoff(t *testing.T) (*UpdateTransaction, string) {
	t.Helper()
	t.Setenv("REASONIX_HOME", t.TempDir())

	installRoot, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app := filepath.Join(installRoot, "Reasonix.app")
	exe := filepath.Join(app, "Contents", "MacOS", "Reasonix")
	if err := os.MkdirAll(filepath.Dir(exe), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("current"), 0o700); err != nil {
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
	if err := os.MkdirAll(stagedApp, 0o700); err != nil {
		t.Fatal(err)
	}
	tx, err := PrepareAppBundleUpdateHandoff(
		"v1",
		"v2",
		app,
		app+".reasonix-update-backup",
		stagedApp,
		staging,
		os.Getpid(),
	)
	if err != nil {
		t.Fatal(err)
	}
	return tx, staging
}

func TestClaimPendingAppBundleUpdateHandoffReturnsRecordedPaths(t *testing.T) {
	tx, _ := prepareTestAppBundleHandoff(t)
	var lockedPaths []string
	originalBeforeLock := repairMutationBeforeLock
	repairMutationBeforeLock = func(paths []string) {
		lockedPaths = append([]string(nil), paths...)
	}
	t.Cleanup(func() { repairMutationBeforeLock = originalBeforeLock })

	claimed, release, err := ClaimPendingAppBundleUpdateHandoff(tx.ToVersion, tx.CreatedAt, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if claimed.TargetPath != tx.TargetPath ||
		claimed.BackupPath != tx.BackupPath ||
		claimed.HandoffAppPath != tx.HandoffAppPath ||
		claimed.HandoffStagingPath != tx.HandoffStagingPath ||
		claimed.HandoffOwnerPID != tx.HandoffOwnerPID {
		release()
		t.Fatalf("claim returned different paths: %#v", claimed)
	}
	if len(lockedPaths) != 2 {
		release()
		t.Fatalf("claim locked %d paths, want target and backup: %v", len(lockedPaths), lockedPaths)
	}
	if err := ClearClaimedAppBundleUpdateHandoff(claimed); err != nil {
		release()
		t.Fatal(err)
	}
	release()
	if _, err := os.Stat(PendingUpdatePath()); !os.IsNotExist(err) {
		t.Fatalf("pending transaction still exists: %v", err)
	}
}

func TestClaimPendingAppBundleUpdateHandoffExactRejectsRewrittenTransaction(t *testing.T) {
	tx, _ := prepareTestAppBundleHandoff(t)
	changed := *tx
	changed.FromVersion = "rewritten"
	if err := overwritePendingUpdateForTest(&changed); err != nil {
		t.Fatal(err)
	}

	_, release, err := ClaimPendingAppBundleUpdateHandoffExact(
		tx.ToVersion,
		tx.CreatedAt,
		UpdateTransactionID(tx),
		time.Second,
	)
	if release != nil {
		release()
	}
	if err == nil || !strings.Contains(err.Error(), "pending transaction changed") {
		t.Fatalf("claim error = %v, want full transaction rejection", err)
	}
	current, readErr := ReadPendingUpdate()
	if readErr != nil || current.FromVersion != changed.FromVersion {
		t.Fatalf("rewritten transaction = %+v, %v", current, readErr)
	}
}

func TestPrepareAppBundleUpdateHandoffRejectsExistingBackup(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	installRoot, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app := filepath.Join(installRoot, "Reasonix.app")
	exe := filepath.Join(app, "Contents", "MacOS", "Reasonix")
	backup := app + ".reasonix-update-backup"
	staging, err := os.MkdirTemp("", "reasonix-mac-update-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(staging) })
	stagedApp := filepath.Join(staging, "Reasonix.app")
	for _, dir := range []string{filepath.Dir(exe), backup, stagedApp} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(exe, []byte("current"), 0o700); err != nil {
		t.Fatal(err)
	}
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return exe, nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })

	if _, err := PrepareAppBundleUpdateHandoff(
		"v1", "v2", app, backup, stagedApp, staging, os.Getpid(),
	); err == nil || !strings.Contains(err.Error(), "backup path already exists") {
		t.Fatalf("prepare error = %v, want existing-backup rejection", err)
	}
	if _, err := os.Stat(PendingUpdatePath()); !os.IsNotExist(err) {
		t.Fatalf("rejected prepare wrote pending transaction: %v", err)
	}
}

func TestClaimPendingAppBundleUpdateHandoffRejectsBackupAppearingAfterPrepare(t *testing.T) {
	tx, _ := prepareTestAppBundleHandoff(t)
	if err := os.MkdirAll(tx.BackupPath, 0o700); err != nil {
		t.Fatal(err)
	}
	_, release, err := ClaimPendingAppBundleUpdateHandoff(tx.ToVersion, tx.CreatedAt, time.Second)
	if release != nil {
		release()
	}
	if err == nil || !strings.Contains(err.Error(), "backup path already exists") {
		t.Fatalf("claim error = %v, want appearing-backup rejection", err)
	}
	if _, err := ReadPendingUpdate(); err != nil {
		t.Fatalf("rejected claim removed pending transaction: %v", err)
	}
	if _, err := CancelPendingAppBundleUpdateHandoff(tx.ToVersion, tx.CreatedAt, time.Second); err == nil ||
		!strings.Contains(err.Error(), "backup path already exists") {
		t.Fatalf("cancel error = %v, want appearing-backup rejection", err)
	}
	if _, err := os.Stat(tx.BackupPath); err != nil {
		t.Fatalf("rejected cancel removed appearing backup: %v", err)
	}
	if _, err := ReadPendingUpdate(); err != nil {
		t.Fatalf("rejected cancel removed pending transaction: %v", err)
	}
}

func TestClearClaimedAppBundleUpdateHandoffRejectsOriginalDrift(t *testing.T) {
	tx, _ := prepareTestAppBundleHandoff(t)
	claimed, release, err := ClaimPendingAppBundleUpdateHandoff(tx.ToVersion, tx.CreatedAt, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if err := os.WriteFile(filepath.Join(tx.TargetPath, "changed-after-claim"), []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ClearClaimedAppBundleUpdateHandoff(claimed); err == nil ||
		!strings.Contains(err.Error(), "installed bundle changed after prepare") {
		t.Fatalf("clear error = %v, want original drift rejection", err)
	}
	if _, err := ReadPendingUpdate(); err != nil {
		t.Fatalf("unsafe clear removed pending handoff: %v", err)
	}
}

func TestClaimPendingAppBundleUpdateHandoffRejectsUnboundTarget(t *testing.T) {
	tx, staging := prepareTestAppBundleHandoff(t)
	arbitrary := filepath.Join(t.TempDir(), "Unrelated.app")
	if err := os.MkdirAll(arbitrary, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(arbitrary, "marker")
	if err := os.WriteFile(marker, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	tx.TargetPath = arbitrary
	tx.BackupPath = arbitrary + ".reasonix-update-backup"
	tx.HandoffAppPath = filepath.Join(staging, "Other.app")
	if err := overwritePendingUpdateForTest(tx); err != nil {
		t.Fatal(err)
	}

	if _, release, err := ClaimPendingAppBundleUpdateHandoff(tx.ToVersion, tx.CreatedAt, time.Second); err == nil {
		release()
		t.Fatal("unbound app target was claimed")
	}
	if got, err := os.ReadFile(marker); err != nil || string(got) != "keep" {
		t.Fatalf("unbound target changed: %q, %v", got, err)
	}
}

func TestClaimPendingAppBundleUpdateHandoffRejectsLegacyTransaction(t *testing.T) {
	tx, _ := prepareTestAppBundleHandoff(t)
	tx.HandoffAppPath = ""
	tx.HandoffStagingPath = ""
	tx.HandoffAppTreeID = ""
	tx.HandoffStagingTreeID = ""
	tx.HandoffOwnerPID = 0
	if err := overwritePendingUpdateForTest(tx); err != nil {
		t.Fatal(err)
	}

	_, release, err := ClaimPendingAppBundleUpdateHandoff(tx.ToVersion, tx.CreatedAt, time.Second)
	if release != nil {
		release()
	}
	if err == nil || !strings.Contains(err.Error(), "handoff metadata is missing") {
		t.Fatalf("claim error = %v, want legacy transaction rejection", err)
	}
	if _, err := ReadPendingUpdate(); err != nil {
		t.Fatalf("legacy transaction should remain readable: %v", err)
	}
}

func TestClaimPendingAppBundleUpdateHandoffRejectsMissingStagingDigest(t *testing.T) {
	tx, _ := prepareTestAppBundleHandoff(t)
	tx.HandoffStagingTreeID = ""
	if err := overwritePendingUpdateForTest(tx); err != nil {
		t.Fatal(err)
	}

	_, release, err := ClaimPendingAppBundleUpdateHandoff(
		tx.ToVersion,
		tx.CreatedAt,
		time.Second,
	)
	if release != nil {
		release()
	}
	if err == nil || !strings.Contains(err.Error(), "handoff staging digest is missing") {
		t.Fatalf("claim error = %v, want missing staging digest rejection", err)
	}
	if _, err := ReadPendingUpdate(); err != nil {
		t.Fatalf("rejected transaction should remain readable: %v", err)
	}
}

func TestClaimPendingAppBundleUpdateHandoffRejectsReplacementWhileLocking(t *testing.T) {
	tx, staging := prepareTestAppBundleHandoff(t)
	replacement := *tx
	replacement.HandoffAppPath = filepath.Join(staging, "Replacement.app")

	originalBeforeLock := repairMutationBeforeLock
	changed := false
	repairMutationBeforeLock = func([]string) {
		if changed {
			return
		}
		changed = true
		if err := overwritePendingUpdateForTest(&replacement); err != nil {
			t.Errorf("replace pending transaction: %v", err)
		}
	}
	t.Cleanup(func() { repairMutationBeforeLock = originalBeforeLock })

	_, release, err := ClaimPendingAppBundleUpdateHandoff(tx.ToVersion, tx.CreatedAt, time.Second)
	if release != nil {
		release()
	}
	if err == nil || !strings.Contains(err.Error(), "changed while waiting") {
		t.Fatalf("claim error = %v, want replacement rejection", err)
	}
}

func TestClaimPendingAppBundleUpdateHandoffRejectsStagedTreeDrift(t *testing.T) {
	tx, staging := prepareTestAppBundleHandoff(t)
	if err := os.WriteFile(filepath.Join(tx.HandoffAppPath, "changed-after-prepare"), []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, release, err := ClaimPendingAppBundleUpdateHandoff(tx.ToVersion, tx.CreatedAt, time.Second)
	if release != nil {
		release()
	}
	if err == nil || !strings.Contains(err.Error(), "staged bundle changed") {
		t.Fatalf("claim error = %v, want staged tree drift rejection", err)
	}
	if _, err := os.Stat(staging); err != nil {
		t.Fatalf("staging was removed after rejected claim: %v", err)
	}
}

func TestClaimPendingAppBundleUpdateHandoffRejectsStagingRootDrift(t *testing.T) {
	tx, staging := prepareTestAppBundleHandoff(t)
	if err := os.WriteFile(filepath.Join(staging, "unexpected"), []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, release, err := ClaimPendingAppBundleUpdateHandoffExact(
		tx.ToVersion,
		tx.CreatedAt,
		UpdateTransactionID(tx),
		time.Second,
	)
	if release != nil {
		release()
	}
	if err == nil || !strings.Contains(err.Error(), "staging directory changed") {
		t.Fatalf("claim error = %v, want staging-root drift rejection", err)
	}
	if _, err := os.Stat(staging); err != nil {
		t.Fatalf("staging was removed after rejected claim: %v", err)
	}
}

func TestCleanupAppBundleUpdateHandoffStagingPreservesConcurrentRecreate(t *testing.T) {
	tx, staging := prepareTestAppBundleHandoff(t)
	originalHook := updateCleanupAfterRename
	updateCleanupAfterRename = func(oldpath, _ string) {
		if oldpath == staging {
			if err := os.MkdirAll(staging, 0o700); err != nil {
				t.Errorf("recreate staging: %v", err)
				return
			}
			if err := os.WriteFile(filepath.Join(staging, "concurrent"), []byte("keep"), 0o600); err != nil {
				t.Errorf("write concurrent staging: %v", err)
			}
		}
	}
	t.Cleanup(func() { updateCleanupAfterRename = originalHook })

	if err := CleanupAppBundleUpdateHandoffStaging(tx); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(staging, "concurrent")); err != nil || string(got) != "keep" {
		t.Fatalf("concurrent staging = %q, %v", got, err)
	}
}

func TestCleanupAppBundleUpdateHandoffStagingRejectsDrift(t *testing.T) {
	tx, staging := prepareTestAppBundleHandoff(t)
	if err := os.WriteFile(filepath.Join(staging, "unexpected"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := CleanupAppBundleUpdateHandoffStaging(tx); err == nil ||
		!strings.Contains(err.Error(), "staging directory changed") {
		t.Fatalf("cleanup error = %v, want staging drift rejection", err)
	}
	if got, err := os.ReadFile(filepath.Join(staging, "unexpected")); err != nil || string(got) != "keep" {
		t.Fatalf("drifted staging = %q, %v", got, err)
	}
}

func TestClaimPendingAppBundleUpdateHandoffRejectsOriginalTreeDrift(t *testing.T) {
	tx, _ := prepareTestAppBundleHandoff(t)
	if err := os.WriteFile(filepath.Join(tx.TargetPath, "changed-after-prepare"), []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, release, err := ClaimPendingAppBundleUpdateHandoff(tx.ToVersion, tx.CreatedAt, time.Second)
	if release != nil {
		release()
	}
	if err == nil || !strings.Contains(err.Error(), "installed bundle changed after prepare") {
		t.Fatalf("claim error = %v, want original tree drift rejection", err)
	}
}

func TestCancelPendingAppBundleUpdateHandoffAfterSourceDrift(t *testing.T) {
	tx, staging := prepareTestAppBundleHandoff(t)
	if err := os.WriteFile(filepath.Join(tx.HandoffAppPath, "changed-after-prepare"), []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, release, err := ClaimPendingAppBundleUpdateHandoff(tx.ToVersion, tx.CreatedAt, time.Second); err == nil {
		if release != nil {
			release()
		}
		t.Fatal("claim accepted staged source drift")
	}
	cancelled, err := CancelPendingAppBundleUpdateHandoff(tx.ToVersion, tx.CreatedAt, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.CreatedAt != tx.CreatedAt {
		t.Fatalf("cancelled transaction = %+v, want createdAt %q", cancelled, tx.CreatedAt)
	}
	if _, err := ReadPendingUpdate(); !os.IsNotExist(err) {
		t.Fatalf("pending handoff survived safe cancellation: %v", err)
	}
	if _, err := os.Stat(staging); err != nil {
		t.Fatalf("repair cancellation removed caller-owned staging: %v", err)
	}
}

func TestCancelPendingAppBundleUpdateHandoffPreservesDriftedOriginal(t *testing.T) {
	tx, _ := prepareTestAppBundleHandoff(t)
	if err := os.WriteFile(filepath.Join(tx.TargetPath, "changed-after-prepare"), []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := CancelPendingAppBundleUpdateHandoff(tx.ToVersion, tx.CreatedAt, time.Second); err == nil ||
		!strings.Contains(err.Error(), "installed bundle changed after prepare") {
		t.Fatalf("cancel error = %v, want original drift rejection", err)
	}
	if _, err := ReadPendingUpdate(); err != nil {
		t.Fatalf("pending handoff was lost after unsafe cancellation: %v", err)
	}
}

func TestCancelPendingAppBundleUpdateHandoffExactRejectsRewrittenTransaction(t *testing.T) {
	tx, _ := prepareTestAppBundleHandoff(t)
	changed := *tx
	changed.HandoffOwnerPID++
	if err := overwritePendingUpdateForTest(&changed); err != nil {
		t.Fatal(err)
	}

	if _, err := CancelPendingAppBundleUpdateHandoffExact(tx, time.Second); err == nil ||
		!strings.Contains(err.Error(), "transaction changed") {
		t.Fatalf("exact cancel error = %v, want full transaction rejection", err)
	}
	current, err := ReadPendingUpdate()
	if err != nil || current.HandoffOwnerPID != changed.HandoffOwnerPID {
		t.Fatalf("rewritten handoff transaction = %+v, %v", current, err)
	}
}

func TestClaimPendingAppBundleUpdateHandoffRejectsStagingSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks requires elevated privileges on Windows CI")
	}
	tx, _ := prepareTestAppBundleHandoff(t)
	outside := filepath.Join(t.TempDir(), "Outside.app")
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(tx.HandoffAppPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, tx.HandoffAppPath); err != nil {
		t.Fatal(err)
	}
	_, release, err := ClaimPendingAppBundleUpdateHandoff(tx.ToVersion, tx.CreatedAt, time.Second)
	if release != nil {
		release()
	}
	if err == nil || !strings.Contains(err.Error(), "resolves outside its staging directory") {
		t.Fatalf("claim error = %v, want staging containment rejection", err)
	}
}
