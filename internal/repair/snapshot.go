package repair

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/fileutil"
)

const configSnapshotRetention = 5

type ConfigSnapshot struct {
	SchemaVersion int    `json:"schemaVersion"`
	ID            string `json:"id"`
	Path          string `json:"path"`
	SHA256        string `json:"sha256"`
	SourcePath    string `json:"sourcePath"`
	RecordedAt    string `json:"recordedAt"`
	Version       string `json:"version,omitempty"`
}

func snapshotDir() string {
	if root := config.MemoryUserDir(); root != "" {
		return filepath.Join(root, "repair", "snapshots")
	}
	return ""
}

func recordConfigSnapshot(source string, b []byte, version string, now time.Time) error {
	dir := snapshotDir()
	if dir == "" {
		return nil
	}
	sum := sha256.Sum256(b)
	hash := hex.EncodeToString(sum[:])
	existing, err := ListConfigSnapshots()
	if err != nil {
		return err
	}
	if len(existing) > 0 && strings.EqualFold(existing[0].SHA256, hash) {
		current, err := readVerifiedConfigSnapshot(existing[0])
		if err != nil {
			return fmt.Errorf("verify existing config snapshot %s: %w", existing[0].ID, err)
		}
		if !bytes.Equal(current, b) {
			return fmt.Errorf("config snapshot %s SHA-256 collision", existing[0].ID)
		}
		return nil
	}
	stamp := now.UTC().Format("20060102T150405.000000000Z")
	id := stamp + "-" + hash[:12]
	path := filepath.Join(dir, id+".toml")
	if err := createOrVerifyConfigSnapshotFile(path, b); err != nil {
		return err
	}
	meta := ConfigSnapshot{SchemaVersion: 1, ID: id, Path: path, SHA256: hash, SourcePath: source, RecordedAt: now.UTC().Format(time.RFC3339Nano), Version: version}
	encoded, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	metadata := append(encoded, '\n')
	if err := createOrVerifyConfigSnapshotFile(path+".json", metadata); err != nil {
		return err
	}
	// Re-read both immutable files before pruning. This catches a conflicting
	// pre-existing ID as well as an uncooperative replacement during publish.
	if err := verifyConfigSnapshotFile(path, b); err != nil {
		return err
	}
	if err := verifyConfigSnapshotFile(path+".json", metadata); err != nil {
		return err
	}
	return pruneConfigSnapshots(configSnapshotRetention)
}

func createOrVerifyConfigSnapshotFile(path string, content []byte) error {
	if err := fileutil.AtomicCreateFile(path, content, 0o600); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return err
		}
		if err := verifyConfigSnapshotFile(path, content); err != nil {
			return fmt.Errorf("config snapshot path already exists with different state: %w", err)
		}
	}
	return nil
}

func verifyConfigSnapshotFile(path string, expected []byte) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", path)
	}
	actual, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !bytes.Equal(actual, expected) {
		return fmt.Errorf("%s content changed", path)
	}
	return nil
}

func ListConfigSnapshots() ([]ConfigSnapshot, error) {
	dir := snapshotDir()
	if dir == "" {
		return []ConfigSnapshot{}, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []ConfigSnapshot{}, nil
		}
		return nil, err
	}
	out := make([]ConfigSnapshot, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".toml.json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		var snap ConfigSnapshot
		if json.Unmarshal(b, &snap) != nil || validateConfigSnapshot(dir, &snap) != nil {
			continue
		}
		if entry.Name() != snap.ID+".toml.json" {
			continue
		}
		out = append(out, snap)
	}
	sort.Slice(out, func(i, j int) bool {
		left, leftErr := time.Parse(time.RFC3339Nano, out[i].RecordedAt)
		right, rightErr := time.Parse(time.RFC3339Nano, out[j].RecordedAt)
		if leftErr == nil && rightErr == nil && !left.Equal(right) {
			return left.After(right)
		}
		if out[i].RecordedAt != out[j].RecordedAt {
			return out[i].RecordedAt > out[j].RecordedAt
		}
		return out[i].ID > out[j].ID
	})
	return out, nil
}

