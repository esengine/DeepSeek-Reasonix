package repair

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
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

func TestReconcilePendingUpdateCancelsAbandonedSameVersionAppHandoff(t *testing.T) {
	tx, staging := prepareTestAppBundleHandoff(t)

	result, err := ReconcilePendingUpdate(tx.ToVersion)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Pending || !result.Cleared || result.RolledBack || result.AwaitingHealth {
		t.Fatalf("reconcile result = %+v", result)
	}
	if _, err := os.Stat(PendingUpdatePath()); !os.IsNotExist(err) {
		t.Fatalf("pending transaction survived reconcile: %v", err)
	}
	if _, err := os.Stat(staging); !os.IsNotExist(err) {
		t.Fatalf("verified handoff staging survived reconcile: %v", err)
	}
	if err := VerifyAppBundleUpdateHandoffOriginal(tx); err != nil {
		t.Fatalf("original bundle changed during reconcile: %v", err)
	}
}

func TestReconcilePendingUpdateLeavesProbationaryTarget(t *testing.T) {
	tx, staging := prepareTestAppBundleHandoff(t)
	if err := os.Rename(tx.TargetPath, tx.BackupPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tx.HandoffAppPath, tx.TargetPath); err != nil {
		t.Fatal(err)
	}

	result, err := ReconcilePendingUpdate(tx.ToVersion)
	if !errors.Is(err, ErrPendingUpdateAwaitingHealth) {
		t.Fatalf("reconcile error = %v", err)
	}
	if !result.Pending || !result.AwaitingHealth || result.Cleared || result.RolledBack {
		t.Fatalf("reconcile result = %+v", result)
	}
	if _, err := ReadPendingUpdate(); err != nil {
		t.Fatalf("probationary transaction was removed: %v", err)
	}
	if _, err := os.Stat(staging); err != nil {
		t.Fatalf("probationary staging was removed: %v", err)
	}
}

func TestReconcilePendingUpdateRollsBackPublishedAppHandoff(t *testing.T) {
	tx, _ := prepareTestAppBundleHandoff(t)
	if err := os.Rename(tx.TargetPath, tx.BackupPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tx.HandoffAppPath, tx.TargetPath); err != nil {
		t.Fatal(err)
	}

	result, err := ReconcilePendingUpdate(tx.FromVersion)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Pending || !result.RolledBack || result.Cleared || result.AwaitingHealth {
		t.Fatalf("reconcile result = %+v", result)
	}
	if err := VerifyAppBundleUpdateHandoffOriginal(tx); err != nil {
		t.Fatalf("previous app bundle was not restored: %v", err)
	}
	if _, err := os.Stat(PendingUpdatePath()); !os.IsNotExist(err) {
		t.Fatalf("pending transaction survived rollback: %v", err)
	}
}

func TestReconcilePendingUpdateRejectsTransactionRewrittenBeforeCancelLock(t *testing.T) {
	tx, _ := prepareTestAppBundleHandoff(t)
	attempted := make(chan struct{})
	allow := make(chan struct{})
	originalAcquire := acquirePendingUpdateLock
	var once sync.Once
	acquirePendingUpdateLock = func() (func(), error) {
		blocked := false
		once.Do(func() {
			blocked = true
			close(attempted)
		})
		if blocked {
			<-allow
		}
		return originalAcquire()
	}
	t.Cleanup(func() { acquirePendingUpdateLock = originalAcquire })

	type outcome struct {
		result PendingUpdateReconcileResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := ReconcilePendingUpdate(tx.FromVersion)
		done <- outcome{result: result, err: err}
	}()
	<-attempted
	changed := *tx
	changed.ToVersion = "v3"
	if err := overwritePendingUpdateForTest(&changed); err != nil {
		t.Fatal(err)
	}
	close(allow)

	got := <-done
	if got.err == nil || !strings.Contains(got.err.Error(), "changed") {
		t.Fatalf("reconcile outcome = %+v, %v", got.result, got.err)
	}
	current, err := ReadPendingUpdate()
	if err != nil || current.FromVersion != changed.FromVersion {
		t.Fatalf("rewritten pending update = %+v, %v", current, err)
	}
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

type existingAppBundleBackupFixture struct {
	app       string
	backup    string
	stagedApp string
	staging   string
}

func newExistingAppBundleBackupFixture(t *testing.T) existingAppBundleBackupFixture {
	t.Helper()
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
	if err := os.WriteFile(filepath.Join(backup, "marker"), []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return exe, nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })
	return existingAppBundleBackupFixture{
		app:       app,
		backup:    backup,
		stagedApp: stagedApp,
		staging:   staging,
	}
}

