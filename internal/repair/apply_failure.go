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
	SchemaVersion int    `json:"schemaVersion"`
	ToVersion     string `json:"toVersion,omitempty"`
	Reason        string `json:"reason,omitempty"`
	RecordedAt    string `json:"recordedAt"`
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
	path := updateApplyFailurePath()
	if path == "" {
		return fmt.Errorf("update apply failure: Reasonix state directory is unavailable")
	}
	failure := UpdateApplyFailure{
		SchemaVersion: 1,
		ToVersion:     toVersion,
		Reason:        reason,
		RecordedAt:    time.Now().UTC().Format(time.RFC3339Nano),
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
	path := updateApplyFailurePath()
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// RecoverFailedInstall rolls back the pending update when an update helper
// recorded an installer failure, restoring the previous release unit without
// waiting for a crash loop. The marker is cleared once the rollback succeeded
// (or when nothing was left to roll back); on rollback errors both the marker
// and the pending transaction are kept so the next launch retries.
func RecoverFailedInstall() (UpdateRollbackResult, *UpdateApplyFailure, error) {
	failure, ok := ReadUpdateApplyFailure()
	if !ok {
		return UpdateRollbackResult{}, nil, nil
	}
	tx, txErr := ReadPendingUpdate()
	if txErr != nil {
		if !os.IsNotExist(txErr) {
			return UpdateRollbackResult{}, failure, txErr
		}
		if clearErr := ClearUpdateApplyFailure(); clearErr != nil {
			return UpdateRollbackResult{}, failure, clearErr
		}
		return UpdateRollbackResult{}, failure, nil
	}
	if !applyFailureMatchesUpdate(failure, tx) {
		// A marker can survive when the helper cannot relaunch Guard. Never let
		// that stale marker roll back a later, unrelated update transaction.
		if clearErr := ClearUpdateApplyFailure(); clearErr != nil {
			return UpdateRollbackResult{}, failure, clearErr
		}
		return UpdateRollbackResult{}, failure, nil
	}
	// Re-check the expected version inside the rollback read so a transaction
	// replaced between correlation and recovery is not acted upon.
	result, err := rollbackPendingUpdate(tx.ToVersion, tx.CreatedAt)
	if err != nil {
		return result, failure, err
	}
	if clearErr := ClearUpdateApplyFailure(); clearErr != nil {
		return result, failure, clearErr
	}
	return result, failure, nil
}

func applyFailureMatchesUpdate(failure *UpdateApplyFailure, tx *UpdateTransaction) bool {
	if failure == nil || tx == nil {
		return false
	}
	if toVersion := strings.TrimSpace(failure.ToVersion); toVersion != "" && toVersion != strings.TrimSpace(tx.ToVersion) {
		return false
	}
	// The marker must have been recorded after this transaction began. This
	// rejects both versionless markers from the first Windows helper and stale
	// same-version markers left by an earlier retry.
	failureAt, failureErr := time.Parse(time.RFC3339Nano, failure.RecordedAt)
	txAt, txErr := time.Parse(time.RFC3339Nano, tx.CreatedAt)
	return failureErr == nil && txErr == nil && !failureAt.Before(txAt)
}
