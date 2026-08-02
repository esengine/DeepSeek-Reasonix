package repair

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func reconcileFileUpdateFixture(t *testing.T, fromVersion, toVersion string) (*UpdateTransaction, string) {
	t.Helper()
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
	tx, err := PrepareFileUpdate(fromVersion, toVersion, target)
	if err != nil {
		t.Fatal(err)
	}
	return tx, target
}

func publishFileUpdateForTest(t *testing.T, tx *UpdateTransaction, content string) {
	t.Helper()
	targetPaths := make([]string, 0, len(tx.Files))
	for _, file := range tx.Files {
		targetPaths = append(targetPaths, file.TargetPath)
	}
	claimed, release, err := ClaimPendingFileUpdateExact(
		tx.ToVersion,
		tx.CreatedAt,
		UpdateTransactionID(tx),
		filepath.Join(filepath.Dir(tx.TargetPath), "reasonix-launcher"),
		targetPaths,
		time.Second*10,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	receipts := make([]FileUpdateInstallReceipt, 0, len(tx.Files))
	for _, file := range tx.Files {
		receipt, err := PublishClaimedFileUpdateMemberExact(claimed, file.TargetPath, []byte(content), 0o700)
		if err != nil {
			t.Fatal(err)
		}
		receipts = append(receipts, receipt)
	}
	if _, err := RecordClaimedFileUpdateInstalled(tx, receipts...); err != nil {
		t.Fatal(err)
	}
}

func TestReconcilePreparedCancelsStaleTransaction(t *testing.T) {
	tx, target := reconcileFileUpdateFixture(t, "v1", "v2")
	_ = target

	view, err := InspectPendingUpdate("v1")
	if err != nil {
		t.Fatal(err)
	}
	if view.State != UpdateRecoveryPrepared {
		t.Fatalf("state = %s, want prepared", view.State)
	}

	result, err := ReconcilePendingUpdate("v1")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Cleared {
		t.Fatalf("reconcile result = %+v, want cleared", result)
	}
	if HasPendingUpdate() {
		t.Fatal("pending transaction survived prepared reconcile")
	}
	// A new update may now start.
	if _, err := PrepareFileUpdate("v1", "v2", tx.TargetPath); err != nil {
		t.Fatalf("new prepare after reconcile: %v", err)
	}
}

func TestReconcileProbationaryKeepsTransaction(t *testing.T) {
	tx, _ := reconcileFileUpdateFixture(t, "v1", "v2")
	publishFileUpdateForTest(t, tx, "new")

	view, err := InspectPendingUpdate("v2")
	if err != nil {
		t.Fatal(err)
	}
	if view.State != UpdateRecoveryProbationary {
		t.Fatalf("state = %s, want probationary", view.State)
	}
	result, err := ReconcilePendingUpdate("v2")
	if !errors.Is(err, ErrPendingUpdateAwaitingHealth) {
		t.Fatalf("reconcile err = %v, want ErrPendingUpdateAwaitingHealth", err)
	}
	if !result.AwaitingHealth {
		t.Fatalf("reconcile result = %+v, want awaitingHealth", result)
	}
	if !HasPendingUpdate() {
		t.Fatal("probationary transaction was removed")
	}
}

func TestReconcileFailedInstallRollsBack(t *testing.T) {
	tx, target := reconcileFileUpdateFixture(t, "v1", "v2")
	if err := MarkUpdateApplyFailedMatching("v2", tx.CreatedAt, "installer exited 5"); err != nil {
		t.Fatal(err)
	}

	view, err := InspectPendingUpdate("v1")
	if err != nil {
		t.Fatal(err)
	}
	if view.State != UpdateRecoveryFailedInstall {
		t.Fatalf("state = %s, want failed_install", view.State)
	}
	// The installer-failure marker is correlated and rolled back by
	// RecoverFailedInstall (the Guard's first startup step).
	result, _, err := RecoverFailedInstall()
	if err != nil {
		t.Fatal(err)
	}
	if !result.RolledBack {
		t.Fatalf("recover result = %+v, want rolled back", result)
	}
	b, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "old" {
		t.Fatalf("binary after rollback = %q, want old", b)
	}
	if HasPendingUpdate() {
		t.Fatal("pending transaction survived failed-install rollback")
	}
	if _, ok := ReadUpdateApplyFailure(); ok {
		t.Fatal("failure marker survived rollback")
	}
	// Idempotent: a second reconcile is a no-op.
	result2, err := ReconcilePendingUpdate("v1")
	if err != nil {
		t.Fatal(err)
	}
	if result2.Pending {
		t.Fatalf("second reconcile result = %+v, want no pending transaction", result2)
	}
}

func TestReconcileRestoredEndsTransaction(t *testing.T) {
	tx, target := reconcileFileUpdateFixture(t, "v1", "v2")
	publishFileUpdateForTest(t, tx, "new")
	// The old release is back in place (a rollback that died before ending
	// the transaction), while the install record still exists.
	if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}

	view, err := InspectPendingUpdate("v1")
	if err != nil {
		t.Fatal(err)
	}
	if view.State != UpdateRecoveryRestored {
		t.Fatalf("state = %s, want restored", view.State)
	}
	// The settled transaction ends through the verified end path (targets
	// match the backups); the mainline rollback correctly refuses because the
	// installed record no longer matches the published state.
	if err := EndPendingUpdateTransactionVerified(tx, func() error {
		return verifyUpdateRestoredUnit(tx)
	}); err != nil {
		t.Fatal(err)
	}
	if HasPendingUpdate() {
		t.Fatal("restored transaction was not ended")
	}
}

