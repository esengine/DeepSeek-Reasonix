package agent

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"reasonix/internal/fileutil"
	"reasonix/internal/store"
)

// Recovery-branch garbage collection. Conflict recovery forks a copy of the
// in-memory transcript whenever a save conflicts (#5993); the triggers are
// fixed, but every fork that ever happened still sits in the session list
// until the user trashes it by hand. Most of them preserve nothing: the
// original session went on to contain everything the fork saved. Those — and
// only those — are safe to reclaim automatically.

// RecoveryGCGracePeriod is how long a reclaimable recovery branch must sit
// idle before GC may collect it. A fresh fork is part of an active conflict
// flow — the user may be comparing it against the original right now.
const RecoveryGCGracePeriod = 24 * time.Hour

const (
	recoveryTrashDir             = ".trash"
	recoveryTrashMetaFile        = ".trash-meta.json"
	recoveryTrashOperationPrefix = "recovery-trash:"
	recoveryTrashPendingFile     = ".recovery-trash-pending.json"
	recoveryTrashStagingPrefix   = ".recovery-trash-staging-"
)

// ErrRecoveryBranchNotCovered means the branch cannot currently be proven
// redundant with its parent. Destructive callers must preserve it.
var ErrRecoveryBranchNotCovered = errors.New("recovery branch is not covered by its parent")

// ErrRecoveryBranchNotIdle means the branch has not yet passed the safety
// grace period. It remains visible and may be retried by a later GC pass.
var ErrRecoveryBranchNotIdle = errors.New("recovery branch is still inside its safety grace period")

type recoveryTrashMeta struct {
	Key       string `json:"key"`
	DeletedAt int64  `json:"deletedAt"`
}

type recoveryTrashPendingMeta struct {
	Key string `json:"key"`
}

// SessionLeaseHeld reports whether ANY live runtime — this process included —
// holds the session's write lease. SessionLeaseHeldByOtherRuntime deliberately
// answers false for the current process; GC needs the stricter question, since
// a branch open in one of our own tabs is just as much in use.
func SessionLeaseHeld(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	if _, ok := sessionLeaseOwners.Load(canonicalSessionSavePath(path)); ok {
		return true
	}
	return SessionLeaseHeldByOtherRuntime(path)
}

// RecoveryBranchCoveredByParent reports whether a conflict-recovery branch
// preserves no content that is absent from its parent. It deliberately reads
// both transcripts instead of trusting listing sidecars: stale metadata must
// never authorize hiding, migration skipping, bulk trash, or permanent purge.
// Missing/corrupt metadata, a changed branch, or a missing/diverged parent are
// all treated conservatively as not covered.
func RecoveryBranchCoveredByParent(path, parentDir string) bool {
	meta, ok, err := LoadBranchMeta(path)
	if err != nil || !ok || !meta.Recovered || strings.TrimSpace(meta.RecoveryDigest) == "" {
		return false
	}
	return recoveryBranchCoveredByParent(path, parentDir, meta)
}

// TryAcquireRecoveryParentGuard verifies that a recovery branch is covered by
// its parent while holding the parent's save and lease locks. The caller must
// keep the returned guard until permanent deletion finishes, then Release it.
// If the parent is open or being rewritten, acquisition fails without waiting
// so bulk cleanup preserves the branch and can be retried later.
func TryAcquireRecoveryParentGuard(path, parentDir string) (*SessionRemovalGuard, error) {
	meta, ok, err := LoadBranchMeta(path)
	if err != nil || !ok || !meta.Recovered || strings.TrimSpace(meta.RecoveryDigest) == "" {
		return nil, ErrRecoveryBranchNotCovered
	}
	parentID := strings.TrimSpace(meta.ParentID)
	if parentID == "" {
		return nil, ErrRecoveryBranchNotCovered
	}
	parentDir = strings.TrimSpace(parentDir)
	if parentDir == "" {
		parentDir = filepath.Dir(path)
	}
	parentPath := filepath.Join(parentDir, parentID+".jsonl")
	if parentPath == path || !IsVisibleSession(parentPath) {
		return nil, ErrRecoveryBranchNotCovered
	}
	guard, err := TryAcquireSessionRemovalGuard(parentPath)
	if err != nil {
		return nil, err
	}
	if !recoveryBranchCoveredByParent(path, parentDir, meta) {
		guard.Release()
		return nil, ErrRecoveryBranchNotCovered
	}
	return guard, nil
}

