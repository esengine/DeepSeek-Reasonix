package repair

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"time"

	"reasonix/internal/contract/config"
)

// RecoverFailedInstall rolls back the pending update when an update helper
// recorded an installer failure, restoring the previous release unit without
// waiting for a crash loop. The marker is cleared once the rollback succeeded
// (or when nothing was left to roll back); on rollback errors both the marker
// and the pending transaction are kept so the next launch retries.
func RecoverFailedInstall() (UpdateRollbackResult, *UpdateApplyFailure, error) {
	invocationFailure, ok := ReadUpdateApplyFailure()
	if !ok {
		return UpdateRollbackResult{}, nil, nil
	}
	invocationFailureID := repairPlanStateID(invocationFailure)
	invocationTx, invocationTxErr := ReadPendingUpdate()
	if invocationTxErr != nil && !os.IsNotExist(invocationTxErr) {
		return UpdateRollbackResult{}, invocationFailure, invocationTxErr
	}
	unlock, lockErr := acquirePendingUpdateLock()
	if lockErr != nil {
		return UpdateRollbackResult{}, invocationFailure, fmt.Errorf("recover failed install: lock pending transaction: %w", lockErr)
	}
	defer unlock()
	failure, ok := ReadUpdateApplyFailure()
	if !ok {
		return UpdateRollbackResult{}, nil, nil
	}
	if repairPlanStateID(failure) != invocationFailureID {
		return UpdateRollbackResult{}, failure, fmt.Errorf("recover failed install: failure marker changed while waiting")
	}
	tx, txErr := ReadPendingUpdate()
	if txErr != nil {
		if !os.IsNotExist(txErr) {
			return UpdateRollbackResult{}, failure, txErr
		}
		if clearErr := clearUpdateApplyFailureExact(failure); clearErr != nil {
			return UpdateRollbackResult{}, failure, clearErr
		}
		return UpdateRollbackResult{}, failure, nil
	}
	if invocationTxErr == nil && UpdateTransactionID(tx) != UpdateTransactionID(invocationTx) {
		return UpdateRollbackResult{}, failure, fmt.Errorf("recover failed install: pending transaction changed while waiting")
	}
	if os.IsNotExist(invocationTxErr) {
		if clearErr := clearUpdateApplyFailureExact(failure); clearErr != nil {
			return UpdateRollbackResult{}, failure, clearErr
		}
		return UpdateRollbackResult{}, failure, nil
	}
	if !applyFailureMatchesUpdate(failure, tx) {
		// A marker can survive when the helper cannot relaunch Guard. Never let
		// that stale marker roll back a later, unrelated update transaction.
		if clearErr := clearUpdateApplyFailureExact(failure); clearErr != nil {
			return UpdateRollbackResult{}, failure, clearErr
		}
		return UpdateRollbackResult{}, failure, nil
	}
	// Keep the exact identity check in the rollback transition as a second
	// fail-closed guard even though correlation and recovery share this lock.
	stateID, states := pendingUpdateBoundPreview(tx)
	result, err := rollbackPendingUpdateMatchingLocked(
		tx.ToVersion,
		tx.CreatedAt,
		stateID,
		states,
		UpdateTransactionID(tx),
		false,
	)
	if err != nil {
		return result, failure, err
	}
	if clearErr := clearUpdateApplyFailureExact(failure); clearErr != nil {
		return result, failure, clearErr
	}
	return result, failure, nil
}

// ClearUpdateApplyFailureExact removes only the marker created for tx. Platform
// updaters call this after the installed release-unit state is durable; a marker
// concurrently replaced by another transaction is retained.
func ClearUpdateApplyFailureExact(tx *UpdateTransaction) error {
	if tx == nil {
		return fmt.Errorf("clear update apply failure: transaction identity is incomplete")
	}
	expectedID := UpdateTransactionID(tx)
	if expectedID == "" {
		return fmt.Errorf("clear update apply failure: transaction identity is incomplete")
	}
	failure, ok := ReadUpdateApplyFailure()
	if !ok {
		return nil
	}
	if strings.TrimSpace(failure.UpdateTransactionID) != expectedID {
		return fmt.Errorf("clear update apply failure: marker does not match transaction")
	}
	return clearUpdateApplyFailureExact(failure)
}