func TestPrepareAppBundleUpdateHandoffQuarantinesExistingBackup(t *testing.T) {
	fixture := newExistingAppBundleBackupFixture(t)

	tx, err := PrepareAppBundleUpdateHandoff(
		"v1", "v2", fixture.app, fixture.backup, fixture.stagedApp, fixture.staging, os.Getpid(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(fixture.backup); !os.IsNotExist(err) {
		t.Fatalf("existing backup still blocks prepare: %v", err)
	}
	quarantines, err := filepath.Glob(fixture.backup + ".reasonix-orphaned-*")
	if err != nil || len(quarantines) != 1 {
		t.Fatalf("quarantined backups = %v, %v", quarantines, err)
	}
	if got, err := os.ReadFile(filepath.Join(quarantines[0], "marker")); err != nil || string(got) != "preserve" {
		t.Fatalf("quarantined backup marker = %q, %v", got, err)
	}
	if tx.OrphanedBackupPath != quarantines[0] || strings.TrimSpace(tx.OrphanedBackupTreeID) == "" {
		t.Fatalf("quarantine ownership = %q %q, want recorded path and digest", tx.OrphanedBackupPath, tx.OrphanedBackupTreeID)
	}
	current, err := ReadPendingUpdate()
	if err != nil || UpdateTransactionID(current) != UpdateTransactionID(tx) {
		t.Fatalf("pending transaction = %+v, %v", current, err)
	}
}

func TestCancelAppBundleUpdateHandoffCleansOwnedQuarantine(t *testing.T) {
	fixture := newExistingAppBundleBackupFixture(t)
	tx, err := PrepareAppBundleUpdateHandoff(
		"v1", "v2", fixture.app, fixture.backup, fixture.stagedApp, fixture.staging, os.Getpid(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CancelPendingAppBundleUpdateHandoffExact(tx, time.Second); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(tx.OrphanedBackupPath); !os.IsNotExist(err) {
		t.Fatalf("terminal transaction retained owned quarantine: %v", err)
	}
	if matches, _ := filepath.Glob(tx.OrphanedBackupPath + ".reasonix-cleanup-*"); len(matches) != 0 {
		t.Fatalf("terminal cleanup retained temporary paths: %v", matches)
	}
	if _, err := os.Lstat(PendingUpdatePath()); !os.IsNotExist(err) {
		t.Fatalf("cancelled transaction remains pending: %v", err)
	}
}

func TestCancelAppBundleUpdateHandoffPreservesChangedQuarantine(t *testing.T) {
	fixture := newExistingAppBundleBackupFixture(t)
	tx, err := PrepareAppBundleUpdateHandoff(
		"v1", "v2", fixture.app, fixture.backup, fixture.stagedApp, fixture.staging, os.Getpid(),
	)
	if err != nil {
		t.Fatal(err)
	}
	changed := filepath.Join(tx.OrphanedBackupPath, "changed-after-prepare")
	if err := os.WriteFile(changed, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := CancelPendingAppBundleUpdateHandoffExact(tx, time.Second); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(changed); err != nil || string(got) != "keep" {
		t.Fatalf("changed quarantine = %q, %v; want preserved", got, err)
	}
	if _, err := os.Lstat(PendingUpdatePath()); !os.IsNotExist(err) {
		t.Fatalf("cancelled transaction remains pending: %v", err)
	}
}

func TestCancelAppBundleUpdateHandoffPreservesConcurrentQuarantineRecreate(t *testing.T) {
	fixture := newExistingAppBundleBackupFixture(t)
	tx, err := PrepareAppBundleUpdateHandoff(
		"v1", "v2", fixture.app, fixture.backup, fixture.stagedApp, fixture.staging, os.Getpid(),
	)
	if err != nil {
		t.Fatal(err)
	}
	originalHook := updateCleanupAfterRename
	updateCleanupAfterRename = func(original, _ string) {
		if original != tx.OrphanedBackupPath {
			return
		}
		if err := os.Mkdir(original, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(original, "concurrent"), []byte("keep"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { updateCleanupAfterRename = originalHook })

	if _, err := CancelPendingAppBundleUpdateHandoffExact(tx, time.Second); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(tx.OrphanedBackupPath, "concurrent")); err != nil || string(got) != "keep" {
		t.Fatalf("concurrently recreated quarantine = %q, %v; want preserved", got, err)
	}
	if _, err := os.Lstat(PendingUpdatePath()); !os.IsNotExist(err) {
		t.Fatalf("cancelled transaction remains pending: %v", err)
	}
}

func TestAppBundleUpdateRejectsForgedOrphanedBackupMetadata(t *testing.T) {
	tx, _ := prepareTestAppBundleHandoff(t)
	tx.OrphanedBackupPath = filepath.Join(filepath.Dir(tx.TargetPath), "unrelated.reasonix-orphaned-1-0")
	tx.OrphanedBackupTreeID = strings.Repeat("0", 64)
	if err := validateUpdateTransaction(tx); err == nil || !strings.Contains(err.Error(), "unexpected name") {
		t.Fatalf("validation error = %v, want forged quarantine rejection", err)
	}
}

func TestAppBundleUpdateWithoutOrphanKeepsLegacyTransactionShape(t *testing.T) {
	prepareTestAppBundleHandoff(t)
	body, err := os.ReadFile(PendingUpdatePath())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "orphanedBackup") {
		t.Fatalf("ordinary transaction unexpectedly gained orphan metadata: %s", body)
	}
	if _, err := ReadPendingUpdate(); err != nil {
		t.Fatalf("ordinary transaction is no longer readable: %v", err)
	}
}

func TestPrepareAppBundleUpdateHandoffPreservesConcurrentBackupRecreate(t *testing.T) {
	fixture := newExistingAppBundleBackupFixture(t)
	originalHook := updateBackupAfterQuarantine
	updateBackupAfterQuarantine = func(original, _ string) {
		if original != fixture.backup {
			return
		}
		if err := os.Mkdir(original, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(original, "concurrent"), []byte("keep"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { updateBackupAfterQuarantine = originalHook })

	_, err := PrepareAppBundleUpdateHandoff(
		"v1", "v2", fixture.app, fixture.backup, fixture.stagedApp, fixture.staging, os.Getpid(),
	)
	if err == nil || !strings.Contains(err.Error(), "recreated") {
		t.Fatalf("prepare error = %v, want recreated-backup rejection", err)
	}
	if got, err := os.ReadFile(filepath.Join(fixture.backup, "concurrent")); err != nil || string(got) != "keep" {
		t.Fatalf("concurrent backup = %q, %v", got, err)
	}
	quarantines, globErr := filepath.Glob(fixture.backup + ".reasonix-orphaned-*")
	if globErr != nil || len(quarantines) != 1 {
		t.Fatalf("preserved quarantines = %v, %v", quarantines, globErr)
	}
	if _, err := os.Stat(PendingUpdatePath()); !os.IsNotExist(err) {
		t.Fatalf("failed prepare wrote pending transaction: %v", err)
	}
}

func TestPrepareAppBundleUpdateHandoffRestoresBackupChangedDuringQuarantine(t *testing.T) {
	fixture := newExistingAppBundleBackupFixture(t)
	originalHook := updateBackupAfterQuarantine
	updateBackupAfterQuarantine = func(original, quarantine string) {
		if original != fixture.backup {
			return
		}
		if err := os.WriteFile(filepath.Join(quarantine, "changed"), []byte("keep"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { updateBackupAfterQuarantine = originalHook })

	_, err := PrepareAppBundleUpdateHandoff(
		"v1", "v2", fixture.app, fixture.backup, fixture.stagedApp, fixture.staging, os.Getpid(),
	)
	if err == nil || !strings.Contains(err.Error(), "changed during quarantine") {
		t.Fatalf("prepare error = %v, want changed-backup rejection", err)
	}
	if got, err := os.ReadFile(filepath.Join(fixture.backup, "changed")); err != nil || string(got) != "keep" {
		t.Fatalf("changed backup was not restored = %q, %v", got, err)
	}
	if matches, _ := filepath.Glob(fixture.backup + ".reasonix-orphaned-*"); len(matches) != 0 {
		t.Fatalf("restored backup left a quarantine: %v", matches)
	}
	if _, err := os.Stat(PendingUpdatePath()); !os.IsNotExist(err) {
		t.Fatalf("failed prepare wrote pending transaction: %v", err)
	}
}

func TestPrepareAppBundleUpdateHandoffRejectsUnboundExistingBackup(t *testing.T) {
	fixture := newExistingAppBundleBackupFixture(t)
	outside := filepath.Join(t.TempDir(), "Reasonix")
	if err := os.WriteFile(outside, []byte("outside"), 0o700); err != nil {
		t.Fatal(err)
	}
	repairExecutable = func() (string, error) { return outside, nil }

	_, err := PrepareAppBundleUpdateHandoff(
		"v1", "v2", fixture.app, fixture.backup, fixture.stagedApp, fixture.staging, os.Getpid(),
	)
	if err == nil || !strings.Contains(err.Error(), "outside the current Reasonix installation") {
		t.Fatalf("prepare error = %v, want current-installation rejection", err)
	}
	if got, err := os.ReadFile(filepath.Join(fixture.backup, "marker")); err != nil || string(got) != "preserve" {
		t.Fatalf("unbound backup changed = %q, %v", got, err)
	}
	if matches, _ := filepath.Glob(fixture.backup + ".reasonix-orphaned-*"); len(matches) != 0 {
		t.Fatalf("unbound backup was quarantined: %v", matches)
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