func RestoreConfigSnapshot(id string) (*RepairTransaction, error) {
	dest := config.UserConfigPath()
	if dest == "" {
		return nil, fmt.Errorf("global config path is unavailable")
	}
	dir, contentPath, metadataPath, err := configSnapshotPaths(id)
	if err != nil {
		return nil, err
	}
	plan := RepairPlan{
		SchemaVersion: RepairPlanSchemaVersion,
		Summary:       "restore config snapshot",
		Actions: []RepairPlanAction{{
			Type:       "restore_snapshot",
			SnapshotID: id,
			Reason:     "direct restore",
		}},
	}
	preview, err := PreviewRepairPlan(plan, ApplyPlanOptions{})
	if err != nil {
		return nil, err
	}
	unlockTransaction, err := lockRepairTransaction()
	if err != nil {
		return nil, err
	}
	defer unlockTransaction()
	if err := reconcilePreparedRepairTransaction(); err != nil {
		return nil, fmt.Errorf("restore config snapshot: reconcile pending mutation: %w", err)
	}
	unlock, err := lockRepairMutations(dest, dir, contentPath, metadataPath)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if err := verifyRepairPlanFileStates(preview[0].fileStates); err != nil {
		return nil, err
	}
	return restoreConfigSnapshotBoundUnlocked(id, preview[0].fileStates, preview[0].afterContent, nil)
}