// MarkUpdateApplyFailedExact records a failure for the complete transaction
// held by an updater claim. The caller must keep that claim's pending lock
// until this write returns; taking it again here would deadlock the updater.
func MarkUpdateApplyFailedExact(tx *UpdateTransaction, reason string) error {
	if tx == nil || strings.TrimSpace(tx.CreatedAt) == "" {
		return fmt.Errorf("update apply failure: transaction identity is incomplete")
	}
	transactionID := UpdateTransactionID(tx)
	if transactionID == "" {
		return fmt.Errorf("update apply failure: transaction identity is incomplete")
	}
	return markUpdateApplyFailed(tx.ToVersion, tx.CreatedAt, transactionID, reason)
}

// MarkUpdateApplyFailed persists the installer-failure marker. It is written
// by the update helper after the NSIS installer exits non-zero.
func MarkUpdateApplyFailed(toVersion, reason string) error {
	tx, err := ReadPendingUpdate()
	if err == nil {
		if strings.TrimSpace(tx.ToVersion) != strings.TrimSpace(toVersion) {
			return fmt.Errorf("update apply failure: pending transaction does not match")
		}
		return markUpdateApplyFailedInvocation(tx, reason)
	}
	if !os.IsNotExist(err) {
		return fmt.Errorf("update apply failure: read pending transaction: %w", err)
	}
	// Keep accepting a diagnostic marker when no matching transaction exists.
	// Recovery treats markers without a complete transaction ID as stale and
	// never lets them authorize rollback.
	unlock, lockErr := acquirePendingUpdateLock()
	if lockErr != nil {
		return fmt.Errorf("update apply failure: lock pending transaction: %w", lockErr)
	}
	defer unlock()
	if _, currentErr := ReadPendingUpdate(); currentErr == nil {
		return fmt.Errorf("update apply failure: pending transaction appeared while waiting")
	} else if !os.IsNotExist(currentErr) {
		return fmt.Errorf("update apply failure: read pending transaction: %w", currentErr)
	}
	return markUpdateApplyFailed(toVersion, "", "", reason)
}

// RollbackPendingUpdateMatching rolls back only the exact transaction prepared
// by the caller. This is used when an apply attempt fails after another process
// may already have replaced pending-update.json with a same-version retry.
func RollbackPendingUpdateMatching(expectedToVersion, expectedCreatedAt string) (UpdateRollbackResult, error) {
	expectedToVersion = strings.TrimSpace(expectedToVersion)
	expectedCreatedAt = strings.TrimSpace(expectedCreatedAt)
	if expectedToVersion == "" || expectedCreatedAt == "" {
		return UpdateRollbackResult{}, fmt.Errorf("rollback update: transaction identity is incomplete")
	}
	return rollbackPendingUpdateInvocation(expectedToVersion, expectedCreatedAt, "")
}

// CancelPendingUpdateMatching removes only the exact transaction prepared by
// the caller. It is used by updater failure paths where a same-version retry can
// replace pending-update.json before cleanup runs.
func CancelPendingUpdateMatching(toVersion, expectedCreatedAt string) error {
	expectedCreatedAt = strings.TrimSpace(expectedCreatedAt)
	if expectedCreatedAt == "" {
		return nil
	}
	return cancelPendingUpdateInvocation(toVersion, expectedCreatedAt, "")
}

// MarkUpdateHealthyMatching commits only the exact pending transaction observed
// when this desktop process started. The creation identity prevents an older
// process from blessing a later same-version retry.
func MarkUpdateHealthyMatching(runningVersion, expectedCreatedAt string) error {
	expectedCreatedAt = strings.TrimSpace(expectedCreatedAt)
	if expectedCreatedAt == "" {
		return nil
	}
	return markUpdateHealthyInvocation(runningVersion, expectedCreatedAt, "")
}

