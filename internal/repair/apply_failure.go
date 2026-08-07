package repair

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/fileutil"
)

// UpdateApplyFailure records that an update installer failed after the desktop
// handed off and exited. The Windows update helper cannot roll back itself —
// it runs from the cache directory, outside the validated Guard installation —
// so it records this marker and relaunches Guard, which performs the rollback
// from inside the install directory on its next start.
type UpdateApplyFailure struct {
	SchemaVersion       int    `json:"schemaVersion"`
	ToVersion           string `json:"toVersion,omitempty"`
	UpdateCreatedAt     string `json:"updateCreatedAt,omitempty"`
	UpdateTransactionID string `json:"updateTransactionId,omitempty"`
	Reason              string `json:"reason,omitempty"`
	RecordedAt          string `json:"recordedAt"`
}

func updateApplyFailurePath() string {
	root := config.MemoryUserDir()
	if root == "" {
		return ""
	}
	return filepath.Join(root, "repair", "update-apply-failed.json")
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

// MarkUpdateApplyFailedMatching binds the marker to the exact transaction held
// by an updater claim. The additive creation identity keeps a same-version
// marker from authorizing rollback of a later retry.
func MarkUpdateApplyFailedMatching(toVersion, updateCreatedAt, reason string) error {
	updateCreatedAt = strings.TrimSpace(updateCreatedAt)
	if updateCreatedAt == "" {
		return fmt.Errorf("update apply failure: transaction identity is incomplete")
	}
	tx, err := ReadPendingUpdate()
	if err != nil {
		return fmt.Errorf("update apply failure: read pending transaction: %w", err)
	}
	if strings.TrimSpace(tx.ToVersion) != strings.TrimSpace(toVersion) ||
		strings.TrimSpace(tx.CreatedAt) != updateCreatedAt {
		return fmt.Errorf("update apply failure: pending transaction does not match")
	}
	return markUpdateApplyFailedInvocation(tx, reason)
}

func markUpdateApplyFailedInvocation(invocation *UpdateTransaction, reason string) error {
	if invocation == nil {
		return fmt.Errorf("update apply failure: transaction identity is incomplete")
	}
	invocationID := UpdateTransactionID(invocation)
	if invocationID == "" {
		return fmt.Errorf("update apply failure: transaction identity is incomplete")
	}
	unlock, err := acquirePendingUpdateLock()
	if err != nil {
		return fmt.Errorf("update apply failure: lock pending transaction: %w", err)
	}
	defer unlock()
	current, err := ReadPendingUpdate()
	if err != nil {
		return fmt.Errorf("update apply failure: read pending transaction: %w", err)
	}
	if UpdateTransactionID(current) != invocationID {
		return fmt.Errorf("update apply failure: pending transaction changed while waiting")
	}
	return markUpdateApplyFailed(
		current.ToVersion,
		current.CreatedAt,
		invocationID,
		reason,
	)
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

func markUpdateApplyFailed(toVersion, updateCreatedAt, updateTransactionID, reason string) error {
	path := updateApplyFailurePath()
	if path == "" {
		return fmt.Errorf("update apply failure: Reasonix state directory is unavailable")
	}
	failure := UpdateApplyFailure{
		SchemaVersion:       1,
		ToVersion:           toVersion,
		UpdateCreatedAt:     updateCreatedAt,
		UpdateTransactionID: updateTransactionID,
		Reason:              reason,
		RecordedAt:          time.Now().UTC().Format(time.RFC3339Nano),
	}
	b, err := json.MarshalIndent(failure, "", "  ")
	if err != nil {
		return err
	}
	return fileutil.AtomicWriteFile(path, append(b, '\n'), 0o600)
}

// ReadUpdateApplyFailure reports the recorded installer failure, if any.
func ReadUpdateApplyFailure() (*UpdateApplyFailure, bool) {
	path := updateApplyFailurePath()
	if path == "" {
		return nil, false
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var failure UpdateApplyFailure
	if json.Unmarshal(b, &failure) != nil || failure.SchemaVersion != 1 {
		return nil, false
	}
	return &failure, true
}

// ClearUpdateApplyFailure removes the marker; a missing marker is not an error.
func ClearUpdateApplyFailure() error {
	failure, ok := ReadUpdateApplyFailure()
	if !ok {
		return nil
	}
	return clearUpdateApplyFailureExact(failure)
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

func clearUpdateApplyFailureExact(expected *UpdateApplyFailure) error {
	if expected == nil {
		return fmt.Errorf("clear update apply failure: marker identity is incomplete")
	}
	path := updateApplyFailurePath()
	if path == "" {
		return nil
	}
	cleanup, err := moveRepairNodeToUniqueCleanup(path)
	if err != nil || cleanup == "" {
		return err
	}
	updateCleanupAfterRename(path, cleanup)
	restore := func(cause error) error {
		if restoreErr := renameRepairNodeNoReplace(cleanup, path); restoreErr != nil {
			return fmt.Errorf("%w; update failure marker retained at %s: %w", cause, cleanup, restoreErr)
		}
		return cause
	}
	b, err := os.ReadFile(cleanup)
	if err != nil {
		return restore(err)
	}
	var actual UpdateApplyFailure
	if err := json.Unmarshal(b, &actual); err != nil {
		return restore(err)
	}
	if repairPlanStateID(&actual) != repairPlanStateID(expected) {
		return restore(fmt.Errorf("clear update apply failure: marker changed"))
	}
	if err := os.Remove(cleanup); err != nil {
		return restore(err)
	}
	return nil
}

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

func applyFailureMatchesUpdate(failure *UpdateApplyFailure, tx *UpdateTransaction) bool {
	if failure == nil || tx == nil {
		return false
	}
	toVersion := strings.TrimSpace(failure.ToVersion)
	if toVersion == "" || toVersion != strings.TrimSpace(tx.ToVersion) {
		return false
	}
	transactionID := strings.TrimSpace(failure.UpdateTransactionID)
	return transactionID != "" && transactionID == UpdateTransactionID(tx)
}