func TestReconcileRestoredRefusesTamperedUnit(t *testing.T) {
	tx, target := reconcileFileUpdateFixture(t, "v1", "v2")
	publishFileUpdateForTest(t, tx, "new")
	// The old release is back but with different bytes than the backup.
	if err := os.WriteFile(target, []byte("tampered"), 0o700); err != nil {
		t.Fatal(err)
	}
	view, err := InspectPendingUpdate("v1")
	if err != nil {
		t.Fatal(err)
	}
	if view.State != UpdateRecoveryBlocked {
		t.Fatalf("state = %s, want blocked for tampered restored unit", view.State)
	}
	if !HasPendingUpdate() {
		t.Fatal("blocked transaction must not be deleted")
	}
}

func TestEndRestoredTransactionRefusesUnitChangedAfterInspect(t *testing.T) {
	tx, target := reconcileFileUpdateFixture(t, "v1", "v2")
	publishFileUpdateForTest(t, tx, "new")
	if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	view, inspectedTx, err := InspectPendingUpdateTransaction("v1")
	if err != nil {
		t.Fatal(err)
	}
	if view.State != UpdateRecoveryRestored || inspectedTx == nil {
		t.Fatalf("inspection = %+v tx=%v, want restored with identity", view, inspectedTx)
	}
	if err := os.WriteFile(target, []byte("tampered-after-inspect"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := EndRestoredPendingUpdateTransaction(inspectedTx, "v1"); err == nil {
		t.Fatal("changed restored unit must fail closed")
	}
	if !HasPendingUpdate() {
		t.Fatal("changed restored unit caused pending transaction deletion")
	}
	if _, err := os.Stat(tx.Files[0].BackupPath); err != nil {
		t.Fatalf("changed restored unit caused backup deletion: %v", err)
	}
}

func TestEndRestoredTransactionRefusesUnknownFieldAddedAfterInspect(t *testing.T) {
	tx, target := reconcileFileUpdateFixture(t, "v1", "v2")
	publishFileUpdateForTest(t, tx, "new")
	if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	view, inspectedTx, err := InspectPendingUpdateTransaction("v1")
	if err != nil {
		t.Fatal(err)
	}
	if view.State != UpdateRecoveryRestored || inspectedTx == nil {
		t.Fatalf("inspection = %+v tx=%v, want restored with identity", view, inspectedTx)
	}

	path := PendingUpdatePath()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	document["future_recovery_policy"] = map[string]any{"keep_backup": true}
	mutated, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	mutated = append(mutated, '\n')
	if err := os.WriteFile(path, mutated, 0o600); err != nil {
		t.Fatal(err)
	}
	// An older binary still derives restored from the known fields. The raw
	// transaction snapshot must nevertheless prevent it from discarding a
	// newer binary's unknown recovery metadata.
	current, err := InspectPendingUpdate("v1")
	if err != nil {
		t.Fatal(err)
	}
	if current.State != UpdateRecoveryRestored {
		t.Fatalf("state after adding unknown field = %s, want restored", current.State)
	}

	if err := EndRestoredPendingUpdateTransaction(inspectedTx, "v1"); err == nil {
		t.Fatal("unknown transaction field added after inspection must fail closed")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("changed pending transaction was deleted: %v", err)
	}
	if string(got) != string(mutated) {
		t.Fatalf("changed pending transaction was rewritten: got %q want %q", got, mutated)
	}
	if _, err := os.Stat(tx.Files[0].BackupPath); err != nil {
		t.Fatalf("changed transaction caused backup deletion: %v", err)
	}
	if _, err := os.Stat(installedFileUpdateStatePath(tx)); err != nil {
		t.Fatalf("changed transaction caused installed-state deletion: %v", err)
	}
}

func TestReconcileBlockedCorruptTransaction(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	path := PendingUpdatePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	view, err := InspectPendingUpdate("v1")
	if err != nil {
		t.Fatal(err)
	}
	if view.State != UpdateRecoveryBlocked {
		t.Fatalf("state = %s, want blocked", view.State)
	}
	if _, err := ReconcilePendingUpdate("v1"); err == nil {
		t.Fatal("reconcile of a corrupt transaction must fail closed")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("corrupt transaction was deleted: %v", err)
	}
}

func TestReconcileActiveHandoffWhenOwnerAlive(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app := filepath.Join(dir, "Reasonix.app")
	exe := filepath.Join(app, "Contents", "MacOS", "Reasonix")
	if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	// The transaction must belong to the current Guard installation: point the
	// launcher at the test bundle's executable.
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return exe, nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })
	staging := filepath.Join(os.TempDir(), "reasonix-mac-update-reconcile-test")
	stagedApp := filepath.Join(staging, "Reasonix.app")
	if err := os.MkdirAll(stagedApp, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(staging) })
	// A prepared app-bundle handoff owned by the current (live) process.
	tx, err := PrepareAppBundleUpdateHandoff("v1", "v2", app, app+".reasonix-update-backup", stagedApp, staging, os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	view, err := InspectPendingUpdate("v1")
	if err != nil {
		t.Fatal(err)
	}
	if view.State != UpdateRecoveryActiveHandoff {
		t.Fatalf("state = %s, want active_handoff (owner alive)", view.State)
	}
	if !processAlive(os.Getpid()) {
		t.Fatal("test process must be alive")
	}
	_ = tx
}