// AbandonPendingUpdate is the user-initiated recovery path for a stuck
// transaction: commit if possible, else reconcile, else force-retire.
func AbandonPendingUpdate(runningVersion string) (PendingUpdateReconcileResult, error) {
	committed, commitErr := CommitProbationaryPendingUpdate(runningVersion)
	if commitErr == nil && committed {
		return PendingUpdateReconcileResult{Cleared: true, Healthy: true}, nil
	}
	if commitErr != nil {
		// Keep going: a drifted backup must not block explicit discard.
		slog.Debug("repair: probationary commit during abandon failed; continuing",
			"err", commitErr)
	}
	result, reconcileErr := ReconcilePendingUpdate(runningVersion)
	if reconcileErr == nil {
		return result, nil
	}
	// Force-retire when still AwaitingHealth with the live target installed.
	if errors.Is(reconcileErr, ErrPendingUpdateAwaitingHealth) {
		if retired, retireErr := forceRetireProbationaryPendingUpdate(runningVersion); retireErr != nil {
			return result, fmt.Errorf("abandon pending update: %w", retireErr)
		} else if retired {
			result.Pending = false
			result.AwaitingHealth = false
			result.Healthy = true
			result.Cleared = true
			return result, nil
		}
	}
	if commitErr != nil && reconcileErr != nil {
		return result, fmt.Errorf("abandon pending update: %w", errors.Join(reconcileErr, commitErr))
	}
	return result, reconcileErr
}

func HasPendingUpdate() bool {
	_, err := ReadPendingUpdate()
	return err == nil
}

// CancelPendingAppBundleUpdateHandoff abandons an exact handoff only when the
// original installed bundle is still the tree captured during prepare. This is
// the safe recovery path when source verification fails after the desktop has
// exited but before any bundle swap occurred.
func CancelPendingAppBundleUpdateHandoff(
	expectedToVersion, expectedCreatedAt string,
	timeout time.Duration,
) (*UpdateTransaction, error) {
	tx, err := ReadPendingUpdate()
	if err != nil {
		return nil, fmt.Errorf("cancel update handoff: read pending transaction: %w", err)
	}
	if tx.TargetKind != "app-bundle" ||
		strings.TrimSpace(tx.ToVersion) != strings.TrimSpace(expectedToVersion) ||
		strings.TrimSpace(tx.CreatedAt) != strings.TrimSpace(expectedCreatedAt) {
		return nil, fmt.Errorf("cancel update handoff: pending transaction does not match")
	}
	return cancelPendingAppBundleUpdateHandoff(
		expectedToVersion,
		expectedCreatedAt,
		timeout,
		UpdateTransactionID(tx),
	)
}

