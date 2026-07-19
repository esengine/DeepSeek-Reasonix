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

type RepairChange struct {
	Scope        string `json:"scope,omitempty"`
	TargetPath   string `json:"targetPath"`
	PreviousPath string `json:"previousPath,omitempty"`
	RemoveOnUndo bool   `json:"removeOnUndo,omitempty"`
	// Undone marks a change already reverted by an interrupted undo, so a
	// retry can resume with the remaining changes instead of failing the
	// preflight on the consumed backup of a change that is already restored.
	Undone bool `json:"undone,omitempty"`
}

type RepairTransaction struct {
	SchemaVersion int            `json:"schemaVersion"`
	ID            string         `json:"id"`
	CreatedAt     string         `json:"createdAt"`
	Changes       []RepairChange `json:"changes"`
	Undone        bool           `json:"undone,omitempty"`
	UndoneAt      string         `json:"undoneAt,omitempty"`
}

func newRepairTransaction(now time.Time) *RepairTransaction {
	now = now.UTC()
	return &RepairTransaction{
		SchemaVersion: 1,
		ID:            fmt.Sprintf("repair-%d", now.UnixNano()),
		CreatedAt:     now.Format(time.RFC3339Nano),
		Changes:       []RepairChange{},
	}
}

func repairTransactionPath() string {
	if root := config.MemoryUserDir(); root != "" {
		return filepath.Join(root, "repair", "last-repair.json")
	}
	return ""
}

func repairLogPath() string {
	if root := config.MemoryUserDir(); root != "" {
		return filepath.Join(root, "repair", "repair-log.jsonl")
	}
	return ""
}

func saveRepairTransaction(tx *RepairTransaction) error {
	if tx == nil || len(tx.Changes) == 0 {
		return nil
	}
	if err := persistRepairTransaction(tx); err != nil {
		return err
	}
	appendRepairLogBestEffort(tx)
	return nil
}

// The append-only audit log is best-effort. last-repair.json is the durable
// undo state, so an audit failure must not turn an already committed filesystem
// change into a reported failure or trigger cleanup that consumes its backup.
func appendRepairLogBestEffort(tx *RepairTransaction) {
	_ = appendRepairLog(tx)
}

func persistRepairTransaction(tx *RepairTransaction) error {
	path := repairTransactionPath()
	if path == "" {
		return nil
	}
	b, err := json.MarshalIndent(tx, "", "  ")
	if err != nil {
		return err
	}
	return fileutil.AtomicWriteFile(path, append(b, '\n'), 0o600)
}

func appendRepairLog(tx *RepairTransaction) error {
	path := repairLogPath()
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(tx)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(b, '\n'))
	return err
}

func ReadLastRepair() (*RepairTransaction, error) {
	path := repairTransactionPath()
	if path == "" {
		return nil, os.ErrNotExist
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var tx RepairTransaction
	if err := json.Unmarshal(b, &tx); err != nil {
		return nil, err
	}
	if tx.SchemaVersion != 1 || tx.ID == "" || len(tx.Changes) == 0 {
		return nil, fmt.Errorf("last repair transaction is incomplete")
	}
	for _, change := range tx.Changes {
		if err := validateRepairChange(change); err != nil {
			return nil, err
		}
	}
	return &tx, nil
}

func validateRepairChange(change RepairChange) error {
	target := filepath.Clean(change.TargetPath)
	switch {
	case change.Scope == "global":
		if target != filepath.Clean(config.UserConfigPath()) {
			return fmt.Errorf("repair transaction global target is invalid")
		}
	case change.Scope == "project":
		if filepath.Base(target) != "reasonix.toml" {
			return fmt.Errorf("repair transaction project target is invalid")
		}
	case strings.HasPrefix(change.Scope, "derived:"):
		name := change.Scope[len("derived:"):]
		want, ok := derivedStatePaths()[name]
		if !ok || target != filepath.Clean(want) {
			return fmt.Errorf("repair transaction derived-state target is invalid")
		}
	default:
		return fmt.Errorf("repair transaction scope is invalid")
	}
	if change.RemoveOnUndo {
		if change.Scope != "global" || change.PreviousPath != "" {
			return fmt.Errorf("repair transaction remove-on-undo action is invalid")
		}
		return nil
	}
	previous := filepath.Clean(change.PreviousPath)
	if filepath.Dir(previous) == filepath.Dir(target) && strings.HasPrefix(filepath.Base(previous), filepath.Base(target)+".reasonix-") {
		return nil
	}
	if config.MemoryUserDir() == "" {
		return fmt.Errorf("repair transaction state directory is unavailable")
	}
	restoreRoot := filepath.Join(config.MemoryUserDir(), "repair", "restore-backups")
	rel, err := filepath.Rel(filepath.Clean(restoreRoot), previous)
	if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil
	}
	return fmt.Errorf("repair transaction previous path is invalid")
}