func TestReconcileNoneWithoutTransaction(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	view, err := InspectPendingUpdate("v1")
	if err != nil {
		t.Fatal(err)
	}
	if view.State != UpdateRecoveryNone {
		t.Fatalf("state = %s, want none", view.State)
	}
	result, err := ReconcilePendingUpdate("v1")
	if err != nil {
		t.Fatal(err)
	}
	if result.Pending {
		t.Fatalf("reconcile result = %+v, want no pending transaction", result)
	}
}

func TestReconcilePreparedIdempotent(t *testing.T) {
	_, _ = reconcileFileUpdateFixture(t, "v1", "v2")
	first, err := ReconcilePendingUpdate("v1")
	if err != nil {
		t.Fatal(err)
	}
	second, err := ReconcilePendingUpdate("v1")
	if err != nil {
		t.Fatal(err)
	}
	if !first.Cleared || second.Pending {
		t.Fatalf("idempotence broken: first=%+v second=%+v", first, second)
	}
}

func TestReconcilePreparedTargetMismatchRefused(t *testing.T) {
	tx, _ := reconcileFileUpdateFixture(t, "v1", "v2")
	// The prepared transaction's backup disappeared: the transaction is no
	// longer exactly reversible and must fail closed instead of cancelling.
	backup := tx.Files[0].BackupPath
	if err := os.Remove(backup); err != nil {
		t.Fatal(err)
	}
	view, err := InspectPendingUpdate("v1")
	if err != nil {
		t.Fatal(err)
	}
	if view.State != UpdateRecoveryBlocked {
		t.Fatalf("state = %s, want blocked (backup missing)", view.State)
	}
	// The transaction file must survive untouched (fail closed); the backup
	// removal invalidates validation, so check the file directly.
	if _, err := os.Stat(PendingUpdatePath()); err != nil {
		t.Fatalf("blocked transaction file was removed: %v", err)
	}
}

func TestUpdateFailureMarkerWithoutTransactionIsStale(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	// A marker with a transaction identity but no transaction file: surfaced
	// as failed_install, then the stale marker is cleared by reconciliation.
	if err := markUpdateApplyFailed("v2", "2026-01-01T00:00:00Z", "some-transaction-id", "installer exited 5"); err != nil {
		t.Fatal(err)
	}
	view, err := InspectPendingUpdate("v1")
	if err != nil {
		t.Fatal(err)
	}
	if view.State != UpdateRecoveryFailedInstall {
		t.Fatalf("state = %s, want failed_install surfaced", view.State)
	}
	if _, _, err := RecoverFailedInstall(); err != nil {
		t.Fatal(err)
	}
	if _, ok := ReadUpdateApplyFailure(); ok {
		t.Fatal("stale failure marker survived recovery")
	}
}

func TestReconcileActiveHandoffLockTimeout(t *testing.T) {
	tx, _ := reconcileFileUpdateFixture(t, "v1", "v2")
	// Hold the pending-update lock like an in-flight updater helper would.
	unlock, err := acquirePendingUpdateLock()
	if err != nil {
		t.Fatal(err)
	}
	_, err = ReconcilePendingUpdate("v1")
	unlock()
	if err == nil {
		t.Fatal("reconcile under a busy lock must wait or fail, not cancel")
	}
	if !HasPendingUpdate() {
		t.Fatal("transaction must survive a busy lock")
	}
	_ = tx
}