// RecordClaimedFileUpdateInstalled binds the complete post-install release unit
// while the platform updater still holds the claim's pending and target locks.
// The binding is a transaction-unique create-only sidecar: pending-update.json
// stays immutable, so a process crash can never strand rollback state in the
// gap between displacing the old pending file and publishing a replacement.
func RecordClaimedFileUpdateInstalled(
	claimed *UpdateTransaction,
	receipts ...FileUpdateInstallReceipt,
) (*UpdateTransaction, error) {
	if claimed == nil || claimed.TargetKind != "file" {
		return nil, fmt.Errorf("record installed update: transaction identity is incomplete")
	}
	current, err := readPendingUpdateForLauncher(claimed.TargetPath)
	if err != nil {
		return nil, fmt.Errorf("record installed update: read pending transaction: %w", err)
	}
	if !reflect.DeepEqual(claimed, current) {
		return nil, fmt.Errorf("record installed update: pending transaction changed")
	}
	if len(current.Files) == 0 {
		return nil, fmt.Errorf("record installed update: release unit is incomplete")
	}
	record := &installedFileUpdateState{
		SchemaVersion:       1,
		UpdateTransactionID: UpdateTransactionID(current),
		InstalledStateIDs:   make([]string, len(current.Files)),
	}
	receiptStates := make(map[string]string, len(receipts))
	for _, receipt := range receipts {
		if strings.TrimSpace(receipt.UpdateTransactionID) != record.UpdateTransactionID {
			return nil, fmt.Errorf("record installed update: publish receipt belongs to a different transaction")
		}
		targetKey := canonicalRepairPath(receipt.TargetPath)
		if targetKey == "" {
			return nil, fmt.Errorf("record installed update: publish receipt target is invalid")
		}
		stateID := strings.TrimSpace(receipt.InstalledStateID)
		if len(stateID) != sha256.Size*2 {
			return nil, fmt.Errorf("record installed update: publish receipt state is invalid")
		}
		if _, err := hex.DecodeString(stateID); err != nil {
			return nil, fmt.Errorf("record installed update: publish receipt state is invalid")
		}
		if _, exists := receiptStates[targetKey]; exists {
			return nil, fmt.Errorf("record installed update: duplicate publish receipt")
		}
		receiptStates[targetKey] = stateID
	}
	for i := range current.Files {
		f := &current.Files[i]
		targetKey := canonicalRepairPath(f.TargetPath)
		if stateID, ok := receiptStates[targetKey]; ok {
			record.InstalledStateIDs[i] = stateID
			delete(receiptStates, targetKey)
			continue
		}
		info, statErr := os.Lstat(f.TargetPath)
		if statErr != nil {
			if os.IsNotExist(statErr) && f.MissingBefore {
				record.InstalledStateIDs[i] = repairPlanReleaseNodeState(f.TargetPath)
				continue
			}
			return nil, fmt.Errorf("record installed update: inspect %s: %w", filepath.Base(f.TargetPath), statErr)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("record installed update: %s is not a regular file", filepath.Base(f.TargetPath))
		}
		return nil, fmt.Errorf("record installed update: publish receipt is missing for %s", filepath.Base(f.TargetPath))
	}
	if len(receiptStates) != 0 {
		return nil, fmt.Errorf("record installed update: publish receipt target is outside the release unit")
	}
	for i, f := range current.Files {
		if err := verifyRepairPlanReleaseNodeStateFor(f.TargetPath, f.TargetPath, record.InstalledStateIDs[i]); err != nil {
			return nil, fmt.Errorf("record installed update: release unit changed while recording: %w", err)
		}
	}
	if err := createInstalledFileUpdateState(current, record); err != nil {
		return nil, fmt.Errorf("record installed update: %w", err)
	}
	installedUpdateAfterCreate(installedFileUpdateStatePath(current))
	latest, err := readPendingUpdateForLauncher(claimed.TargetPath)
	if err != nil {
		return nil, fmt.Errorf("record installed update: re-read pending transaction: %w", err)
	}
	if !reflect.DeepEqual(current, latest) {
		return nil, fmt.Errorf("record installed update: pending transaction changed")
	}
	if _, _, err := installedFileUpdateTargets(latest, true); err != nil {
		return nil, fmt.Errorf("record installed update: %w", err)
	}
	return latest, nil
}

// PublishClaimedFileUpdateMember replaces one release-unit member without ever
// overwriting an unverified node. The platform updater must hold the claim
// returned by ClaimPendingFileUpdateExact for the whole release-unit operation.
// A concurrent recreation after the prepared node moves aside wins; the new
// bytes and the verified prior node remain staged for recovery.
func PublishClaimedFileUpdateMember(claimed *UpdateTransaction, targetPath string, content []byte, mode os.FileMode) error {
	_, err := PublishClaimedFileUpdateMemberExact(claimed, targetPath, content, mode)
	return err
}

// ClaimPendingFileUpdateExact additionally binds the updater to every field in
// the transaction prepared by the desktop process.
func ClaimPendingFileUpdateExact(
	expectedToVersion, expectedCreatedAt, expectedTransactionID, launcherPath string,
	expectedTargetPaths []string,
	timeout time.Duration,
) (*UpdateTransaction, func(), error) {
	expectedTransactionID = strings.TrimSpace(expectedTransactionID)
	if expectedTransactionID == "" {
		return nil, nil, fmt.Errorf("claim file update: transaction identity is incomplete")
	}
	return claimPendingFileUpdate(
		expectedToVersion,
		expectedCreatedAt,
		expectedTransactionID,
		launcherPath,
		expectedTargetPaths,
		timeout,
	)
}

