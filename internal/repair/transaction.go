package repair

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/fileutil"
)

type RepairChange struct {
	Scope           string `json:"scope,omitempty"`
	TargetPath      string `json:"targetPath"`
	PreviousPath    string `json:"previousPath,omitempty"`
	PreviousStateID string `json:"previousStateId,omitempty"`
	RemoveOnUndo    bool   `json:"removeOnUndo,omitempty"`
	// CreatedStateID binds a remove-on-undo entry to the exact node this repair
	// intended to create. If type, mode, or bytes later differ, undo preserves
	// it as an unowned concurrent write.
	CreatedStateID string `json:"createdStateId,omitempty"`
	// Prepared is a write-ahead filesystem-mutation intent. The exact previous
	// state may still be at TargetPath (rename not run) or at PreviousPath
	// (rename committed). Undo resolves either state without guessing.
	Prepared bool `json:"prepared,omitempty"`
	// Undone marks a change already reverted by an interrupted undo, so a
	// retry can resume with the remaining changes instead of failing the
	// preflight on the consumed backup of a change that is already restored.
	Undone bool `json:"undone,omitempty"`
}

type RepairTransaction struct {
	SchemaVersion             int            `json:"schemaVersion"`
	ID                        string         `json:"id"`
	CreatedAt                 string         `json:"createdAt"`
	Changes                   []RepairChange `json:"changes"`
	PreparedLastRepairStateID string         `json:"preparedLastRepairStateId,omitempty"`
	Undone                    bool           `json:"undone,omitempty"`
	UndoneAt                  string         `json:"undoneAt,omitempty"`
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

func repairChangeForPrevious(scope, target, previous string) RepairChange {
	return RepairChange{
		Scope:           scope,
		TargetPath:      target,
		PreviousPath:    previous,
		PreviousStateID: repairPlanReleaseNodeStateFor(previous, target),
	}
}

func preparedRepairChangeForPrevious(scope, target, previous string) RepairChange {
	return RepairChange{
		Scope:           scope,
		TargetPath:      target,
		PreviousPath:    previous,
		PreviousStateID: repairPlanReleaseNodeStateFor(target, target),
		Prepared:        true,
	}
}

func preparedRepairChangeForCreate(scope, target, createdStateID string) RepairChange {
	return RepairChange{
		Scope:          scope,
		TargetPath:     target,
		RemoveOnUndo:   true,
		CreatedStateID: createdStateID,
		Prepared:       true,
	}
}

func repairTransactionPath() string {
	if root := config.MemoryUserDir(); root != "" {
		return filepath.Join(root, "repair", "last-repair.json")
	}
	return ""
}

func pendingRepairTransactionPath() string {
	if root := config.MemoryUserDir(); root != "" {
		return filepath.Join(root, "repair", "pending-repair.json")
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
	if tx != nil {
		if strings.TrimSpace(tx.PreparedLastRepairStateID) != "" {
			return fmt.Errorf("last repair transaction contains pending-only state")
		}
		for _, change := range tx.Changes {
			if change.Prepared {
				return fmt.Errorf("last repair transaction contains a prepared change")
			}
		}
	}
	path := repairTransactionPath()
	if path == "" {
		return nil
	}
	b, err := json.MarshalIndent(tx, "", "  ")
	if err != nil {
		return err
	}
	if err := fileutil.AtomicWriteFile(path, append(b, '\n'), 0o600); err != nil {
		return err
	}
	repairTransactionAfterPersist(tx)
	return nil
}

var repairTransactionAfterPersist = func(*RepairTransaction) {}
var repairPendingAfterMove = func(string, string) {}

func persistPreparedRepairTransaction(tx *RepairTransaction) error {
	if tx == nil || len(tx.Changes) == 0 || !tx.Changes[len(tx.Changes)-1].Prepared {
		return fmt.Errorf("pending repair transaction is incomplete")
	}
	path := pendingRepairTransactionPath()
	if path == "" {
		return fmt.Errorf("pending repair state path is unavailable")
	}
	if _, err := os.Lstat(repairTransactionPath()); err == nil {
		if _, err := ReadLastRepair(); err != nil {
			return fmt.Errorf("prepare repair: current undo state is invalid: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("prepare repair: inspect current undo state: %w", err)
	}
	// Bind the journal to the undo state it is allowed to replace. A stale
	// pending file must never promote itself over a newer completed repair.
	tx.PreparedLastRepairStateID = repairPlanReleaseNodeState(repairTransactionPath())
	b, err := json.MarshalIndent(tx, "", "  ")
	if err != nil {
		return err
	}
	if err := fileutil.AtomicCreateFile(path, append(b, '\n'), 0o600); err != nil {
		return fmt.Errorf("publish pending repair transaction: %w", err)
	}
	return nil
}

// verifyPreparedCreateOwnership proves that AtomicCreateFile published the
// state recorded before the mutation. It must never rebind to whatever happens
// to be at path afterward: an uncooperative writer could replace the file in
// that window and would then be incorrectly claimed and removed by undo.
func verifyPreparedCreateOwnership(tx *RepairTransaction, changeIndex int, path string) error {
	if tx == nil || changeIndex < 0 || changeIndex >= len(tx.Changes) {
		return fmt.Errorf("prepared create ownership is incomplete")
	}
	change := tx.Changes[changeIndex]
	if !change.Prepared || !change.RemoveOnUndo {
		return fmt.Errorf("prepared create ownership is incomplete")
	}
	expected := strings.TrimSpace(change.CreatedStateID)
	if !validRepairStateID(expected) {
		return fmt.Errorf("prepared create ownership is incomplete")
	}
	if err := verifyRepairPlanReleaseNodeStateFor(path, path, expected); err != nil {
		return fmt.Errorf("published create ownership changed: %w", err)
	}
	return nil
}

func clearPreparedRepairTransaction(expected *RepairTransaction) error {
	if expected == nil {
		return fmt.Errorf("pending repair transaction identity is incomplete")
	}
	path := pendingRepairTransactionPath()
	if path == "" {
		return nil
	}
	cleanup, err := moveRepairNodeToUniqueCleanup(path)
	if err != nil || cleanup == "" {
		return err
	}
	repairPendingAfterMove(path, cleanup)
	restoreUnknown := func(cause error) error {
		if restoreErr := renameRepairNodeNoReplace(cleanup, path); restoreErr != nil {
			return fmt.Errorf("%w; displaced pending repair retained at %s: %w", cause, cleanup, restoreErr)
		}
		return cause
	}
	b, err := os.ReadFile(cleanup)
	if err != nil {
		return restoreUnknown(fmt.Errorf("read displaced pending repair: %w", err))
	}
	var actual RepairTransaction
	if err := json.Unmarshal(b, &actual); err != nil {
		return restoreUnknown(fmt.Errorf("pending repair transaction changed before cleanup: %w", err))
	}
	if !reflect.DeepEqual(&actual, expected) {
		return restoreUnknown(fmt.Errorf("pending repair transaction changed before cleanup"))
	}
	// Do not unlink the moved journal. A path-based remove has an unavoidable
	// check/remove race: an uncooperative writer could replace cleanup after the
	// equality check and have its node deleted. The uniquely named, inactive
	// journal is small and doubles as crash-recovery evidence.
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("a new pending repair transaction appeared during cleanup")
	} else if !os.IsNotExist(err) {
		return err
	}
	return nil
}

// reconcilePreparedRepairTransaction runs while the repair transaction lock is
// held. It promotes a crashed post-rename intent into last-repair.json, or
// discards an intent whose exact source is still live and therefore never
// committed. Ambiguous state remains fail closed for manual recovery.
func reconcilePreparedRepairTransaction() error {
	path := pendingRepairTransactionPath()
	if path == "" {
		return nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var tx RepairTransaction
	if err := json.Unmarshal(b, &tx); err != nil {
		return fmt.Errorf("pending repair transaction is invalid: %w", err)
	}
	if tx.SchemaVersion != 1 || tx.ID == "" || len(tx.Changes) == 0 ||
		!validRepairStateID(tx.PreparedLastRepairStateID) {
		return fmt.Errorf("pending repair transaction is incomplete")
	}
	for i, change := range tx.Changes {
		if err := validateRepairChange(change); err != nil {
			return err
		}
		if change.Prepared != (i == len(tx.Changes)-1) {
			return fmt.Errorf("pending repair transaction has an invalid prepared prefix")
		}
	}
	last := tx.Changes[len(tx.Changes)-1]
	unlockTargets, err := lockRepairMutations(last.TargetPath, last.PreviousPath)
	if err != nil {
		return err
	}
	defer unlockTargets()

	// The target lock may have blocked behind another cooperative process.
	// Re-read the journal so that process cannot substitute a different intent
	// between validation and promotion.
	currentPendingBytes, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var currentPending RepairTransaction
	if err := json.Unmarshal(currentPendingBytes, &currentPending); err != nil {
		return fmt.Errorf("pending repair transaction changed while waiting: %w", err)
	}
	if !reflect.DeepEqual(&currentPending, &tx) {
		return fmt.Errorf("pending repair transaction changed while waiting")
	}

	committed := tx
	committed.Changes = append([]RepairChange(nil), tx.Changes...)
	committed.Changes[len(committed.Changes)-1].Prepared = false
	committed.PreparedLastRepairStateID = ""
	currentLast, readLastErr := ReadLastRepair()
	if readLastErr == nil && reflect.DeepEqual(currentLast, &committed) {
		// last-repair.json was already published and only journal cleanup was
		// interrupted. Its backup must not be compensated or reclassified.
		return clearPreparedRepairTransaction(&tx)
	}
	if readLastErr != nil && !os.IsNotExist(readLastErr) {
		return fmt.Errorf("pending repair transaction cannot replace invalid undo state: %w", readLastErr)
	}
	if actual := repairPlanReleaseNodeState(repairTransactionPath()); actual != tx.PreparedLastRepairStateID {
		return fmt.Errorf("pending repair transaction no longer matches the previous undo state")
	}
	applied, err := preparedRepairChangeApplied(last)
	if err != nil {
		return err
	}
	if applied {
		if err := persistRepairTransaction(&committed); err != nil {
			return err
		}
	}
	return clearPreparedRepairTransaction(&tx)
}

// commitPreparedRepairTransaction reports whether last-repair.json became
// durable separately from journal-cleanup errors. Once durable is true callers
// must retain the backup referenced by the undo record and never compensate the
// already committed rename.
func commitPreparedRepairTransaction(tx *RepairTransaction, changeIndex int) (durable bool, err error) {
	if tx == nil || changeIndex < 0 || changeIndex >= len(tx.Changes) || !tx.Changes[changeIndex].Prepared {
		return false, fmt.Errorf("prepared repair transaction is incomplete")
	}
	pending := *tx
	pending.Changes = append([]RepairChange(nil), tx.Changes...)
	tx.Changes[changeIndex].Prepared = false
	tx.PreparedLastRepairStateID = ""
	if err := persistRepairTransaction(tx); err != nil {
		tx.Changes[changeIndex].Prepared = true
		tx.PreparedLastRepairStateID = pending.PreparedLastRepairStateID
		return false, err
	}
	if err := clearPreparedRepairTransaction(&pending); err != nil {
		return true, err
	}
	return true, nil
}

func validRepairStateID(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) != sha256.Size*2 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
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
	if strings.TrimSpace(tx.PreparedLastRepairStateID) != "" {
		return nil, fmt.Errorf("last repair transaction contains pending-only state")
	}
	for _, change := range tx.Changes {
		if change.Prepared {
			return nil, fmt.Errorf("last repair transaction contains a prepared change")
		}
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
		value := strings.TrimSpace(change.CreatedStateID)
		if change.Prepared && value == "" {
			return fmt.Errorf("repair transaction prepared create state is invalid")
		}
		if value != "" && !validRepairStateID(value) {
			return fmt.Errorf("repair transaction created state is invalid")
		}
		return nil
	}
	if change.Prepared && strings.TrimSpace(change.PreviousStateID) == "" {
		return fmt.Errorf("repair transaction prepared state is invalid")
	}
	previous := filepath.Clean(change.PreviousPath)
	if filepath.Dir(previous) == filepath.Dir(target) && strings.HasPrefix(filepath.Base(previous), filepath.Base(target)+".reasonix-") {
		return nil
	}
	if config.MemoryUserDir() == "" {
		return fmt.Errorf("repair transaction state directory is unavailable")
	}
	restoreRoot := filepath.Join(config.MemoryUserDir(), "repair", "restore-backups")
	if repairNodeInsideResolvedRoot(restoreRoot, previous) {
		return nil
	}
	return fmt.Errorf("repair transaction previous path is invalid")
}

// repairNodeInsideResolvedRoot follows directory symlinks but deliberately
// leaves the leaf unresolved. A backed-up config may itself be a symlink whose
// referent is outside the repair directory; cleanup owns that link node, not
// its target. A symlinked parent, however, would let a forged transaction move
// and delete an unrelated node outside the repair root.
func repairNodeInsideResolvedRoot(root, path string) bool {
	root = filepath.Clean(strings.TrimSpace(root))
	path = filepath.Clean(strings.TrimSpace(path))
	if root == "" || path == "" || root == "." || path == "." {
		return false
	}
	rootInfo, err := os.Lstat(root)
	if err != nil || !rootInfo.IsDir() {
		return false
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return false
	}
	resolvedParent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return false
	}
	resolvedPath := filepath.Join(resolvedParent, filepath.Base(path))
	rel, err := filepath.Rel(resolvedRoot, resolvedPath)
	return err == nil && rel != "." && rel != ".." &&
		!strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

var (
	readRepairPreviousFile = os.ReadFile
	readRepairPreviousLink = os.Readlink
)

// UndoLastRepair restores the exact files moved aside by the latest repair. Any
// currently repaired file is retained as a timestamped redo candidate.
func UndoLastRepair() (*RepairTransaction, error) {
	invocationLastState := repairPlanReleaseNodeState(repairTransactionPath())
	invocationPendingState := repairPlanReleaseNodeState(pendingRepairTransactionPath())
	unlockTransaction, err := lockRepairTransaction()
	if err != nil {
		return nil, err
	}
	defer unlockTransaction()
	if repairPlanReleaseNodeState(repairTransactionPath()) != invocationLastState ||
		repairPlanReleaseNodeState(pendingRepairTransactionPath()) != invocationPendingState {
		return nil, fmt.Errorf("undo repair: repair transaction changed while waiting")
	}
	if err := reconcilePreparedRepairTransaction(); err != nil {
		return nil, fmt.Errorf("undo repair: reconcile pending mutation: %w", err)
	}
	tx, err := ReadLastRepair()
	if err != nil {
		return nil, err
	}
	if tx.Undone {
		return nil, fmt.Errorf("repair %s was already undone", tx.ID)
	}
	if err := verifyUndoRepairBackups(tx); err != nil {
		return nil, err
	}
	invocationID := repairPlanStateID(tx)
	targets := make([]string, 0, len(tx.Changes))
	for _, change := range tx.Changes {
		targets = append(targets, change.TargetPath)
	}
	unlockTargets, err := lockRepairMutations(targets...)
	if err != nil {
		return nil, err
	}
	defer unlockTargets()
	current, err := ReadLastRepair()
	if err != nil {
		return nil, fmt.Errorf("undo repair: re-read last repair transaction: %w", err)
	}
	if repairPlanStateID(current) != invocationID {
		return nil, fmt.Errorf("undo repair: last repair transaction changed while waiting")
	}
	tx = current
	if err := verifyUndoRepairBackups(tx); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	// markUndone persists per-change progress so a failure partway through a
	// multi-change undo leaves a transaction the next undo can resume.
	markUndone := func(i int) error {
		tx.Changes[i].Undone = true
		return persistRepairTransaction(tx)
	}
	for i, v := range slices.Backward(tx.Changes) {
		change := v
		if change.Undone {
			// Progress was persisted but the backup removal may have been cut
			// short by a crash; finish the cleanup the completed step owed.
			if !change.RemoveOnUndo && change.PreviousPath != "" {
				_ = removeRepairNodeIfMatching(change.PreviousPath, change.TargetPath, change.PreviousStateID)
			}
			continue
		}
		if change.Prepared {
			applied, resolveErr := preparedRepairChangeApplied(change)
			if resolveErr != nil {
				return nil, fmt.Errorf("undo repair: resolve prepared change: %w", resolveErr)
			}
			if !applied {
				if err := markUndone(i); err != nil {
					return nil, err
				}
				continue
			}
		}
		previousStateID := strings.TrimSpace(change.PreviousStateID)
		redo := ""
		if change.RemoveOnUndo && strings.TrimSpace(change.CreatedStateID) != "" {
			info, statErr := os.Lstat(change.TargetPath)
			if os.IsNotExist(statErr) {
				if err := markUndone(i); err != nil {
					return nil, err
				}
				continue
			}
			if statErr != nil {
				return nil, fmt.Errorf("undo repair: inspect created file: %w", statErr)
			}
			owned := info.Mode().IsRegular() &&
				verifyRepairPlanReleaseNodeStateFor(
					change.TargetPath,
					change.TargetPath,
					change.CreatedStateID,
				) == nil
			if !owned {
				// A failed/crashed create intent may be followed by an
				// uncooperative writer. It is not this repair's file, so leave
				// it live while allowing earlier plan changes to be undone.
				if err := markUndone(i); err != nil {
					return nil, err
				}
				continue
			}
		}
		// Lstat so a dangling symlink at the target is still moved aside
		// instead of being clobbered by the restore below.
		if _, err := os.Lstat(change.TargetPath); err == nil {
			// Index suffix keeps redo names unique when one undo touches the
			// same target twice (e.g. quarantine + snapshot restore): a shared
			// name would silently overwrite the earlier redo copy.
			redo = fmt.Sprintf("%s.reasonix-redo-%s-%d", change.TargetPath, now.Format("20060102T150405.000000000Z"), i)
			if err := renameRepairNodeNoReplace(change.TargetPath, redo); err != nil {
				return nil, fmt.Errorf("undo repair: retain current file: %w", err)
			}
			repairMutationAfterRename(change.TargetPath)
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
			if expected := previousStateID; expected != "" {
				if err := verifyRepairPlanReleaseNodeStateFor(change.PreviousPath, change.TargetPath, expected); err != nil {
					return fmt.Errorf("previous state changed: %w", err)
				}
			}
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
				linkTarget, err := readRepairPreviousLink(change.PreviousPath)
				if err != nil {
					return err
				}
				linkContent, linkContentErr := readRepairPreviousFile(change.PreviousPath)
				exactStateID := repairPlanReadStateIDFor(
					change.TargetPath,
					info.Mode(),
					"symlink",
					linkTarget,
					linkContent,
					linkContentErr == nil,
				)
				if previousStateID != "" && exactStateID != previousStateID {
					return fmt.Errorf("previous link read changed since it was verified")
				}
				if expected := previousStateID; expected != "" {
					if err := verifyRepairPlanReleaseNodeStateFor(change.PreviousPath, change.TargetPath, expected); err != nil {
						return fmt.Errorf("previous state changed while reading link: %w", err)
					}
				}
				return os.Symlink(linkTarget, change.TargetPath)
			}
			b, err := readRepairPreviousFile(change.PreviousPath)
			if err != nil {
				return err
			}
			exactStateID := repairPlanReadStateIDFor(
				change.TargetPath,
				info.Mode(),
				"file",
				"",
				b,
				true,
			)
			if previousStateID != "" && exactStateID != previousStateID {
				return fmt.Errorf("previous file bytes changed since they were verified")
			}
			if expected := previousStateID; expected != "" {
				if err := verifyRepairPlanReleaseNodeStateFor(change.PreviousPath, change.TargetPath, expected); err != nil {
					return fmt.Errorf("previous state changed while reading file: %w", err)
				}
			}
			return fileutil.AtomicCreateFile(change.TargetPath, b, info.Mode().Perm())
		}()
		if restoreErr != nil {
			if redo != "" {
				if compensateErr := restoreRepairNodeIfAbsent(redo, change.TargetPath); compensateErr != nil {
					return nil, fmt.Errorf("undo repair: restore %s: %w (current state retained at %s: %w)", change.TargetPath, restoreErr, redo, compensateErr)
				}
			}
			return nil, fmt.Errorf("undo repair: restore %s: %w", change.TargetPath, restoreErr)
		}
		if err := markUndone(i); err != nil {
			return nil, err
		}
		_ = removeRepairNodeIfMatching(change.PreviousPath, change.TargetPath, previousStateID)
	}
	tx.Undone = true
	tx.UndoneAt = now.Format(time.RFC3339Nano)
	if err := saveRepairTransaction(tx); err != nil {
		return nil, err
	}
	return tx, nil
}

func verifyUndoRepairBackups(tx *RepairTransaction) error {
	if tx == nil {
		return fmt.Errorf("undo repair: transaction identity is incomplete")
	}
	for _, change := range tx.Changes {
		if change.RemoveOnUndo {
			continue
		}
		expected := strings.TrimSpace(change.PreviousStateID)
		if expected == "" {
			return fmt.Errorf("undo repair: previous state identity is missing; legacy transaction cannot be undone safely")
		}
		if change.Undone {
			continue
		}
		if change.Prepared {
			applied, err := preparedRepairChangeApplied(change)
			if err != nil {
				return fmt.Errorf("undo repair: resolve prepared change: %w", err)
			}
			if !applied {
				continue
			}
		}
		// Lstat: a quarantined symlink counts as present even when its link
		// target is gone. Undo restores the link node, not its referent.
		if _, err := os.Lstat(change.PreviousPath); err != nil {
			return fmt.Errorf("undo repair: previous file %s: %w", change.PreviousPath, err)
		}
		if err := verifyRepairPlanReleaseNodeStateFor(change.PreviousPath, change.TargetPath, expected); err != nil {
			return fmt.Errorf("undo repair: previous state changed: %w", err)
		}
	}
	return nil
}

func preparedRepairChangeApplied(change RepairChange) (bool, error) {
	if !change.Prepared {
		return true, nil
	}
	if change.RemoveOnUndo {
		if _, err := os.Lstat(change.TargetPath); err == nil {
			// The target was absent when the intent was prepared. Any node now
			// present makes this the newest repair transaction, but undo removes
			// it only when CreatedStateID proves the repair owns the exact node.
			return true, nil
		} else if os.IsNotExist(err) {
			return false, nil
		} else {
			return false, err
		}
	}
	expected := strings.TrimSpace(change.PreviousStateID)
	if expected == "" {
		return false, fmt.Errorf("prepared previous state identity is missing")
	}
	targetExists := true
	if _, err := os.Lstat(change.TargetPath); err != nil {
		if !os.IsNotExist(err) {
			return false, err
		}
		targetExists = false
	}
	if targetExists && verifyRepairPlanReleaseNodeStateFor(change.TargetPath, change.TargetPath, expected) == nil {
		// The exact source is still live, so the no-replace rename did not
		// commit. Ignore an unrelated/colliding node at PreviousPath.
		return false, nil
	}
	if _, err := os.Lstat(change.PreviousPath); err == nil {
		if err := verifyRepairPlanReleaseNodeStateFor(change.PreviousPath, change.TargetPath, expected); err != nil {
			return false, fmt.Errorf("prepared previous state changed: %w", err)
		}
		return true, nil
	} else if !os.IsNotExist(err) {
		return false, err
	}
	return false, fmt.Errorf("prepared state is not present at its target or previous path")
}