func restoreConfigSnapshotBoundUnlocked(
	id string,
	expectedStates map[string]string,
	confirmedSnapshot []byte,
	planTx *RepairTransaction,
) (*RepairTransaction, error) {
	if err := verifyRepairPlanFileStates(expectedStates); err != nil {
		return nil, err
	}
	snapshots, err := ListConfigSnapshots()
	if err != nil {
		return nil, err
	}
	var selected *ConfigSnapshot
	for i := range snapshots {
		if snapshots[i].ID == id {
			selected = &snapshots[i]
			break
		}
	}
	if selected == nil {
		return nil, fmt.Errorf("config snapshot %q not found", id)
	}
	var verifiedSnapshot []byte
	if expectedStates == nil {
		verifiedSnapshot, err = readVerifiedConfigSnapshot(*selected)
		if err != nil {
			return nil, err
		}
	} else {
		if err := verifyConfirmedConfigSnapshot(*selected, confirmedSnapshot); err != nil {
			return nil, err
		}
	}
	dest := config.UserConfigPath()
	if dest == "" {
		return nil, fmt.Errorf("global config path is unavailable")
	}
	if err := verifyRepairPlanFileStates(expectedStates); err != nil {
		return nil, err
	}
	b := confirmedSnapshot
	if expectedStates == nil {
		b = verifiedSnapshot
	}
	repairMutationBeforeRename(dest)
	if err := verifyRepairPlanFileStates(expectedStates); err != nil {
		return nil, err
	}
	tx := planTx
	if tx == nil {
		tx = newRepairTransaction(time.Now())
	}
	backup := filepath.Join(config.MemoryUserDir(), "repair", "restore-backups", tx.ID+".toml")
	if expectedStates != nil {
		backup = dest + ".reasonix-restore-" + tx.ID
	}
	moved := false
	if _, err := os.Lstat(dest); err == nil {
		// Move the live file aside instead of copying its bytes: dest may be a
		// symlink (a dotfiles-managed config), and a byte copy would record a
		// plain file, so undo could never restore the link. Rename preserves
		// the exact node; undo recreates symlinks from the quarantined link.
		if err := os.MkdirAll(filepath.Dir(backup), 0o700); err != nil {
			return nil, err
		}
		changeIndex := len(tx.Changes)
		tx.Changes = append(tx.Changes, preparedRepairChangeForPrevious("global", dest, backup))
		if err := persistPreparedRepairTransaction(tx); err != nil {
			return nil, fmt.Errorf("prepare snapshot restore: %w", err)
		}
		repairMutationAfterPrepare(dest)
		if renameErr := snapshotRename(dest, backup); renameErr == nil {
			moved = true
			repairMutationAfterRename(dest)
		} else if expectedStates != nil {
			return nil, fmt.Errorf("restore config snapshot: move confirmed config: %w", renameErr)
		} else {
			// The repair state directory can live on another filesystem. Fall
			// back to a unique sibling so displacement remains one atomic rename;
			// copy-then-remove could delete a concurrent recreation of dest.
			prepared := *tx
			prepared.Changes = append([]RepairChange(nil), tx.Changes...)
			if err := clearPreparedRepairTransaction(&prepared); err != nil {
				return nil, fmt.Errorf("clear cross-device snapshot restore intent: %w", err)
			}
			backup = dest + ".reasonix-restore-" + tx.ID
			tx.Changes[changeIndex].PreviousPath = backup
			if err := persistPreparedRepairTransaction(tx); err != nil {
				return nil, fmt.Errorf("prepare sibling snapshot restore: %w", err)
			}
			repairMutationAfterPrepare(dest)
			if fallbackErr := snapshotRename(dest, backup); fallbackErr != nil {
				return nil, errors.Join(
					fmt.Errorf("restore config snapshot: move config to repair state: %w", renameErr),
					fmt.Errorf("move config to sibling backup: %w", fallbackErr),
				)
			}
			moved = true
			repairMutationAfterRename(dest)
		}
		if moved {
			expected := tx.Changes[changeIndex].PreviousStateID
			if err := verifyRepairPlanReleaseNodeStateFor(backup, dest, expected); err != nil {
				return nil, joinRestoreCleanupError(err, backup, restoreRepairNodeIfAbsent(backup, dest))
			}
			if durable, err := commitPreparedRepairTransaction(tx, changeIndex); err != nil {
				if durable {
					return nil, fmt.Errorf("commit snapshot restore undo state: cleanup pending journal: %w", err)
				}
				return nil, joinRestoreCleanupError(err, backup, restoreRepairNodeIfAbsent(backup, dest))
			}
			if _, err := os.Lstat(dest); err == nil {
				return nil, fmt.Errorf("target was recreated during snapshot restore; original state remains at %s", backup)
			} else if !os.IsNotExist(err) {
				return nil, err
			}
		}
	} else if os.IsNotExist(err) {
		createdStateID := repairPlanPreparedCreateStateID(dest, b, 0o600)
		tx.Changes = append(tx.Changes, preparedRepairChangeForCreate("global", dest, createdStateID))
	} else {
		return nil, err
	}
	if !moved {
		changeIndex := len(tx.Changes) - 1
		if err := persistPreparedRepairTransaction(tx); err != nil {
			return nil, fmt.Errorf("prepare snapshot create: %w", err)
		}
		repairMutationAfterPrepare(dest)
		if err := fileutil.AtomicCreateFile(dest, b, 0o600); err != nil {
			prepared := *tx
			prepared.Changes = append([]RepairChange(nil), tx.Changes...)
			clearErr := clearPreparedRepairTransaction(&prepared)
			return nil, errors.Join(err, clearErr)
		}
		repairSnapshotAfterCreate(dest)
		if err := verifyPreparedCreateOwnership(tx, changeIndex, dest); err != nil {
			return nil, fmt.Errorf("verify snapshot create ownership: %w", err)
		}
		if err := verifyConfigSnapshotFile(dest, b); err != nil {
			return nil, fmt.Errorf("restored config changed during publish: %w", err)
		}
		if durable, err := commitPreparedRepairTransaction(tx, changeIndex); err != nil {
			if durable {
				return nil, fmt.Errorf("commit snapshot create undo state: cleanup pending journal: %w", err)
			}
			return nil, fmt.Errorf("commit snapshot create undo state: %w", err)
		}
		return tx, nil
	}
	if err := fileutil.AtomicCreateFile(dest, b, 0o600); err != nil {
		return nil, err
	}
	repairSnapshotAfterCreate(dest)
	if err := verifyConfigSnapshotFile(dest, b); err != nil {
		return nil, fmt.Errorf("restored config changed during publish: %w", err)
	}
	return tx, nil
}

var repairSnapshotAfterCreate = func(string) {}