// ClaimPendingFileUpdate binds an updater's actual replacement window to the
// exact transaction and release-unit paths prepared by the desktop. The
// launcher path is explicit because the Windows helper runs from a cache
// directory rather than from the installation it is authorized to replace.
func ClaimPendingFileUpdate(
	expectedToVersion, expectedCreatedAt, launcherPath string,
	expectedTargetPaths []string,
	timeout time.Duration,
) (*UpdateTransaction, func(), error) {
	tx, err := readPendingUpdateForLauncher(launcherPath)
	if err != nil {
		return nil, nil, fmt.Errorf("claim file update: read pending transaction: %w", err)
	}
	if tx.TargetKind != "file" ||
		strings.TrimSpace(tx.ToVersion) != strings.TrimSpace(expectedToVersion) ||
		strings.TrimSpace(tx.CreatedAt) != strings.TrimSpace(expectedCreatedAt) {
		return nil, nil, fmt.Errorf("claim file update: pending transaction does not match")
	}
	return claimPendingFileUpdate(
		expectedToVersion,
		expectedCreatedAt,
		UpdateTransactionID(tx),
		launcherPath,
		expectedTargetPaths,
		timeout,
	)
}

// ClaimPendingAppBundleUpdateHandoff authorizes a detached child to perform the
// recorded bundle swap. It returns with both the pending transaction lock and
// the target mutation locks held; release must be called on every path.
func ClaimPendingAppBundleUpdateHandoff(expectedToVersion, expectedCreatedAt string, timeout time.Duration) (*UpdateTransaction, func(), error) {
	tx, err := ReadPendingUpdate()
	if err != nil {
		return nil, nil, fmt.Errorf("claim update handoff: read pending transaction: %w", err)
	}
	if tx.TargetKind != "app-bundle" ||
		strings.TrimSpace(tx.ToVersion) != strings.TrimSpace(expectedToVersion) ||
		strings.TrimSpace(tx.CreatedAt) != strings.TrimSpace(expectedCreatedAt) {
		return nil, nil, fmt.Errorf("claim update handoff: pending transaction does not match")
	}
	return claimPendingAppBundleUpdateHandoff(
		expectedToVersion,
		expectedCreatedAt,
		UpdateTransactionID(tx),
		timeout,
	)
}

// PrepareAppBundleUpdate records the sibling bundle backup that the macOS
// handoff script creates. The script performs the directory move after exit.
func PrepareAppBundleUpdate(fromVersion, toVersion, appPath, backupPath string) (*UpdateTransaction, error) {
	tx, err := newAppBundleUpdateTransaction(fromVersion, toVersion, appPath, backupPath)
	if err != nil {
		return nil, err
	}
	unlock, err := acquirePendingUpdateLock()
	if err != nil {
		return nil, fmt.Errorf("prepare update: lock pending transaction: %w", err)
	}
	defer unlock()
	if err := ensureNoPendingUpdate(); err != nil {
		return nil, err
	}
	unlockTargets, lockErr := lockRepairMutations(tx.TargetPath, tx.BackupPath)
	if lockErr != nil {
		return nil, fmt.Errorf("prepare update: lock targets: %w", lockErr)
	}
	defer unlockTargets()
	tx.BackupTreeID, err = repairPlanTreeContentStateID(tx.TargetPath)
	if err != nil {
		return nil, fmt.Errorf("prepare update: current bundle digest: %w", err)
	}
	if err := ensureNoPendingUpdate(); err != nil {
		return nil, err
	}
	if err := createPendingUpdate(tx); err != nil {
		return nil, err
	}
	return tx, nil
}