// UndoLastRepair restores the exact files moved aside by the latest repair. Any
// currently repaired file is retained as a timestamped redo candidate.
func UndoLastRepair() (*RepairTransaction, error) {
	tx, err := ReadLastRepair()
	if err != nil {
		return nil, err
	}
	if tx.Undone {
		return nil, fmt.Errorf("repair %s was already undone", tx.ID)
	}
	now := time.Now().UTC()
	for _, change := range tx.Changes {
		if change.RemoveOnUndo || change.Undone {
			continue
		}
		// Lstat: a quarantined symlink counts as present even when its link
		// target is gone — undo restores the link itself, not the target.
		if _, err := os.Lstat(change.PreviousPath); err != nil {
			return nil, fmt.Errorf("undo repair: previous file %s: %w", change.PreviousPath, err)
		}
	}
	// markUndone persists per-change progress so a failure partway through a
	// multi-change undo leaves a transaction the next undo can resume.
	markUndone := func(i int) error {
		tx.Changes[i].Undone = true
		return persistRepairTransaction(tx)
	}
	for i := len(tx.Changes) - 1; i >= 0; i-- {
		change := tx.Changes[i]
		if change.Undone {
			// Progress was persisted but the backup removal may have been cut
			// short by a crash; finish the cleanup the completed step owed.
			if !change.RemoveOnUndo && change.PreviousPath != "" {
				_ = os.Remove(change.PreviousPath)
			}
			continue
		}
		redo := ""
		// Lstat so a dangling symlink at the target is still moved aside
		// instead of being clobbered by the restore below.
		if _, err := os.Lstat(change.TargetPath); err == nil {
			// Index suffix keeps redo names unique when one undo touches the
			// same target twice (e.g. quarantine + snapshot restore): a shared
			// name would silently overwrite the earlier redo copy.
			redo = fmt.Sprintf("%s.reasonix-redo-%s-%d", change.TargetPath, now.Format("20060102T150405.000000000Z"), i)
			if err := os.Rename(change.TargetPath, redo); err != nil {
				return nil, fmt.Errorf("undo repair: retain current file: %w", err)
			}
		}
		if change.RemoveOnUndo {
			if err := markUndone(i); err != nil {
				return nil, err
			}
			continue
		}
		// Restore by copy and keep the backup until the progress record is on
		// disk: consuming the backup first (rename or delete) would leave an
		// unresumable transaction if the process died before markUndone, since
		// the retry's preflight requires the backup of every un-undone change.
		restoreErr := func() error {
			info, err := os.Lstat(change.PreviousPath)
			if err != nil {
				return err
			}
			if info.Mode()&os.ModeSymlink != 0 {
				// A quarantined symlink (e.g. a dotfiles-managed config.toml)
				// must come back as a symlink: ReadFile would follow it and
				// materialize a regular file, permanently severing the link.
				// Recreating a symlink is a single atomic syscall, so the
				// crash-safety of the copy path is preserved.
				linkTarget, err := os.Readlink(change.PreviousPath)
				if err != nil {
					return err
				}
				return os.Symlink(linkTarget, change.TargetPath)
			}
			b, err := os.ReadFile(change.PreviousPath)
			if err != nil {
				return err
			}
			return fileutil.AtomicWriteFile(change.TargetPath, b, info.Mode().Perm())
		}()
		if restoreErr != nil {
			if redo != "" {
				_ = os.Rename(redo, change.TargetPath)
			}
			return nil, fmt.Errorf("undo repair: restore %s: %w", change.TargetPath, restoreErr)
		}
		if err := markUndone(i); err != nil {
			return nil, err
		}
		_ = os.Remove(change.PreviousPath)
	}
	tx.Undone = true
	tx.UndoneAt = now.Format(time.RFC3339Nano)
	if err := saveRepairTransaction(tx); err != nil {
		return nil, err
	}
	return tx, nil
}