func verifyConfirmedConfigSnapshot(snap ConfigSnapshot, content []byte) error {
	if err := config.ValidateBytes(content); err != nil {
		return fmt.Errorf("config snapshot %q is invalid: %w", snap.ID, err)
	}
	sum := sha256.Sum256(content)
	if !strings.EqualFold(hex.EncodeToString(sum[:]), snap.SHA256) {
		return fmt.Errorf("config snapshot %q hash mismatch", snap.ID)
	}
	return nil
}

// snapshotRename is an indirection over the no-replace move so tests can force
// cross-device fallback paths.
var snapshotRename = renameRepairNodeNoReplace

func joinRestoreCleanupError(err error, backup string, cleanupErr error) error {
	if cleanupErr == nil {
		return err
	}
	return errors.Join(err, fmt.Errorf("restore original config from %s: %w", backup, cleanupErr))
}

func validateConfigSnapshot(dir string, snap *ConfigSnapshot) error {
	if snap == nil || snap.SchemaVersion != 1 || snap.ID == "" || snap.Path == "" || snap.SHA256 == "" {
		return fmt.Errorf("snapshot metadata is incomplete")
	}
	if len(snap.SHA256) != sha256.Size*2 {
		return fmt.Errorf("snapshot SHA-256 is invalid")
	}
	if _, err := hex.DecodeString(snap.SHA256); err != nil {
		return fmt.Errorf("snapshot SHA-256 is invalid")
	}
	rel, err := filepath.Rel(filepath.Clean(dir), filepath.Clean(snap.Path))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("snapshot path is outside snapshot directory")
	}
	expectedPath := filepath.Join(filepath.Clean(dir), snap.ID+".toml")
	if filepath.Clean(snap.Path) != expectedPath {
		return fmt.Errorf("snapshot id does not match path")
	}
	return nil
}