func recoveryBranchCoveredByParent(path, parentDir string, meta BranchMeta) bool {
	parentID := strings.TrimSpace(meta.ParentID)
	if parentID == "" {
		return false
	}
	branch, err := LoadSession(path)
	if err != nil || branch == nil {
		return false
	}
	branchMsgs := branch.Snapshot()
	branchDigest, err := digestSessionMessages(branchMsgs)
	if err != nil || digestString(branchDigest) != strings.TrimSpace(meta.RecoveryDigest) {
		// Continued on (or undigestable): this is someone's conversation now.
		return false
	}
	parentDir = strings.TrimSpace(parentDir)
	if parentDir == "" {
		parentDir = filepath.Dir(path)
	}
	parentPath := filepath.Join(parentDir, parentID+".jsonl")
	if parentPath == path || !IsVisibleSession(parentPath) {
		return false
	}
	parent, err := LoadSession(parentPath)
	if err != nil || parent == nil {
		return false
	}
	parentMsgs := parent.Snapshot()
	parentDigest, err := digestSessionMessages(parentMsgs)
	if err != nil {
		return false
	}
	return bytes.Equal(parentDigest[:], branchDigest[:]) ||
		messagesHavePrefix(parentMsgs, branchMsgs) ||
		messagesHavePrefixWithCompatibleSystem(parentMsgs, branchMsgs)
}

// ReclaimableRecoveryBranches scans dir for conflict-recovery branches that
// are safe to dispose of. Every condition must hold — when in doubt the branch
// stays, because a recovery branch exists precisely to prevent data loss:
//
//  1. The branch meta says Recovered and records the fork digest.
//  2. The transcript still matches that fork digest: the branch was never
//     continued on. A single follow-up turn disqualifies it permanently.
//  3. The parent transcript (meta.ParentID, same directory) exists and covers
//     the branch content — equal digest, or the branch is a strict prefix
//     (allowing a compatible leading-system swap). These are the same checks
//     SaveRecoveryBranch uses to declare a recovery not needed in the first
//     place, so "covered" here means the fork preserves nothing unique.
//  4. No live runtime holds the branch's session lease.
//  5. The branch has been idle for at least grace.
//
// It returns candidate paths only; disposal (trash, delete) is caller policy.
func ReclaimableRecoveryBranches(dir string, now time.Time, grace time.Duration) ([]string, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") || strings.HasSuffix(e.Name(), ".events.jsonl") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		if !IsVisibleSession(path) {
			continue
		}
		meta, ok, err := LoadBranchMeta(path)
		if err != nil || !ok || !meta.Recovered || strings.TrimSpace(meta.RecoveryDigest) == "" {
			continue
		}
		if strings.TrimSpace(meta.ParentID) == "" {
			continue
		}
		if !recoveryBranchIdle(path, meta, now, grace) {
			continue
		}
		if SessionLeaseHeld(path) {
			continue
		}
		if !recoveryBranchCoveredByParent(path, dir, meta) {
			continue
		}
		out = append(out, path)
	}
	return out, nil
}

// TrashCoveredRecoveryBranch moves a redundant recovery branch into the same
// recoverable .trash layout used by Desktop. This is the explicit/manual cleanup
// path, so it does not require the background GC idle grace period. Parent
// coverage is rechecked while both parent and branch removal guards are held.
func TrashCoveredRecoveryBranch(path, parentDir string) error {
	return trashCoveredRecoveryBranch(path, parentDir, false)
}

// TrashReclaimableRecoveryBranch is the background-GC variant. In addition to
// the same atomic coverage proof, it requires the branch to remain idle for the
// full grace period.
func TrashReclaimableRecoveryBranch(path, parentDir string) error {
	return trashCoveredRecoveryBranch(path, parentDir, true)
}