// PrepareFileUpdate snapshots the current desktop executable — plus any sibling
// binaries of the release unit the installer also replaces (Guard, launcher,
// update helper) — and records an update transaction before an updater applies
// the replacement. Sibling paths that do not exist are recorded explicitly so
// rollback can remove files introduced by the replacement release.
func PrepareFileUpdate(fromVersion, toVersion, targetPath string, siblingPaths ...string) (*UpdateTransaction, error) {
	targetPath = filepath.Clean(strings.TrimSpace(targetPath))
	if targetPath == "" || targetPath == "." {
		return nil, fmt.Errorf("prepare update: empty target path")
	}
	root := config.MemoryUserDir()
	if root == "" {
		return nil, fmt.Errorf("prepare update: Reasonix state directory is unavailable")
	}
	unlock, err := acquirePendingUpdateLock()
	if err != nil {
		return nil, fmt.Errorf("prepare update: lock pending transaction: %w", err)
	}
	defer unlock()
	if err := ensureNoPendingUpdate(); err != nil {
		return nil, err
	}
	// Hold the same target locks as rollback so prepare/snapshot cannot race
	// a concurrent Guard restore of the release unit.
	lockPaths := append([]string{targetPath}, siblingPaths...)
	unlockTargets, lockErr := lockRepairMutations(lockPaths...)
	if lockErr != nil {
		return nil, fmt.Errorf("prepare update: lock targets: %w", lockErr)
	}
	defer unlockTargets()
	backupDir := filepath.Join(root, "repair", "updates")
	if err := os.MkdirAll(backupDir, 0o700); err != nil {
		return nil, err
	}
	if !pathInsideResolvedRoot(filepath.Join(root, "repair"), backupDir) {
		return nil, fmt.Errorf("prepare update: backup directory resolves outside the repair directory")
	}
	tx := &UpdateTransaction{
		SchemaVersion: updateTransactionVersion,
		FromVersion:   fromVersion,
		ToVersion:     toVersion,
		Platform:      runtime.GOOS + "/" + runtime.GOARCH,
		TargetKind:    "file",
		TargetPath:    targetPath,
		CreatedAt:     time.Now().UTC().Format(time.RFC3339Nano),
	}
	seen := map[string]bool{}
	for i, path := range append([]string{targetPath}, siblingPaths...) {
		path = filepath.Clean(strings.TrimSpace(path))
		key := canonicalRepairPath(path)
		if path == "" || path == "." || key == "" || seen[key] {
			continue
		}
		seen[key] = true
		info, statErr := os.Lstat(path)
		if statErr != nil {
			if i > 0 && os.IsNotExist(statErr) {
				tx.Files = append(tx.Files, UpdateTransactionFile{TargetPath: path, MissingBefore: true})
				continue
			}
			return nil, fmt.Errorf("prepare update backup: %w", statErr)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("prepare update backup: release file %s is not a regular file", filepath.Base(path))
		}
		backupIdentity := repairPlanStateID(struct {
			CreatedAt  string `json:"createdAt"`
			TargetPath string `json:"targetPath"`
			Index      int    `json:"index"`
		}{
			CreatedAt:  tx.CreatedAt,
			TargetPath: canonicalRepairPath(path),
			Index:      i,
		})
		backupPath := filepath.Join(
			backupDir,
			fmt.Sprintf("%s.%s.previous", filepath.Base(path), backupIdentity[:16]),
		)
		hash, err := copyFileWithHashCreate(path, backupPath, 0o700)
		if err != nil {
			return nil, fmt.Errorf("prepare update backup: %w", err)
		}
		tx.Files = append(tx.Files, UpdateTransactionFile{TargetPath: path, BackupPath: backupPath, SHA256: hash})
		if i == 0 {
			tx.BackupPath = backupPath
			tx.BackupSHA256 = hash
		}
	}
	if err := verifyPreparedFileUpdateTargets(tx); err != nil {
		return nil, fmt.Errorf("prepare update: %w", err)
	}
	if err := ensureNoPendingUpdate(); err != nil {
		return nil, err
	}
	if err := createPendingUpdate(tx); err != nil {
		return nil, err
	}
	return tx, nil
}