func configSnapshotPaths(id string) (dir, contentPath, metadataPath string, err error) {
	id = strings.TrimSpace(id)
	if id == "" || id == "." || id == ".." || filepath.Base(id) != id || strings.ContainsAny(id, `/\`) {
		return "", "", "", fmt.Errorf("invalid config snapshot id %q", id)
	}
	dir = snapshotDir()
	if dir == "" {
		return "", "", "", fmt.Errorf("config snapshot directory is unavailable")
	}
	contentPath = filepath.Join(dir, id+".toml")
	metadataPath = contentPath + ".json"
	return dir, contentPath, metadataPath, nil
}

// readVerifiedConfigSnapshot validates the exact bytes it returns. Keeping
// validation and consumption in one read closes the verify-then-read window in
// direct (non-preview) snapshot restores.
func readVerifiedConfigSnapshot(snap ConfigSnapshot) ([]byte, error) {
	b, err := os.ReadFile(snap.Path)
	if err != nil {
		return nil, err
	}
	if err := config.ValidateBytes(b); err != nil {
		return nil, err
	}
	sum := sha256.Sum256(b)
	got := hex.EncodeToString(sum[:])
	if !strings.EqualFold(got, snap.SHA256) {
		return nil, fmt.Errorf("snapshot %s failed SHA-256 verification", snap.ID)
	}
	return b, nil
}

func pruneConfigSnapshots(keep int) error {
	snapshots, err := ListConfigSnapshots()
	if err != nil {
		return err
	}
	for _, snap := range snapshots[minimum(keep, len(snapshots)):] {
		if err := pruneConfigSnapshot(snap); err != nil {
			return err
		}
	}
	return nil
}

// configSnapshotPruneAfterMove is a test seam for an uncooperative writer that
// recreates or changes a snapshot path after prune atomically displaces it.
var configSnapshotPruneAfterMove = func(string, string, string) {}

func pruneConfigSnapshot(snap ConfigSnapshot) error {
	dir := snapshotDir()
	metaPath := snap.Path + ".json"
	expectedMeta, err := os.ReadFile(metaPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var current ConfigSnapshot
	if err := json.Unmarshal(expectedMeta, &current); err != nil || validateConfigSnapshot(dir, &current) != nil || current != snap {
		return fmt.Errorf("config snapshot %s metadata changed before prune", snap.ID)
	}

	metaCleanup, err := moveRepairNodeToUniqueCleanup(metaPath)
	if err != nil {
		return fmt.Errorf("prune config snapshot %s metadata: %w", snap.ID, err)
	}
	if metaCleanup == "" {
		return nil
	}
	configSnapshotPruneAfterMove("metadata", metaPath, metaCleanup)
	if err := verifyConfigSnapshotFile(metaCleanup, expectedMeta); err != nil {
		restoreErr := renameRepairNodeNoReplace(metaCleanup, metaPath)
		if restoreErr != nil {
			return errors.Join(err, fmt.Errorf("preserve changed snapshot metadata at %s: %w", metaCleanup, restoreErr))
		}
		return fmt.Errorf("config snapshot %s metadata changed during prune: %w", snap.ID, err)
	}
	if _, err := os.Lstat(metaPath); err == nil {
		return fmt.Errorf("config snapshot %s metadata was recreated during prune; displaced metadata remains at %s", snap.ID, metaCleanup)
	} else if !os.IsNotExist(err) {
		return errors.Join(err, fmt.Errorf("displaced snapshot metadata remains at %s", metaCleanup))
	}

	contentCleanup, err := moveRepairNodeToUniqueCleanup(snap.Path)
	if err != nil {
		restoreErr := renameRepairNodeNoReplace(metaCleanup, metaPath)
		if restoreErr != nil {
			return errors.Join(err, fmt.Errorf("restore snapshot metadata from %s: %w", metaCleanup, restoreErr))
		}
		return fmt.Errorf("prune config snapshot %s content: %w", snap.ID, err)
	}
	if contentCleanup == "" {
		if err := os.Remove(metaCleanup); err != nil {
			return fmt.Errorf("remove metadata for missing config snapshot %s: %w", snap.ID, err)
		}
		return nil
	}
	configSnapshotPruneAfterMove("content", snap.Path, contentCleanup)
	moved := snap
	moved.Path = contentCleanup
	if _, err := readVerifiedConfigSnapshot(moved); err != nil {
		contentRestoreErr := renameRepairNodeNoReplace(contentCleanup, snap.Path)
		if contentRestoreErr != nil {
			return errors.Join(err, fmt.Errorf("preserve changed snapshot content at %s: %w", contentCleanup, contentRestoreErr))
		}
		metaRestoreErr := renameRepairNodeNoReplace(metaCleanup, metaPath)
		if metaRestoreErr != nil {
			return errors.Join(err, fmt.Errorf("restore snapshot metadata from %s: %w", metaCleanup, metaRestoreErr))
		}
		return fmt.Errorf("config snapshot %s content changed during prune: %w", snap.ID, err)
	}
	if _, err := os.Lstat(metaPath); err == nil {
		return fmt.Errorf(
			"config snapshot %s metadata was recreated during content cleanup; displaced files remain at %s and %s",
			snap.ID,
			metaCleanup,
			contentCleanup,
		)
	} else if !os.IsNotExist(err) {
		return errors.Join(
			err,
			fmt.Errorf("displaced snapshot files remain at %s and %s", metaCleanup, contentCleanup),
		)
	}

	// Metadata is removed first. A crash or content-cleanup failure therefore
	// leaves only an ignored orphan content file, never a visible partial pair.
	if err := os.Remove(metaCleanup); err != nil {
		contentRestoreErr := renameRepairNodeNoReplace(contentCleanup, snap.Path)
		if contentRestoreErr != nil {
			return errors.Join(err, fmt.Errorf("restore snapshot content from %s: %w", contentCleanup, contentRestoreErr))
		}
		metaRestoreErr := renameRepairNodeNoReplace(metaCleanup, metaPath)
		if metaRestoreErr != nil {
			return errors.Join(err, fmt.Errorf("restore snapshot metadata from %s: %w", metaCleanup, metaRestoreErr))
		}
		return fmt.Errorf("remove config snapshot %s metadata: %w", snap.ID, err)
	}
	if err := os.Remove(contentCleanup); err != nil {
		return fmt.Errorf("remove config snapshot %s content: %w", snap.ID, err)
	}
	return nil
}

func minimum(a, b int) int {
	if a < b {
		return a
	}
	return b
}