func trashCoveredRecoveryBranch(path, parentDir string, requireIdle bool) error {
	path = filepath.Clean(strings.TrimSpace(path))
	parentDir = filepath.Clean(strings.TrimSpace(parentDir))
	if path == "." || parentDir == "." || filepath.Dir(path) != parentDir {
		return fmt.Errorf("recovery branch must be a direct child of its session directory")
	}
	key := filepath.Base(path)
	if !strings.HasSuffix(key, ".jsonl") || strings.HasSuffix(key, ".events.jsonl") {
		return fmt.Errorf("invalid recovery session path")
	}

	parentGuard, err := TryAcquireRecoveryParentGuard(path, parentDir)
	if err != nil {
		return err
	}
	defer parentGuard.Release()

	branchGuard, err := TryAcquireSessionRemovalGuard(path)
	if err != nil {
		return err
	}
	defer branchGuard.Release()
	if requireIdle {
		meta, ok, err := LoadBranchMeta(path)
		if err != nil || !ok || !recoveryBranchIdle(path, meta, time.Now(), RecoveryGCGracePeriod) {
			return ErrRecoveryBranchNotIdle
		}
	}
	if !RecoveryBranchCoveredByParent(path, parentDir) {
		return ErrRecoveryBranchNotCovered
	}

	stageDir, err := reserveRecoveryTrashStage(parentDir)
	if err != nil {
		return err
	}
	// Keep the move invisible until every artifact is staged. Older Reasonix
	// versions ignore the non-session staging directory, while new versions can
	// finish it from the durable in-directory marker after a crash. Publishing is
	// one same-filesystem rename, so Desktop can never restore or purge a split
	// transcript/sidecar set.
	if err := prepareRecoveryTrashStage(path, key, stageDir); err != nil {
		return err
	}
	return finishRecoveryTrashStage(parentDir, path, key, stageDir, branchGuard)
}

func recoveryBranchIdle(path string, meta BranchMeta, now time.Time, grace time.Duration) bool {
	idleSince := meta.UpdatedAt
	if idleSince.IsZero() {
		info, err := os.Stat(path)
		if err != nil {
			return false
		}
		idleSince = info.ModTime()
	}
	return now.Sub(idleSince) >= grace
}

// reconcileRecoveryTrashPending completes an interrupted move left by the
// pre-staging recovery-trash protocol. Keep this compatibility path so users
// upgrading from an intermediate build do not strand its typed marker.
func reconcileRecoveryTrashPending(item CleanupPendingInfo) (bool, error) {
	operation := strings.TrimSpace(item.Meta.Operation)
	if !strings.HasPrefix(operation, recoveryTrashOperationPrefix) {
		return false, nil
	}
	itemName := strings.TrimPrefix(operation, recoveryTrashOperationPrefix)
	if itemName == "" || filepath.Base(itemName) != itemName || itemName == "." || itemName == ".." {
		return true, fmt.Errorf("invalid recovery trash target")
	}
	path := filepath.Clean(item.SessionPath)
	dir := filepath.Dir(path)
	key := filepath.Base(path)
	guard, err := TryAcquireSessionRemovalGuard(path)
	if err != nil {
		return true, err
	}
	defer guard.Release()
	return true, finishRecoveryTrashMove(dir, path, key, filepath.Join(dir, recoveryTrashDir, itemName), guard)
}

// reconcileRecoveryTrashStages completes the atomic staging protocol used by
// new runtimes. A staging directory is intentionally not a valid Desktop trash
// item: it has neither a session-shaped directory name nor .trash-meta.json.
// Once complete, the whole directory is renamed into place atomically.
func reconcileRecoveryTrashStages(dir string) error {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil
	}
	root := filepath.Join(dir, recoveryTrashDir)
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var errs []error
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		itemDir := filepath.Join(root, entry.Name())
		if _, err := os.Stat(filepath.Join(itemDir, recoveryTrashPendingFile)); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			errs = append(errs, fmt.Errorf("inspect recovery trash stage %s: %w", itemDir, err))
			continue
		}
		if err := reconcileRecoveryTrashStage(dir, itemDir); err != nil {
			errs = append(errs, fmt.Errorf("reconcile recovery trash stage %s: %w", itemDir, err))
		}
	}
	return errors.Join(errs...)
}

