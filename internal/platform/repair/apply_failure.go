package repair

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"reasonix/internal/base/fileutil"
	"reasonix/internal/contract/config"
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