func reconcileRecoveryTrashStage(dir, itemDir string) error {
	pending, err := readRecoveryTrashPending(itemDir)
	if err != nil {
		return err
	}
	key := strings.TrimSpace(pending.Key)
	if !validRecoveryTrashKey(key) {
		return fmt.Errorf("invalid recovery trash key")
	}
	stagedPath := filepath.Join(itemDir, key)
	staged, err := regularRecoveryTrashPath(stagedPath)
	if err != nil {
		return err
	}

	// A non-staging name means the atomic directory rename already succeeded;
	// only visibility metadata/final marker cleanup may remain.
	if !strings.HasPrefix(filepath.Base(itemDir), recoveryTrashStagingPrefix) {
		if !staged {
			return fmt.Errorf("published recovery trash item is missing transcript")
		}
		if _, err := os.Stat(filepath.Join(itemDir, recoveryTrashMetaFile)); err == nil {
			return clearRecoveryTrashPending(itemDir)
		} else if !os.IsNotExist(err) {
			return err
		}
		if err := writeRecoveryTrashMetaExisting(itemDir, key); err != nil {
			if os.IsNotExist(err) {
				return nil // restored or purged after complete publication
			}
			return err
		}
		return clearRecoveryTrashPending(itemDir)
	}

	livePath := filepath.Join(dir, key)
	live, err := regularRecoveryTrashPath(livePath)
	if err != nil {
		return err
	}
	switch {
	case live && !staged:
		// The durable marker landed but the first rename did not. No session
		// artifact has moved, so discard the empty stage and let a later GC pass
		// revalidate coverage before trying again.
		return removeEmptyRecoveryTrashStage(itemDir)
	case live && staged:
		return fmt.Errorf("live and staged recovery transcripts both exist")
	case !live && !staged:
		return fmt.Errorf("recovery trash stage is missing transcript")
	}

	guard, err := TryAcquireSessionRemovalGuard(livePath)
	if err != nil {
		return err
	}
	defer guard.Release()
	return finishRecoveryTrashStage(dir, livePath, key, itemDir, guard)
}

func regularRecoveryTrashPath(path string) (bool, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("recovery trash path is not a regular file: %s", path)
	}
	return true, nil
}

func removeEmptyRecoveryTrashStage(itemDir string) error {
	entries, err := os.ReadDir(itemDir)
	if err != nil {
		return err
	}
	if len(entries) != 1 || entries[0].Name() != recoveryTrashPendingFile || entries[0].IsDir() {
		return fmt.Errorf("recovery trash stage contains artifacts without a transcript")
	}
	return os.RemoveAll(itemDir)
}

func reserveRecoveryTrashStage(dir string) (string, error) {
	root := filepath.Join(dir, recoveryTrashDir)
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}
	return os.MkdirTemp(root, recoveryTrashStagingPrefix)
}

func prepareRecoveryTrashStage(path, key, stageDir string) error {
	if err := writeRecoveryTrashPending(stageDir, key); err != nil {
		return err
	}
	return moveRecoveryTrashPath(path, filepath.Join(stageDir, key))
}

func finishRecoveryTrashStage(dir, path, key, stageDir string, guard *SessionRemovalGuard) error {
	if err := moveRecoveryTrashArtifacts(dir, path, stageDir); err != nil {
		return err
	}
	itemDir, err := publishRecoveryTrashStage(dir, key, stageDir)
	if err != nil {
		return err
	}
	if err := writeRecoveryTrashMetaExisting(itemDir, key); err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		// A complete entry may be restored or purged as soon as it is published.
		// In that case there is nothing left for this producer to finalize.
		return guard.RemoveSidecarsAndRelease()
	}
	if err := clearRecoveryTrashPending(itemDir); err != nil {
		return err
	}
	return guard.RemoveSidecarsAndRelease()
}

func publishRecoveryTrashStage(dir, key, stageDir string) (string, error) {
	root := filepath.Join(dir, recoveryTrashDir)
	stem := strings.TrimSuffix(key, filepath.Ext(key))
	stamp := time.Now().UTC().UnixMilli()
	for i := range 1000 {
		name := key
		if i > 0 {
			name = fmt.Sprintf("%s-recovery-%d-%d", stem, stamp, i)
		}
		itemDir := filepath.Join(root, name)
		if _, err := os.Lstat(itemDir); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return "", err
		}
		if err := os.Rename(stageDir, itemDir); err == nil {
			return itemDir, nil
		} else if _, statErr := os.Lstat(itemDir); statErr == nil {
			continue // another producer won this candidate
		} else if !os.IsNotExist(statErr) {
			return "", statErr
		} else {
			return "", err
		}
	}
	return "", fmt.Errorf("could not publish recovery trash target")
}

func writeRecoveryTrashPending(itemDir, key string) error {
	if !validRecoveryTrashKey(key) {
		return fmt.Errorf("invalid recovery trash key")
	}
	b, err := json.MarshalIndent(recoveryTrashPendingMeta{Key: key}, "", "  ")
	if err != nil {
		return err
	}
	return fileutil.AtomicWriteFileStrict(filepath.Join(itemDir, recoveryTrashPendingFile), b, 0o644)
}

func readRecoveryTrashPending(itemDir string) (recoveryTrashPendingMeta, error) {
	b, err := os.ReadFile(filepath.Join(itemDir, recoveryTrashPendingFile))
	if err != nil {
		return recoveryTrashPendingMeta{}, err
	}
	var meta recoveryTrashPendingMeta
	if err := json.Unmarshal(b, &meta); err != nil {
		return recoveryTrashPendingMeta{}, err
	}
	return meta, nil
}

func clearRecoveryTrashPending(itemDir string) error {
	err := os.Remove(filepath.Join(itemDir, recoveryTrashPendingFile))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func validRecoveryTrashKey(key string) bool {
	return key != "" && filepath.Base(key) == key && key != "." && key != ".." &&
		strings.HasSuffix(key, ".jsonl") && !strings.HasSuffix(key, ".events.jsonl")
}

func reserveRecoveryTrashItemDir(dir, key string) (string, string, error) {
	root := filepath.Join(dir, recoveryTrashDir)
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", "", err
	}
	stem := strings.TrimSuffix(key, filepath.Ext(key))
	for i := range 1000 {
		name := key
		if i > 0 {
			name = fmt.Sprintf("%s-recovery-%d-%d", stem, time.Now().UTC().UnixMilli(), i)
		}
		itemDir := filepath.Join(root, name)
		if err := os.Mkdir(itemDir, 0o755); err == nil {
			return name, itemDir, nil
		} else if !os.IsExist(err) {
			return "", "", err
		}
	}
	return "", "", fmt.Errorf("could not reserve recovery trash target")
}

func finishRecoveryTrashMove(dir, path, key, itemDir string, guard *SessionRemovalGuard) error {
	if err := os.MkdirAll(itemDir, 0o755); err != nil {
		return err
	}
	if err := moveRecoveryTrashArtifacts(dir, path, itemDir); err != nil {
		return err
	}
	if err := writeRecoveryTrashMeta(itemDir, key); err != nil {
		return err
	}
	// Keep the branch guard until the trash entry is complete and the hidden
	// marker is cleared. No runtime can bind the now-vacant live path in the
	// middle and inherit an incomplete cleanup state.
	if err := ClearCleanupPending(path); err != nil {
		return err
	}
	return guard.RemoveSidecarsAndRelease()
}

func moveRecoveryTrashArtifacts(dir, path, itemDir string) error {
	for _, src := range recoveryTrashSidecars(path) {
		if err := moveRecoveryTrashPath(src, filepath.Join(itemDir, filepath.Base(src))); err != nil {
			return err
		}
	}
	return moveRecoverySubagentArtifacts(dir, path, itemDir)
}

func prepareRecoveryTrashEntry(path, key, itemDir string) error {
	if err := writeRecoveryTrashMeta(itemDir, key); err != nil {
		return err
	}
	return moveRecoveryTrashPath(path, filepath.Join(itemDir, key))
}

func writeRecoveryTrashMeta(itemDir, key string) error {
	meta := recoveryTrashMeta{Key: key, DeletedAt: time.Now().UnixMilli()}
	b, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(itemDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(itemDir, recoveryTrashMetaFile), b, 0o644)
}

func writeRecoveryTrashMetaExisting(itemDir, key string) error {
	meta := recoveryTrashMeta{Key: key, DeletedAt: time.Now().UnixMilli()}
	b, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(itemDir, recoveryTrashMetaFile), b, 0o644)
}

func recoveryTrashSidecars(path string) []string {
	artifacts := append([]string(nil), store.SessionSidecarFiles(path)...)
	artifacts = append(artifacts,
		path+".telemetry.json",
		store.SessionCheckpointDir(path),
		store.SessionJobsDir(path),
		store.SessionInboxDir(path),
	)
	return artifacts
}

func moveRecoverySubagentArtifacts(dir, path, itemDir string) error {
	artifacts, err := ListSubagentsByParent(dir, BranchID(path))
	if err != nil {
		return err
	}
	targetDir := filepath.Join(itemDir, "subagents")
	for _, artifact := range artifacts {
		paths := []string{artifact.SessionPath, artifact.MetaPath}
		paths = append(paths, store.SessionSidecarFiles(artifact.SessionPath)...)
		for _, src := range paths {
			if err := moveRecoveryTrashPath(src, filepath.Join(targetDir, filepath.Base(src))); err != nil {
				return err
			}
		}
	}
	return nil
}

func moveRecoveryTrashPath(src, dst string) error {
	if strings.TrimSpace(src) == "" {
		return nil
	}
	if _, err := os.Lstat(src); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.Rename(src, dst)
}
