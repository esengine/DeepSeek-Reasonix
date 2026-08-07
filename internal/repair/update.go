package repair

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/filelock"
	"reasonix/internal/fileutil"
)

const updateTransactionVersion = 1
const pendingUpdateLockTimeout = 5 * time.Second

var repairExecutable = os.Executable
var updateBackupAfterQuarantine = func(string, string) {}

type UpdateTransaction struct {
	SchemaVersion int    `json:"schemaVersion"`
	FromVersion   string `json:"fromVersion,omitempty"`
	ToVersion     string `json:"toVersion"`
	Platform      string `json:"platform"`
	TargetKind    string `json:"targetKind"` // file | app-bundle
	TargetPath    string `json:"targetPath"`
	BackupPath    string `json:"backupPath"`
	BackupSHA256  string `json:"backupSha256,omitempty"`
	// Files lists every binary of the release unit the update replaces
	// (main executable first, then Guard/launcher siblings). Rollback must
	// restore all of them together: restoring only the main binary would
	// leave a mixed old-desktop/new-Guard install. Empty on transactions
	// recorded by kinds that back up a single unit (macOS app bundles).
	Files     []UpdateTransactionFile `json:"files,omitempty"`
	CreatedAt string                  `json:"createdAt"`
	// Handoff fields authorize the detached macOS updater to act on paths
	// recorded by the live desktop process. They are optional so pending
	// transactions written by older releases remain readable.
	HandoffAppPath       string `json:"handoffAppPath,omitempty"`
	HandoffStagingPath   string `json:"handoffStagingPath,omitempty"`
	HandoffAppTreeID     string `json:"handoffAppTreeId,omitempty"`
	HandoffStagingTreeID string `json:"handoffStagingTreeId,omitempty"`
	HandoffOwnerPID      int    `json:"handoffOwnerPid,omitempty"`
	// BackupTreeID binds a macOS rollback backup to the bundle captured before
	// the update. It remains optional for legacy transactions.
	BackupTreeID string `json:"backupTreeId,omitempty"`
	// OrphanedBackupPath and OrphanedBackupTreeID bind a quarantined backup to
	// the transaction that displaced it. Terminal transaction cleanup removes
	// only this exact tree after re-verifying its digest; older transactions
	// without these optional fields retain their existing behavior.
	OrphanedBackupPath   string `json:"orphanedBackupPath,omitempty"`
	OrphanedBackupTreeID string `json:"orphanedBackupTreeId,omitempty"`
}

type UpdateTransactionFile struct {
	TargetPath       string `json:"targetPath"`
	BackupPath       string `json:"backupPath,omitempty"`
	SHA256           string `json:"sha256,omitempty"`
	InstalledStateID string `json:"installedStateId,omitempty"`
	MissingBefore    bool   `json:"missingBefore,omitempty"`
}

type installedFileUpdateState struct {
	SchemaVersion       int      `json:"schemaVersion"`
	UpdateTransactionID string   `json:"updateTransactionId"`
	InstalledStateIDs   []string `json:"installedStateIds"`
}

// FileUpdateInstallReceipt binds one published release-unit member to the exact
// transaction, target, node type, mode, and bytes that were staged and verified.
// RecordClaimedFileUpdateInstalled accepts only these receipts, so a replacement
// that appears after publish verification cannot be adopted by the transaction.
type FileUpdateInstallReceipt struct {
	UpdateTransactionID string
	TargetPath          string
	InstalledStateID    string
}

type UpdateRollbackResult struct {
	RolledBack  bool   `json:"rolledBack"`
	FromVersion string `json:"fromVersion,omitempty"`
	ToVersion   string `json:"toVersion,omitempty"`
	TargetPath  string `json:"targetPath,omitempty"`
	// MixedInstall reports that a failed rollback could not be compensated:
	// the install now mixes binaries from two releases. Launchers must not
	// start the desktop in this state.
	MixedInstall bool `json:"mixedInstall,omitempty"`
}

// ErrPendingUpdateAwaitingHealth reports that the currently running release is
// still the probationary target of a prior update. Callers must not cancel or
// roll it back merely to start another update; the normal startup health
// confirmation owns that transition.
var ErrPendingUpdateAwaitingHealth = errors.New("previous update is awaiting startup health confirmation")

var errPendingUpdateForeignInstall = errors.New("pending update belongs to a different installation")

// pendingUpdateHealthStaleAfter bounds how long Reconcile waits for startup
// health before auto-committing a still-running probationary target.
var pendingUpdateHealthStaleAfter = 24 * time.Hour

// PendingUpdateReconcileResult describes the safe transition performed before
// startup or a new install. Cleared = pre-publish cancel; RolledBack = verified
// restore; Healthy = probationary target committed after install evidence.
type PendingUpdateReconcileResult struct {
	Pending        bool   `json:"pending"`
	Cleared        bool   `json:"cleared,omitempty"`
	RolledBack     bool   `json:"rolledBack,omitempty"`
	MixedInstall   bool   `json:"mixedInstall,omitempty"`
	AwaitingHealth bool   `json:"awaitingHealth,omitempty"`
	Healthy        bool   `json:"healthy,omitempty"`
	FromVersion    string `json:"fromVersion,omitempty"`
	ToVersion      string `json:"toVersion,omitempty"`
	TargetPath     string `json:"targetPath,omitempty"`
}

// UpdateVersionsEqual reports whether two release version strings name the same
// release, normalizing an optional leading "v"/"V" prefix.
func UpdateVersionsEqual(a, b string) bool {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	return normalizeUpdateVersion(a) == normalizeUpdateVersion(b)
}

func normalizeUpdateVersion(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if !strings.HasPrefix(v, "v") && !strings.HasPrefix(v, "V") {
		return "v" + v
	}
	return "v" + strings.TrimPrefix(strings.TrimPrefix(v, "v"), "V")
}

// pendingUpdateHealthIsStaleOverride forces the stale decision in tests without
// rewriting CreatedAt (part of transaction identity).
var pendingUpdateHealthIsStaleOverride func(*UpdateTransaction) bool

func pendingUpdateHealthIsStale(tx *UpdateTransaction) bool {
	if tx == nil {
		return false
	}
	if pendingUpdateHealthIsStaleOverride != nil {
		return pendingUpdateHealthIsStaleOverride(tx)
	}
	if pendingUpdateHealthStaleAfter <= 0 {
		return false
	}
	created, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(tx.CreatedAt))
	if err != nil {
		created, err = time.Parse(time.RFC3339, strings.TrimSpace(tx.CreatedAt))
	}
	if err != nil {
		return false
	}
	return time.Since(created) >= pendingUpdateHealthStaleAfter
}

// UpdateTransactionID returns a stable, opaque identity for the complete
// transaction. Platform handoff processes use it so copied scalar fields such
// as version and creation time cannot authorize a rewritten pending update.
func UpdateTransactionID(tx *UpdateTransaction) string {
	if tx == nil {
		return ""
	}
	return repairPlanStateID(tx)
}

func PendingUpdatePath() string {
	root := config.MemoryUserDir()
	if root == "" {
		return ""
	}
	return filepath.Join(root, "repair", "pending-update.json")
}

// lockPendingUpdateStrict serializes cross-process pending-update transitions:
// prepare, rollback, commit, and cancel. Two launchers can run recovery at
// once — a failed update makes startup slow, so a double-clicked Guard is
// realistic — and restoreReleaseUnit's fixed staging/aside paths assume a
// single restorer; unserialized, the loser's compensation can re-install the
// new binaries over the winner's completed rollback.
func lockPendingUpdateStrict() (func(), error) {
	path := PendingUpdatePath()
	if path == "" {
		return nil, fmt.Errorf("pending update: Reasonix state directory is unavailable")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), pendingUpdateLockTimeout)
	defer cancel()
	unlock, err := filelock.Acquire(ctx, path+".lock")
	if err != nil {
		return nil, err
	}
	return unlock, nil
}

var acquirePendingUpdateLock = lockPendingUpdateStrict

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

// PrepareAppBundleUpdateHandoff records every path the detached macOS updater
// may mutate. The child receives only the transaction identity and must claim
// these recorded paths under the pending-update and mutation locks.
func PrepareAppBundleUpdateHandoff(fromVersion, toVersion, appPath, backupPath, stagedAppPath, stagingPath string, ownerPID int) (*UpdateTransaction, error) {
	tx, err := newAppBundleUpdateTransaction(fromVersion, toVersion, appPath, backupPath)
	if err != nil {
		return nil, err
	}
	if !filepath.IsAbs(tx.TargetPath) {
		return nil, fmt.Errorf("prepare update: invalid macOS bundle paths")
	}
	tx.HandoffAppPath = filepath.Clean(strings.TrimSpace(stagedAppPath))
	tx.HandoffStagingPath = filepath.Clean(strings.TrimSpace(stagingPath))
	tx.HandoffOwnerPID = ownerPID
	if err := validateAppBundleHandoffMetadata(tx); err != nil {
		return nil, fmt.Errorf("prepare update: %w", err)
	}

	unlock, err := acquirePendingUpdateLock()
	if err != nil {
		return nil, fmt.Errorf("prepare update: lock pending transaction: %w", err)
	}
	defer unlock()
	if err := ensureNoPendingUpdate(); err != nil {
		return nil, err
	}
	unlockTargets, err := lockRepairMutations(tx.TargetPath, tx.BackupPath)
	if err != nil {
		return nil, fmt.Errorf("prepare update: lock targets: %w", err)
	}
	defer unlockTargets()
	tx.HandoffAppTreeID, err = repairPlanTreeContentStateID(tx.HandoffAppPath)
	if err != nil {
		return nil, fmt.Errorf("prepare update: stage bundle digest: %w", err)
	}
	tx.HandoffStagingTreeID, err = repairPlanTreeContentStateID(tx.HandoffStagingPath)
	if err != nil {
		return nil, fmt.Errorf("prepare update: staging directory digest: %w", err)
	}
	tx.BackupTreeID, err = repairPlanTreeContentStateID(tx.TargetPath)
	if err != nil {
		return nil, fmt.Errorf("prepare update: current bundle digest: %w", err)
	}
	if err := VerifyAppBundleUpdateHandoffSource(tx); err != nil {
		return nil, fmt.Errorf("prepare update: %w", err)
	}
	if err := VerifyAppBundleUpdateHandoffOriginal(tx); err != nil {
		return nil, fmt.Errorf("prepare update: %w", err)
	}
	orphanedBackup, orphanedTreeID, err := quarantineExistingAppBundleUpdateBackup(tx)
	if err != nil {
		return nil, fmt.Errorf("prepare update: recover existing handoff backup: %w", err)
	}
	tx.OrphanedBackupPath = orphanedBackup
	tx.OrphanedBackupTreeID = orphanedTreeID
	// An uncooperative writer is not covered by Reasonix's mutation lock. Recheck
	// the public path after quarantine so a recreated node is never adopted as the
	// rollback backup of the new transaction.
	if err := verifyAppBundleUpdateHandoffBackupAbsent(tx); err != nil {
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

func newAppBundleUpdateTransaction(fromVersion, toVersion, appPath, backupPath string) (*UpdateTransaction, error) {
	tx := &UpdateTransaction{
		SchemaVersion: updateTransactionVersion,
		FromVersion:   fromVersion,
		ToVersion:     toVersion,
		Platform:      runtime.GOOS + "/" + runtime.GOARCH,
		TargetKind:    "app-bundle",
		TargetPath:    filepath.Clean(strings.TrimSpace(appPath)),
		BackupPath:    filepath.Clean(strings.TrimSpace(backupPath)),
		CreatedAt:     time.Now().UTC().Format(time.RFC3339Nano),
	}
	if !strings.HasSuffix(strings.ToLower(tx.TargetPath), ".app") ||
		tx.BackupPath != tx.TargetPath+".reasonix-update-backup" {
		return nil, fmt.Errorf("prepare update: invalid macOS bundle paths")
	}
	return tx, nil
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

// ClaimPendingAppBundleUpdateHandoffExact additionally binds the detached
// updater to the complete transaction prepared by the parent process.
func ClaimPendingAppBundleUpdateHandoffExact(
	expectedToVersion, expectedCreatedAt, expectedTransactionID string,
	timeout time.Duration,
) (*UpdateTransaction, func(), error) {
	expectedTransactionID = strings.TrimSpace(expectedTransactionID)
	if expectedTransactionID == "" {
		return nil, nil, fmt.Errorf("claim update handoff: transaction identity is incomplete")
	}
	return claimPendingAppBundleUpdateHandoff(
		expectedToVersion,
		expectedCreatedAt,
		expectedTransactionID,
		timeout,
	)
}

func claimPendingAppBundleUpdateHandoff(
	expectedToVersion, expectedCreatedAt, expectedTransactionID string,
	timeout time.Duration,
) (*UpdateTransaction, func(), error) {
	expectedToVersion = strings.TrimSpace(expectedToVersion)
	expectedCreatedAt = strings.TrimSpace(expectedCreatedAt)
	if expectedToVersion == "" || expectedCreatedAt == "" {
		return nil, nil, fmt.Errorf("claim update handoff: transaction identity is incomplete")
	}
	unlockPending, err := acquirePendingUpdateLock()
	if err != nil {
		return nil, nil, fmt.Errorf("claim update handoff: lock pending transaction: %w", err)
	}
	fail := func(err error) (*UpdateTransaction, func(), error) {
		unlockPending()
		return nil, nil, err
	}

	tx, err := ReadPendingUpdate()
	if err != nil {
		return fail(fmt.Errorf("claim update handoff: read pending transaction: %w", err))
	}
	if tx.TargetKind != "app-bundle" ||
		strings.TrimSpace(tx.ToVersion) != expectedToVersion ||
		strings.TrimSpace(tx.CreatedAt) != expectedCreatedAt {
		return fail(fmt.Errorf("claim update handoff: pending transaction does not match"))
	}
	if expectedTransactionID != "" && UpdateTransactionID(tx) != expectedTransactionID {
		return fail(fmt.Errorf("claim update handoff: pending transaction changed"))
	}
	if tx.Platform != runtime.GOOS+"/"+runtime.GOARCH {
		return fail(fmt.Errorf("claim update handoff: pending transaction platform does not match"))
	}
	if err := validateAppBundleHandoffMetadata(tx); err != nil {
		return fail(fmt.Errorf("claim update handoff: %w", err))
	}
	if strings.TrimSpace(tx.HandoffAppPath) == "" ||
		strings.TrimSpace(tx.HandoffStagingPath) == "" ||
		tx.HandoffOwnerPID <= 0 {
		return fail(fmt.Errorf("claim update handoff: handoff metadata is missing"))
	}

	unlockTargets, err := lockRepairMutationsTimeout(timeout, pendingUpdateTargetPaths(tx)...)
	if err != nil {
		return fail(fmt.Errorf("claim update handoff: lock targets: %w", err))
	}
	current, err := ReadPendingUpdate()
	if err != nil {
		unlockTargets()
		return fail(fmt.Errorf("claim update handoff: re-read pending transaction: %w", err))
	}
	if !reflect.DeepEqual(tx, current) {
		unlockTargets()
		return fail(fmt.Errorf("claim update handoff: pending transaction changed while waiting"))
	}
	if err := verifyAppBundleUpdateHandoffBackupAbsent(current); err != nil {
		unlockTargets()
		return fail(fmt.Errorf("claim update handoff: %w", err))
	}
	if err := VerifyAppBundleUpdateHandoffSource(current); err != nil {
		unlockTargets()
		return fail(fmt.Errorf("claim update handoff: %w", err))
	}
	if err := VerifyAppBundleUpdateHandoffOriginal(current); err != nil {
		unlockTargets()
		return fail(fmt.Errorf("claim update handoff: %w", err))
	}

	var once sync.Once
	release := func() {
		once.Do(func() {
			unlockTargets()
			unlockPending()
		})
	}
	return current, release, nil
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

func claimPendingFileUpdate(
	expectedToVersion, expectedCreatedAt, expectedTransactionID, launcherPath string,
	expectedTargetPaths []string,
	timeout time.Duration,
) (*UpdateTransaction, func(), error) {
	expectedToVersion = strings.TrimSpace(expectedToVersion)
	expectedCreatedAt = strings.TrimSpace(expectedCreatedAt)
	launcherPath = filepath.Clean(strings.TrimSpace(launcherPath))
	if expectedToVersion == "" || expectedCreatedAt == "" || launcherPath == "" || launcherPath == "." {
		return nil, nil, fmt.Errorf("claim file update: transaction identity is incomplete")
	}
	if len(expectedTargetPaths) == 0 {
		return nil, nil, fmt.Errorf("claim file update: release unit is empty")
	}

	unlockPending, err := acquirePendingUpdateLock()
	if err != nil {
		return nil, nil, fmt.Errorf("claim file update: lock pending transaction: %w", err)
	}
	fail := func(err error) (*UpdateTransaction, func(), error) {
		unlockPending()
		return nil, nil, err
	}
	tx, err := readPendingUpdateForLauncher(launcherPath)
	if err != nil {
		return fail(fmt.Errorf("claim file update: read pending transaction: %w", err))
	}
	if tx.TargetKind != "file" ||
		strings.TrimSpace(tx.ToVersion) != expectedToVersion ||
		strings.TrimSpace(tx.CreatedAt) != expectedCreatedAt {
		return fail(fmt.Errorf("claim file update: pending transaction does not match"))
	}
	if expectedTransactionID != "" && UpdateTransactionID(tx) != expectedTransactionID {
		return fail(fmt.Errorf("claim file update: pending transaction changed"))
	}
	if tx.Platform != runtime.GOOS+"/"+runtime.GOARCH {
		return fail(fmt.Errorf("claim file update: pending transaction platform does not match"))
	}
	targetPaths := pendingUpdateTargetPaths(tx)
	if !sameRepairMutationPaths(targetPaths, expectedTargetPaths) {
		return fail(fmt.Errorf("claim file update: release unit does not match"))
	}

	unlockTargets, err := lockRepairMutationsTimeout(timeout, targetPaths...)
	if err != nil {
		return fail(fmt.Errorf("claim file update: lock targets: %w", err))
	}
	current, err := readPendingUpdateForLauncher(launcherPath)
	if err != nil {
		unlockTargets()
		return fail(fmt.Errorf("claim file update: re-read pending transaction: %w", err))
	}
	if !reflect.DeepEqual(tx, current) {
		unlockTargets()
		return fail(fmt.Errorf("claim file update: pending transaction changed while waiting"))
	}
	if err := verifyPreparedFileUpdateTargets(current); err != nil {
		unlockTargets()
		return fail(fmt.Errorf("claim file update: %w", err))
	}

	var once sync.Once
	release := func() {
		once.Do(func() {
			unlockTargets()
			unlockPending()
		})
	}
	return current, release, nil
}

// verifyPreparedFileUpdateTargets proves that the release unit still matches
// the exact files snapshotted by PrepareFileUpdate. Path and transaction
// identity alone are insufficient: another installer can replace the binaries
// between prepare and claim while leaving pending-update.json untouched.
func verifyPreparedFileUpdateTargets(tx *UpdateTransaction) error {
	if err := verifyPreparedFileUpdateBackups(tx); err != nil {
		return err
	}
	for _, f := range pendingUpdateFiles(tx) {
		info, err := os.Lstat(f.TargetPath)
		if f.MissingBefore {
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return fmt.Errorf("inspect prepared release file %s: %w", filepath.Base(f.TargetPath), err)
			}
			return fmt.Errorf("prepared release file %s appeared after backup", filepath.Base(f.TargetPath))
		}
		if err != nil {
			return fmt.Errorf("inspect prepared release file %s: %w", filepath.Base(f.TargetPath), err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("prepared release file %s changed type", filepath.Base(f.TargetPath))
		}
		got, err := hashFile(f.TargetPath)
		if err != nil {
			return fmt.Errorf("hash prepared release file %s: %w", filepath.Base(f.TargetPath), err)
		}
		if !strings.EqualFold(got, f.SHA256) {
			return fmt.Errorf("prepared release file %s changed after backup", filepath.Base(f.TargetPath))
		}
	}
	return nil
}

func verifyPreparedFileUpdateBackups(tx *UpdateTransaction) error {
	for _, f := range pendingUpdateFiles(tx) {
		if f.MissingBefore {
			continue
		}
		backupInfo, err := os.Lstat(f.BackupPath)
		if err != nil {
			return fmt.Errorf("inspect prepared backup for %s: %w", filepath.Base(f.TargetPath), err)
		}
		if !backupInfo.Mode().IsRegular() {
			return fmt.Errorf("prepared backup for %s changed type", filepath.Base(f.TargetPath))
		}
		backupHash, err := hashFile(f.BackupPath)
		if err != nil {
			return fmt.Errorf("hash prepared backup for %s: %w", filepath.Base(f.TargetPath), err)
		}
		if !strings.EqualFold(backupHash, f.SHA256) {
			return fmt.Errorf("prepared backup for %s changed after backup", filepath.Base(f.TargetPath))
		}
	}
	return nil
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

// PublishClaimedFileUpdateMemberExact returns proof of the exact node it
// published. Callers must retain every receipt and pass them to
// RecordClaimedFileUpdateInstalled before releasing the update claim.
func PublishClaimedFileUpdateMemberExact(
	claimed *UpdateTransaction,
	targetPath string,
	content []byte,
	mode os.FileMode,
) (FileUpdateInstallReceipt, error) {
	if claimed == nil || claimed.TargetKind != "file" {
		return FileUpdateInstallReceipt{}, fmt.Errorf("publish file update: transaction identity is incomplete")
	}
	current, err := readPendingUpdateForLauncher(claimed.TargetPath)
	if err != nil {
		return FileUpdateInstallReceipt{}, fmt.Errorf("publish file update: read pending transaction: %w", err)
	}
	if !reflect.DeepEqual(claimed, current) {
		return FileUpdateInstallReceipt{}, fmt.Errorf("publish file update: pending transaction changed")
	}
	targetPath = filepath.Clean(strings.TrimSpace(targetPath))
	targetKey := canonicalRepairPath(targetPath)
	var member *UpdateTransactionFile
	for i := range current.Files {
		if canonicalRepairPath(current.Files[i].TargetPath) == targetKey {
			member = &current.Files[i]
			break
		}
	}
	if targetKey == "" || member == nil {
		return FileUpdateInstallReceipt{}, fmt.Errorf("publish file update: target is outside the claimed release unit")
	}
	preparedState := ""
	if !member.MissingBefore {
		if err := verifyUpdateFileMatchesPrepared(*member); err != nil {
			return FileUpdateInstallReceipt{}, fmt.Errorf("publish file update: %w", err)
		}
		if err := verifyUpdateBackupFile(*member); err != nil {
			return FileUpdateInstallReceipt{}, fmt.Errorf("publish file update: %w", err)
		}
		preparedState = repairPlanReleaseNodeState(member.TargetPath)
	} else if _, err := os.Lstat(member.TargetPath); err == nil {
		return FileUpdateInstallReceipt{}, fmt.Errorf("publish file update: prepared release file %s appeared after backup", filepath.Base(member.TargetPath))
	} else if !os.IsNotExist(err) {
		return FileUpdateInstallReceipt{}, fmt.Errorf("publish file update: inspect prepared release file %s: %w", filepath.Base(member.TargetPath), err)
	}

	stage, expectedHash, installedStateID, err := stageFileUpdateContent(member.TargetPath, content, mode)
	if err != nil {
		return FileUpdateInstallReceipt{}, fmt.Errorf("publish file update: stage %s: %w", filepath.Base(member.TargetPath), err)
	}
	stagePublished := false
	defer func() {
		if !stagePublished {
			_ = removeUpdateBackupFileMatching(stage, expectedHash)
		}
	}()

	retained := ""
	if !member.MissingBefore {
		transactionID := UpdateTransactionID(claimed)
		if len(transactionID) < 16 {
			return FileUpdateInstallReceipt{}, fmt.Errorf("publish file update: transaction identity is incomplete")
		}
		retained = member.TargetPath + ".reasonix-update-aside-" + transactionID[:16]
		if err := renameRepairNodeNoReplace(member.TargetPath, retained); err != nil {
			return FileUpdateInstallReceipt{}, fmt.Errorf("publish file update: retain %s: %w", filepath.Base(member.TargetPath), err)
		}
		fileUpdateAfterRetain(member.TargetPath, retained)
		if err := verifyRepairPlanReleaseNodeStateFor(retained, member.TargetPath, preparedState); err != nil {
			if restoreErr := restoreRepairNodeIfAbsent(retained, member.TargetPath); restoreErr != nil {
				return FileUpdateInstallReceipt{}, fmt.Errorf("%w; verified prior release file retained at %s: %w", err, retained, restoreErr)
			}
			return FileUpdateInstallReceipt{}, fmt.Errorf("publish file update: prepared release file changed during retain: %w", err)
		}
	}
	if err := renameRepairNodeNoReplace(stage, member.TargetPath); err != nil {
		if retained != "" {
			if restoreErr := restoreRepairNodeIfAbsent(retained, member.TargetPath); restoreErr != nil {
				return FileUpdateInstallReceipt{}, fmt.Errorf("publish file update: publish %s: %w; verified prior release file retained at %s: %w", filepath.Base(member.TargetPath), err, retained, restoreErr)
			}
		}
		return FileUpdateInstallReceipt{}, fmt.Errorf("publish file update: publish %s: %w", filepath.Base(member.TargetPath), err)
	}
	stagePublished = true
	if err := verifyRepairPlanReleaseNodeStateFor(member.TargetPath, member.TargetPath, installedStateID); err != nil {
		rejected, retainErr := moveRepairNodeToUniqueCleanup(member.TargetPath)
		if retainErr != nil || rejected == "" {
			if retainErr != nil {
				return FileUpdateInstallReceipt{}, fmt.Errorf("publish file update: installed %s changed: %w; retain rejected file: %w", filepath.Base(member.TargetPath), err, retainErr)
			}
			return FileUpdateInstallReceipt{}, fmt.Errorf("publish file update: installed %s changed: %w; rejected file disappeared before compensation", filepath.Base(member.TargetPath), err)
		}
		if retained == "" {
			return FileUpdateInstallReceipt{}, fmt.Errorf("publish file update: installed %s changed: %w; rejected file retained at %s", filepath.Base(member.TargetPath), err, rejected)
		}
		if verifyErr := verifyRepairPlanReleaseNodeStateFor(retained, member.TargetPath, preparedState); verifyErr != nil {
			return FileUpdateInstallReceipt{}, fmt.Errorf("publish file update: installed %s changed: %w; rejected file retained at %s; prepared file changed at %s: %w", filepath.Base(member.TargetPath), err, rejected, retained, verifyErr)
		}
		if restoreErr := restoreRepairNodeIfAbsent(retained, member.TargetPath); restoreErr != nil {
			return FileUpdateInstallReceipt{}, fmt.Errorf("publish file update: installed %s changed: %w; rejected file retained at %s; restore prepared file: %w", filepath.Base(member.TargetPath), err, rejected, restoreErr)
		}
		return FileUpdateInstallReceipt{}, fmt.Errorf("publish file update: installed %s changed: %w; rejected file retained at %s and prepared file restored", filepath.Base(member.TargetPath), err, rejected)
	}
	if retained != "" {
		_ = removeUpdateNodeMatching(retained, func(moved string) error {
			return verifyRepairPlanReleaseNodeStateFor(moved, member.TargetPath, preparedState)
		}, false)
	}
	return FileUpdateInstallReceipt{
		UpdateTransactionID: UpdateTransactionID(current),
		TargetPath:          member.TargetPath,
		InstalledStateID:    installedStateID,
	}, nil
}

func verifyUpdateFileMatchesPrepared(f UpdateTransactionFile) error {
	info, err := os.Lstat(f.TargetPath)
	if err != nil {
		return fmt.Errorf("inspect prepared release file %s: %w", filepath.Base(f.TargetPath), err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("prepared release file %s changed type", filepath.Base(f.TargetPath))
	}
	if err := verifyRegularFileHash(f.TargetPath, f.SHA256); err != nil {
		return fmt.Errorf("prepared release file %s changed after backup: %w", filepath.Base(f.TargetPath), err)
	}
	return nil
}

func verifyUpdateBackupFile(f UpdateTransactionFile) error {
	info, err := os.Lstat(f.BackupPath)
	if err != nil {
		return fmt.Errorf("inspect prepared backup for %s: %w", filepath.Base(f.TargetPath), err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("prepared backup for %s changed type", filepath.Base(f.TargetPath))
	}
	if err := verifyRegularFileHash(f.BackupPath, f.SHA256); err != nil {
		return fmt.Errorf("prepared backup for %s changed after backup: %w", filepath.Base(f.TargetPath), err)
	}
	return nil
}

func verifyRegularFileHash(path, expected string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("not a regular file")
	}
	actual, err := hashFile(path)
	if err != nil {
		return err
	}
	if !strings.EqualFold(actual, expected) {
		return fmt.Errorf("hash mismatch")
	}
	return nil
}

func stageFileUpdateContent(targetPath string, content []byte, mode os.FileMode) (string, string, string, error) {
	tmp, err := os.CreateTemp(filepath.Dir(targetPath), "."+filepath.Base(targetPath)+".reasonix-update-stage-*")
	if err != nil {
		return "", "", "", err
	}
	path := tmp.Name()
	cleanup := func(err error) (string, string, string, error) {
		_ = tmp.Close()
		_ = os.Remove(path)
		return "", "", "", err
	}
	if _, err := tmp.Write(content); err != nil {
		return cleanup(err)
	}
	if err := tmp.Sync(); err != nil {
		return cleanup(err)
	}
	if err := tmp.Chmod(mode); err != nil {
		return cleanup(err)
	}
	info, err := tmp.Stat()
	if err != nil {
		return cleanup(err)
	}
	installedStateID := repairPlanReadStateIDFor(
		targetPath,
		info.Mode(),
		"file",
		"",
		content,
		true,
	)
	if err := tmp.Close(); err != nil {
		_ = os.Remove(path)
		return "", "", "", err
	}
	sum := sha256.Sum256(content)
	return path, hex.EncodeToString(sum[:]), installedStateID, nil
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

func installedFileUpdateStatePath(tx *UpdateTransaction) string {
	if tx == nil {
		return ""
	}
	transactionID := UpdateTransactionID(tx)
	root := config.MemoryUserDir()
	if root == "" || len(transactionID) != sha256.Size*2 {
		return ""
	}
	return filepath.Join(root, "repair", "updates", transactionID+".installed.json")
}

func createInstalledFileUpdateState(tx *UpdateTransaction, record *installedFileUpdateState) error {
	if err := validateInstalledFileUpdateState(tx, record); err != nil {
		return err
	}
	path := installedFileUpdateStatePath(tx)
	if path == "" {
		return fmt.Errorf("installed release-unit state path is unavailable")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if !repairNodeInsideResolvedRoot(filepath.Join(config.MemoryUserDir(), "repair"), path) {
		return fmt.Errorf("installed release-unit state resolves outside the repair directory")
	}
	b, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	if err := fileutil.AtomicCreateFile(path, append(b, '\n'), 0o600); err == nil {
		return nil
	} else if !os.IsExist(err) {
		return err
	}
	existing, err := readInstalledFileUpdateState(tx)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(existing, record) {
		return fmt.Errorf("installed release-unit state already exists with different content")
	}
	return nil
}

func readInstalledFileUpdateState(tx *UpdateTransaction) (*installedFileUpdateState, error) {
	path := installedFileUpdateStatePath(tx)
	if path == "" {
		return nil, fmt.Errorf("installed release-unit state path is unavailable")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("installed release-unit state is not a regular file")
	}
	if !repairNodeInsideResolvedRoot(filepath.Join(config.MemoryUserDir(), "repair"), path) {
		return nil, fmt.Errorf("installed release-unit state resolves outside the repair directory")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var record installedFileUpdateState
	if err := json.Unmarshal(b, &record); err != nil {
		return nil, err
	}
	if err := validateInstalledFileUpdateState(tx, &record); err != nil {
		return nil, err
	}
	return &record, nil
}

func validateInstalledFileUpdateState(tx *UpdateTransaction, record *installedFileUpdateState) error {
	if tx == nil || tx.TargetKind != "file" || len(tx.Files) == 0 ||
		record == nil || record.SchemaVersion != 1 ||
		record.UpdateTransactionID != UpdateTransactionID(tx) ||
		len(record.InstalledStateIDs) != len(tx.Files) {
		return fmt.Errorf("installed release-unit state is incomplete")
	}
	for _, stateID := range record.InstalledStateIDs {
		stateID = strings.TrimSpace(stateID)
		if len(stateID) != sha256.Size*2 {
			return fmt.Errorf("installed release-unit state is invalid")
		}
		if _, err := hex.DecodeString(stateID); err != nil {
			return fmt.Errorf("installed release-unit state is invalid")
		}
	}
	return nil
}

func installedFileUpdateTargets(
	tx *UpdateTransaction,
	requireBinding bool,
) ([]UpdateTransactionFile, bool, error) {
	if tx == nil || tx.TargetKind != "file" || len(tx.Files) == 0 {
		if requireBinding {
			return nil, false, fmt.Errorf("installed release-unit state is missing")
		}
		return pendingUpdateFiles(tx), false, nil
	}
	files := append([]UpdateTransactionFile(nil), tx.Files...)
	bound := 0
	for _, f := range files {
		if strings.TrimSpace(f.InstalledStateID) != "" {
			bound++
		}
	}
	if bound != 0 && bound != len(files) {
		return nil, false, fmt.Errorf("installed release-unit state is incomplete")
	}
	if bound == 0 {
		record, err := readInstalledFileUpdateState(tx)
		if err != nil {
			if os.IsNotExist(err) {
				if requireBinding {
					return nil, false, fmt.Errorf("installed release-unit state is missing")
				}
				return files, false, nil
			}
			return nil, false, err
		}
		for i := range files {
			files[i].InstalledStateID = record.InstalledStateIDs[i]
		}
	}
	for _, f := range files {
		if err := verifyRepairPlanReleaseNodeStateFor(f.TargetPath, f.TargetPath, f.InstalledStateID); err != nil {
			return nil, true, fmt.Errorf("installed release file %s changed: %w", filepath.Base(f.TargetPath), err)
		}
	}
	return files, true, nil
}

func removeInstalledFileUpdateState(tx *UpdateTransaction) error {
	record, err := readInstalledFileUpdateState(tx)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	path := installedFileUpdateStatePath(tx)
	expectedState := repairPlanFileState(path)
	return removeUpdateNodeMatching(path, func(moved string) error {
		if err := verifyRepairPlanStateIDFor(moved, path, expectedState); err != nil {
			return err
		}
		b, err := os.ReadFile(moved)
		if err != nil {
			return err
		}
		var actual installedFileUpdateState
		if err := json.Unmarshal(b, &actual); err != nil {
			return err
		}
		if !reflect.DeepEqual(&actual, record) {
			return fmt.Errorf("installed release-unit state changed before cleanup")
		}
		return nil
	}, false)
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

// CancelPendingAppBundleUpdateHandoffExact abandons only the full transaction
// read or prepared by the caller. It is safe to use after a PID wait or failed
// claim where pending state may have been rewritten with copied scalar IDs.
func CancelPendingAppBundleUpdateHandoffExact(
	expected *UpdateTransaction,
	timeout time.Duration,
) (*UpdateTransaction, error) {
	if expected == nil {
		return nil, fmt.Errorf("cancel update handoff: transaction identity is incomplete")
	}
	return cancelPendingAppBundleUpdateHandoff(
		expected.ToVersion,
		expected.CreatedAt,
		timeout,
		repairPlanStateID(expected),
	)
}

func cancelPendingAppBundleUpdateHandoff(
	expectedToVersion, expectedCreatedAt string,
	timeout time.Duration,
	expectedTransactionID string,
) (*UpdateTransaction, error) {
	expectedToVersion = strings.TrimSpace(expectedToVersion)
	expectedCreatedAt = strings.TrimSpace(expectedCreatedAt)
	if expectedToVersion == "" || expectedCreatedAt == "" {
		return nil, fmt.Errorf("cancel update handoff: transaction identity is incomplete")
	}
	unlockPending, err := acquirePendingUpdateLock()
	if err != nil {
		return nil, fmt.Errorf("cancel update handoff: lock pending transaction: %w", err)
	}
	defer unlockPending()
	tx, err := ReadPendingUpdate()
	if err != nil {
		return nil, fmt.Errorf("cancel update handoff: read pending transaction: %w", err)
	}
	if tx.TargetKind != "app-bundle" ||
		strings.TrimSpace(tx.ToVersion) != expectedToVersion ||
		strings.TrimSpace(tx.CreatedAt) != expectedCreatedAt {
		return nil, fmt.Errorf("cancel update handoff: pending transaction does not match")
	}
	if expected := strings.TrimSpace(expectedTransactionID); expected != "" && expected != repairPlanStateID(tx) {
		return nil, fmt.Errorf("cancel update handoff: pending transaction changed")
	}
	unlockTargets, err := lockRepairMutationsTimeout(timeout, pendingUpdateTargetPaths(tx)...)
	if err != nil {
		return nil, fmt.Errorf("cancel update handoff: lock targets: %w", err)
	}
	defer unlockTargets()
	current, err := ReadPendingUpdate()
	if err != nil {
		return nil, fmt.Errorf("cancel update handoff: re-read pending transaction: %w", err)
	}
	if !reflect.DeepEqual(tx, current) {
		return nil, fmt.Errorf("cancel update handoff: pending transaction changed while waiting")
	}
	if err := VerifyAppBundleUpdateHandoffOriginal(current); err != nil {
		return nil, fmt.Errorf("cancel update handoff: %w", err)
	}
	if err := verifyAppBundleUpdateHandoffBackupAbsent(current); err != nil {
		return nil, fmt.Errorf("cancel update handoff: %w", err)
	}
	if err := removePendingUpdateExactVerified(current, func() error {
		if err := VerifyAppBundleUpdateHandoffOriginal(current); err != nil {
			return fmt.Errorf("cancel update handoff: %w", err)
		}
		if err := verifyAppBundleUpdateHandoffBackupAbsent(current); err != nil {
			return fmt.Errorf("cancel update handoff: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return current, nil
}

func sameRepairMutationPaths(a, b []string) bool {
	keys := func(paths []string) []string {
		seen := make(map[string]struct{}, len(paths))
		result := make([]string, 0, len(paths))
		for _, path := range paths {
			key := canonicalRepairPath(path)
			if key == "" {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			result = append(result, key)
		}
		sort.Strings(result)
		return result
	}
	return reflect.DeepEqual(keys(a), keys(b))
}

// VerifyAppBundleUpdateHandoffSource checks the real staging containment and
// the complete staged tree immediately before a handoff mutates the install.
// The lexical metadata check remains readable after staging cleanup, while this
// stronger check is only used while the source bundle still exists.
func VerifyAppBundleUpdateHandoffSource(tx *UpdateTransaction) error {
	if tx == nil || tx.TargetKind != "app-bundle" {
		return fmt.Errorf("handoff source transaction is invalid")
	}
	if strings.TrimSpace(tx.HandoffAppTreeID) == "" {
		return fmt.Errorf("handoff source digest is missing")
	}
	if strings.TrimSpace(tx.HandoffStagingTreeID) == "" {
		return fmt.Errorf("handoff staging digest is missing")
	}
	if err := validateAppBundleHandoffSourcePaths(tx); err != nil {
		return err
	}
	actual, err := repairPlanTreeContentStateID(tx.HandoffAppPath)
	if err != nil {
		return fmt.Errorf("read staged bundle digest: %w", err)
	}
	if actual != tx.HandoffAppTreeID {
		return fmt.Errorf("staged bundle changed after verification")
	}
	actual, err = repairPlanTreeContentStateID(tx.HandoffStagingPath)
	if err != nil {
		return fmt.Errorf("read staging directory digest: %w", err)
	}
	if actual != tx.HandoffStagingTreeID {
		return fmt.Errorf("staging directory changed after verification")
	}
	return nil
}

// CleanupAppBundleUpdateHandoffStaging removes only the complete staging tree
// recorded by the transaction. The root is first displaced to a unique sibling,
// so a concurrent recreation at the public staging path survives.
func CleanupAppBundleUpdateHandoffStaging(tx *UpdateTransaction) error {
	if tx == nil || strings.TrimSpace(tx.HandoffStagingTreeID) == "" {
		return fmt.Errorf("cleanup update staging: transaction identity is incomplete")
	}
	if err := validateAppBundleHandoffMetadata(tx); err != nil {
		return fmt.Errorf("cleanup update staging: %w", err)
	}
	relApp, err := filepath.Rel(tx.HandoffStagingPath, tx.HandoffAppPath)
	if err != nil || relApp == "." || relApp == ".." || strings.HasPrefix(relApp, ".."+string(filepath.Separator)) {
		return fmt.Errorf("cleanup update staging: app path is invalid")
	}
	return removeUpdateNodeMatching(tx.HandoffStagingPath, func(moved string) error {
		actual, err := repairPlanTreeContentStateID(moved)
		if err != nil {
			return err
		}
		if actual != tx.HandoffStagingTreeID {
			return fmt.Errorf("staging directory changed before cleanup")
		}
		return verifyAppBundleUpdateHandoffReplacement(
			tx,
			filepath.Join(moved, relApp),
			"staged",
		)
	}, true)
}

// CleanupAppBundleUpdateReplacement removes a displaced replacement only when
// its complete tree still matches the transaction's verified source.
func CleanupAppBundleUpdateReplacement(tx *UpdateTransaction, path string) error {
	return removeUpdateNodeMatching(path, func(moved string) error {
		return verifyAppBundleUpdateHandoffReplacement(tx, moved, "replacement")
	}, true)
}

// VerifyAppBundleUpdateHandoffTarget proves that the bytes copied into the
// installed bundle are the same tree that was verified in staging.
func VerifyAppBundleUpdateHandoffTarget(tx *UpdateTransaction) error {
	if tx == nil {
		return fmt.Errorf("handoff target transaction is invalid")
	}
	return verifyAppBundleUpdateHandoffReplacement(tx, tx.TargetPath, "installed")
}

// VerifyAppBundleUpdateHandoffReplacement proves that a candidate replacement
// tree matches the bundle captured during prepare. The macOS handoff uses this
// before atomically publishing a sibling staging bundle at the install path.
func VerifyAppBundleUpdateHandoffReplacement(tx *UpdateTransaction, path string) error {
	return verifyAppBundleUpdateHandoffReplacement(tx, path, "replacement")
}

func verifyAppBundleUpdateHandoffReplacement(tx *UpdateTransaction, path, subject string) error {
	if tx == nil || tx.TargetKind != "app-bundle" {
		return fmt.Errorf("handoff target transaction is invalid")
	}
	if strings.TrimSpace(tx.HandoffAppTreeID) == "" {
		return fmt.Errorf("handoff target digest is missing")
	}
	actual, err := repairPlanTreeContentStateID(path)
	if err != nil {
		return fmt.Errorf("read %s bundle digest: %w", subject, err)
	}
	if actual != tx.HandoffAppTreeID {
		return fmt.Errorf("%s bundle differs from verified staging", subject)
	}
	return nil
}

// VerifyAppBundleUpdateHandoffOriginal checks that the installed bundle about
// to become the rollback backup is still the tree captured during prepare.
func VerifyAppBundleUpdateHandoffOriginal(tx *UpdateTransaction) error {
	if tx == nil || tx.TargetKind != "app-bundle" {
		return fmt.Errorf("handoff original transaction is invalid")
	}
	return verifyAppBundleUpdateTree(tx.TargetPath, tx.BackupTreeID, "installed bundle changed after prepare")
}

// VerifyAppBundleUpdateHandoffBackup checks the node produced by the
// target-to-backup rename before the replacement bundle is copied into place.
func VerifyAppBundleUpdateHandoffBackup(tx *UpdateTransaction) error {
	if tx == nil || tx.TargetKind != "app-bundle" {
		return fmt.Errorf("handoff backup transaction is invalid")
	}
	return verifyAppBundleUpdateTree(tx.BackupPath, tx.BackupTreeID, "rollback backup differs from prepared bundle")
}

func verifyAppBundleUpdateHandoffBackupAbsent(tx *UpdateTransaction) error {
	if tx == nil || tx.TargetKind != "app-bundle" || strings.TrimSpace(tx.BackupPath) == "" {
		return fmt.Errorf("handoff backup transaction is invalid")
	}
	if _, err := os.Lstat(tx.BackupPath); err == nil {
		return fmt.Errorf("handoff backup path already exists")
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect handoff backup path: %w", err)
	}
	return nil
}

// quarantineExistingAppBundleUpdateBackup recovers the pre-v1.20 state where
// a committed macOS update removed pending-update.json but its sibling rollback
// bundle survived best-effort cleanup. Without the transaction there is no
// trustworthy authority to delete or reuse that bundle, so preparation moves it
// aside with a no-replace rename and preserves it for diagnosis.
//
// The caller holds both the pending-update lock and the target mutation locks.
// The current executable binding prevents a crafted caller from quarantining a
// similarly named bundle beside an unrelated application.
func quarantineExistingAppBundleUpdateBackup(tx *UpdateTransaction) (string, string, error) {
	if tx == nil || tx.TargetKind != "app-bundle" ||
		tx.BackupPath != tx.TargetPath+".reasonix-update-backup" {
		return "", "", fmt.Errorf("handoff backup transaction is invalid")
	}
	info, err := os.Lstat(tx.BackupPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", "", nil
		}
		return "", "", fmt.Errorf("inspect existing handoff backup: %w", err)
	}
	if !info.IsDir() {
		return "", "", fmt.Errorf("existing handoff backup is not a directory")
	}

	launcher, err := repairExecutable()
	if err != nil {
		return "", "", fmt.Errorf("resolve current Reasonix executable: %w", err)
	}
	resolvedTarget, err := filepath.EvalSymlinks(tx.TargetPath)
	if err != nil {
		return "", "", fmt.Errorf("resolve current app bundle: %w", err)
	}
	resolvedLauncher, err := filepath.EvalSymlinks(launcher)
	if err != nil {
		return "", "", fmt.Errorf("resolve current Reasonix executable: %w", err)
	}
	rel, err := filepath.Rel(resolvedTarget, resolvedLauncher)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("existing handoff backup is outside the current Reasonix installation")
	}

	expectedTreeID, err := repairPlanTreeContentStateID(tx.BackupPath)
	if err != nil {
		return "", "", fmt.Errorf("read existing handoff backup digest: %w", err)
	}
	for attempt := range 16 {
		quarantine := fmt.Sprintf(
			"%s.reasonix-orphaned-%d-%d",
			tx.BackupPath,
			time.Now().UTC().UnixNano(),
			attempt,
		)
		if err := renameRepairNodeNoReplace(tx.BackupPath, quarantine); err != nil {
			if os.IsExist(err) {
				continue
			}
			return "", "", fmt.Errorf("quarantine existing handoff backup: %w", err)
		}
		updateBackupAfterQuarantine(tx.BackupPath, quarantine)

		restore := func(cause error) error {
			if _, statErr := os.Lstat(tx.BackupPath); statErr == nil {
				return fmt.Errorf("%w; preserved quarantined backup at %s because the public path was recreated", cause, quarantine)
			} else if !os.IsNotExist(statErr) {
				return fmt.Errorf("%w; inspect recreated handoff backup: %w", cause, statErr)
			}
			if restoreErr := renameRepairNodeNoReplace(quarantine, tx.BackupPath); restoreErr != nil {
				return fmt.Errorf("%w; preserved quarantined backup at %s: %w", cause, quarantine, restoreErr)
			}
			return cause
		}

		actualTreeID, digestErr := repairPlanTreeContentStateID(quarantine)
		if digestErr != nil {
			return "", "", restore(fmt.Errorf("read quarantined handoff backup digest: %w", digestErr))
		}
		if actualTreeID != expectedTreeID {
			return "", "", restore(fmt.Errorf("existing handoff backup changed during quarantine"))
		}
		if _, statErr := os.Lstat(tx.BackupPath); statErr == nil {
			return "", "", fmt.Errorf("handoff backup path was recreated during recovery; preserved quarantined backup at %s", quarantine)
		} else if !os.IsNotExist(statErr) {
			return "", "", fmt.Errorf("inspect recovered handoff backup path: %w", statErr)
		}
		return quarantine, actualTreeID, nil
	}
	return "", "", fmt.Errorf("cannot allocate handoff backup quarantine path")
}

// cleanupOrphanedAppBundleUpdateBackup retires only the quarantine recorded by
// a terminal transaction. The no-replace move and digest check keep a changed
// or concurrently replaced directory intact for diagnosis instead of deleting
// a path merely because its name resembles a Reasonix quarantine.
func cleanupOrphanedAppBundleUpdateBackup(tx *UpdateTransaction) {
	if validateOrphanedAppBundleBackupMetadata(tx) != nil ||
		strings.TrimSpace(tx.OrphanedBackupPath) == "" {
		return
	}
	if err := removeUpdateBackupTreeMatching(tx.OrphanedBackupPath, tx.OrphanedBackupTreeID); err != nil {
		slog.Warn("repair: preserving quarantined app backup after cleanup failed",
			"path", tx.OrphanedBackupPath, "error", err)
	}
}

func verifyAppBundleUpdateTree(path, expected, mismatch string) error {
	if strings.TrimSpace(expected) == "" {
		return fmt.Errorf("original bundle digest is missing")
	}
	actual, err := repairPlanTreeContentStateID(path)
	if err != nil {
		return fmt.Errorf("read original bundle digest: %w", err)
	}
	if actual != expected {
		return fmt.Errorf("%s", mismatch)
	}
	return nil
}

// AppBundleTreeDigest exposes the deterministic bundle-content digest to the
// desktop handoff tests and other platform glue without exposing path identity.
func AppBundleTreeDigest(path string) (string, error) {
	return repairPlanTreeContentStateID(path)
}

func validateAppBundleHandoffSourcePaths(tx *UpdateTransaction) error {
	staging, err := filepath.EvalSymlinks(tx.HandoffStagingPath)
	if err != nil {
		return fmt.Errorf("resolve handoff staging directory: %w", err)
	}
	app, err := filepath.EvalSymlinks(tx.HandoffAppPath)
	if err != nil {
		return fmt.Errorf("resolve handoff app bundle: %w", err)
	}
	tempRoot, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		return fmt.Errorf("resolve temporary directory: %w", err)
	}
	within := func(root, path string) bool {
		rel, relErr := filepath.Rel(root, path)
		return relErr == nil && rel != "." && rel != ".." &&
			!strings.HasPrefix(rel, ".."+string(filepath.Separator))
	}
	if !within(tempRoot, staging) {
		return fmt.Errorf("handoff staging directory resolves outside the system temporary directory")
	}
	if !within(staging, app) {
		return fmt.Errorf("handoff app bundle resolves outside its staging directory")
	}
	if info, statErr := os.Stat(app); statErr != nil || !info.IsDir() {
		if statErr != nil {
			return fmt.Errorf("handoff app bundle is unavailable: %w", statErr)
		}
		return fmt.Errorf("handoff app bundle is not a directory")
	}
	return nil
}

// ClearClaimedAppBundleUpdateHandoff removes a failed handoff transaction.
// The caller must still hold the claim returned above.
func ClearClaimedAppBundleUpdateHandoff(claimed *UpdateTransaction) error {
	current, err := ReadPendingUpdate()
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(claimed, current) {
		return fmt.Errorf("clear update handoff: pending transaction changed")
	}
	if err := VerifyAppBundleUpdateHandoffOriginal(current); err != nil {
		return fmt.Errorf("clear update handoff: %w", err)
	}
	if err := verifyAppBundleUpdateHandoffBackupAbsent(current); err != nil {
		return fmt.Errorf("clear update handoff: %w", err)
	}
	if err := removePendingUpdateExactVerified(current, func() error {
		if err := VerifyAppBundleUpdateHandoffOriginal(current); err != nil {
			return fmt.Errorf("clear update handoff: %w", err)
		}
		if err := verifyAppBundleUpdateHandoffBackupAbsent(current); err != nil {
			return fmt.Errorf("clear update handoff: %w", err)
		}
		return nil
	}); err != nil {
		return err
	}
	return nil
}

// WritePendingUpdate is retained for source compatibility with older repair
// callers. Pending transactions are immutable once created; callers that need
// to start an update should use the prepare APIs, and callers that need to
// transition one must use the exact transaction helpers below.
//
// Deprecated: this function only creates a pending transaction and refuses to
// replace an existing one.
func WritePendingUpdate(tx *UpdateTransaction) error {
	return createPendingUpdate(tx)
}

func createPendingUpdate(tx *UpdateTransaction) error {
	return writePendingUpdate(tx, true)
}

func writePendingUpdate(tx *UpdateTransaction, createOnly bool) error {
	if tx == nil {
		return fmt.Errorf("pending update: nil transaction")
	}
	path := PendingUpdatePath()
	if path == "" {
		return fmt.Errorf("pending update: Reasonix state directory is unavailable")
	}
	b, err := json.MarshalIndent(tx, "", "  ")
	if err != nil {
		return err
	}
	if createOnly {
		return fileutil.AtomicCreateFile(path, append(b, '\n'), 0o600)
	}
	return fileutil.AtomicWriteFile(path, append(b, '\n'), 0o600)
}

func removePendingUpdateExactVerified(expected *UpdateTransaction, verify func() error) error {
	if expected == nil {
		return fmt.Errorf("clear pending update: transaction identity is incomplete")
	}
	path := PendingUpdatePath()
	pendingUpdateBeforeCleanup(path)
	cleanup, err := moveRepairNodeToUniqueCleanup(path)
	if err != nil {
		return err
	}
	if cleanup == "" {
		return fmt.Errorf("clear pending update: pending transaction disappeared before commit")
	}
	updateCleanupAfterRename(path, cleanup)
	restore := func(cause error) error {
		if restoreErr := renameRepairNodeNoReplace(cleanup, path); restoreErr != nil {
			return fmt.Errorf("%w; pending transaction retained at %s: %w", cause, cleanup, restoreErr)
		}
		return cause
	}
	b, err := os.ReadFile(cleanup)
	if err != nil {
		return restore(err)
	}
	var actual UpdateTransaction
	if err := json.Unmarshal(b, &actual); err != nil {
		return restore(err)
	}
	if UpdateTransactionID(&actual) != UpdateTransactionID(expected) {
		return restore(fmt.Errorf("clear pending update: pending transaction changed"))
	}
	if verify != nil {
		if err := verify(); err != nil {
			return restore(err)
		}
	}
	if err := removePendingUpdateFile(cleanup); err != nil {
		return restore(err)
	}
	cleanupOrphanedAppBundleUpdateBackup(expected)
	return nil
}

// ensureNoPendingUpdate runs with the pending-update lock held. Preparing a new
// transaction over an existing one would overwrite fixed backup paths before
// the new transaction is durable, destroying the previous rollback material if
// preparation later fails.
func ensureNoPendingUpdate() error {
	disposition, tx, err := classifyPendingUpdate()
	if err != nil {
		return fmt.Errorf("prepare update: %w", err)
	}
	switch disposition {
	case pendingUpdateActionable:
		if tx == nil {
			// Self-describing but not valid for this installation (see
			// ReconcilePendingUpdate): nothing can resume or roll it back, and
			// refusing here would refuse forever. Quarantine the marker so a
			// future update can proceed; target and rollback material are
			// untouched.
			if _, err := quarantinePendingUpdate("not valid for this installation"); err != nil {
				return fmt.Errorf("prepare update: quarantine unusable transaction: %w", err)
			}
			return nil
		}
		return fmt.Errorf("prepare update: a pending update already exists")
	case pendingUpdateDebris:
		// Refusing here would be refusing forever: debris cannot be resumed,
		// rolled back, or cleared by reconciliation, so every future update
		// would fail on a transaction nothing can act on.
		if _, err := quarantinePendingUpdate("blocked a new update"); err != nil {
			return fmt.Errorf("prepare update: quarantine unusable transaction: %w", err)
		}
	}
	return nil
}

// pendingUpdateDisposition is what the pending-update marker on disk currently
// means. Preparation and reconciliation both classify through it so they cannot
// disagree about whether a transaction exists — when they did, a marker that
// reconciliation could not act on still made preparation refuse, and updates
// stayed blocked permanently (#7342).
type pendingUpdateDisposition int

const (
	// pendingUpdateNone: no marker on disk.
	pendingUpdateNone pendingUpdateDisposition = iota
	// pendingUpdateActionable: a transaction that can still be resumed or
	// rolled back. Preparation must refuse over one of these — writing a new
	// transaction would overwrite fixed backup paths and destroy the rollback
	// material this one still owns.
	pendingUpdateActionable
	// pendingUpdateDebris: a marker that cannot describe a recoverable
	// transaction, so it owns no rollback material worth protecting.
	pendingUpdateDebris
)

// classifyPendingUpdate reads the marker and decides what can be done with it.
//
// The line between debris and an actionable transaction is deliberately drawn
// at self-description. A transaction that cannot be parsed, or that does not
// say which release it targets, for which platform, and when it was opened,
// names nothing to roll back to — discarding it loses nothing. Every other
// validation failure is environment-relative (the launcher path, whether the
// target sits inside this Guard installation, where the backup lives) and can
// fail for a perfectly good transaction observed from the wrong install, so
// those keep the old refusal rather than risking real rollback material.
//
// IO failures are errors, never debris: an unreadable marker is not an absent
// one, and quarantining on a transient permission error would throw away a
// recoverable transaction.
func classifyPendingUpdate() (pendingUpdateDisposition, *UpdateTransaction, error) {
	path := PendingUpdatePath()
	if path == "" {
		return pendingUpdateNone, nil, fmt.Errorf("Reasonix state directory is unavailable")
	}
	if _, err := os.Lstat(path); err != nil {
		if os.IsNotExist(err) {
			return pendingUpdateNone, nil, nil
		}
		return pendingUpdateNone, nil, fmt.Errorf("inspect pending transaction: %w", err)
	}
	tx, err := readPendingUpdateUnchecked()
	if err != nil {
		switch {
		case os.IsNotExist(err):
			return pendingUpdateNone, nil, nil
		case isPendingUpdateContentError(err):
			return pendingUpdateDebris, nil, nil
		default:
			return pendingUpdateNone, nil, fmt.Errorf("read pending transaction: %w", err)
		}
	}
	if !pendingUpdateSelfDescribing(tx) {
		return pendingUpdateDebris, nil, nil
	}
	if err := validateUpdateTransaction(tx); err != nil {
		if errors.Is(err, errPendingUpdateForeignInstall) {
			return pendingUpdateActionable, nil, nil
		}
		return pendingUpdateNone, nil, fmt.Errorf("validate pending transaction: %w", err)
	}
	return pendingUpdateActionable, tx, nil
}

// isPendingUpdateContentError reports whether err means the bytes on disk are
// not a transaction, as opposed to the file being unreadable. A prepare
// interrupted mid-write leaves a truncated object, which decodes to a syntax
// error rather than an IO error.
func isPendingUpdateContentError(err error) bool {
	var syntax *json.SyntaxError
	var unmarshalType *json.UnmarshalTypeError
	return errors.As(err, &syntax) || errors.As(err, &unmarshalType) || errors.Is(err, io.ErrUnexpectedEOF)
}

// pendingUpdateSelfDescribing reports whether tx carries the identity any
// recovery needs regardless of where Reasonix is installed: which release it
// targets, for which platform, and when it was opened.
func pendingUpdateSelfDescribing(tx *UpdateTransaction) bool {
	if tx == nil || tx.SchemaVersion != updateTransactionVersion || strings.TrimSpace(tx.ToVersion) == "" {
		return false
	}
	if strings.TrimSpace(tx.Platform) == "" || strings.TrimSpace(tx.CreatedAt) == "" {
		return false
	}
	_, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(tx.CreatedAt))
	return err == nil
}

// quarantinePendingUpdate moves an unusable marker aside and returns where it
// went. It is deliberately not a delete: the marker is the only evidence of
// what went wrong, and a user who reports a stuck updater should still have it.
// Callers hold the pending-update lock.
func quarantinePendingUpdate(reason string) (string, error) {
	path := PendingUpdatePath()
	if path == "" {
		return "", fmt.Errorf("Reasonix state directory is unavailable")
	}
	base := path + ".unusable-" + time.Now().UTC().Format("20060102T150405Z")
	aside := base
	for i := 1; ; i++ {
		if _, err := os.Lstat(aside); os.IsNotExist(err) {
			break
		} else if err != nil {
			return "", err
		}
		aside = fmt.Sprintf("%s-%d", base, i)
	}
	if err := os.Rename(path, aside); err != nil {
		return "", err
	}
	slog.Warn("repair: quarantined an unusable pending update transaction",
		"path", aside, "reason", reason)
	return aside, nil
}

func pendingUpdateMarkerDigest(path string) (string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:]), nil
}

// quarantinePendingUpdateAfterReconcile rechecks an unusable marker while
// holding the cross-process pending lock. Reconciliation initially classifies
// without that lock because the normal cancel/rollback paths acquire it later;
// the marker digest prevents a concurrent prepare from being quarantined after
// it has replaced the marker.
func quarantinePendingUpdateAfterReconcile(reason string, expectedDigest string, wantForeign bool) (bool, error) {
	unlock, err := acquirePendingUpdateLock()
	if err != nil {
		return false, fmt.Errorf("lock pending transaction: %w", err)
	}
	defer unlock()

	path := PendingUpdatePath()
	actualDigest, err := pendingUpdateMarkerDigest(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("read pending transaction: %w", err)
	}
	if actualDigest != expectedDigest {
		return false, fmt.Errorf("pending update changed while waiting")
	}
	disposition, tx, err := classifyPendingUpdate()
	if err != nil {
		return false, err
	}
	if wantForeign {
		if disposition != pendingUpdateActionable || tx != nil {
			return false, fmt.Errorf("pending update is no longer a foreign transaction")
		}
	} else if disposition != pendingUpdateDebris {
		return false, fmt.Errorf("pending update is no longer unusable debris")
	}
	if _, err := quarantinePendingUpdate(reason); err != nil {
		return false, err
	}
	return true, nil
}

func ReadPendingUpdate() (*UpdateTransaction, error) {
	tx, err := readPendingUpdateUnchecked()
	if err != nil {
		return nil, err
	}
	if err := validateUpdateTransaction(tx); err != nil {
		return nil, err
	}
	return tx, nil
}

func readPendingUpdateForLauncher(launcherPath string) (*UpdateTransaction, error) {
	tx, err := readPendingUpdateUnchecked()
	if err != nil {
		return nil, err
	}
	if err := validateUpdateTransactionForLauncher(tx, launcherPath); err != nil {
		return nil, err
	}
	return tx, nil
}

func readPendingUpdateUnchecked() (*UpdateTransaction, error) {
	path := PendingUpdatePath()
	if path == "" {
		return nil, os.ErrNotExist
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var tx UpdateTransaction
	if err := json.Unmarshal(b, &tx); err != nil {
		return nil, err
	}
	return &tx, nil
}

func HasPendingUpdate() bool {
	_, err := ReadPendingUpdate()
	return err == nil
}

// PendingUpdateExists reports the on-disk marker even when its contents are
// malformed. It is intended only for progress/UI decisions; callers must use
// ReadPendingUpdate or ReconcilePendingUpdate before authorizing mutations.
func PendingUpdateExists() bool {
	path := PendingUpdatePath()
	if path == "" {
		return false
	}
	_, err := os.Lstat(path)
	return err == nil
}

// ReconcilePendingUpdate resolves an older immutable update transaction before
// startup or a new install. It first attempts the narrow cancellation path,
// which succeeds only while every original target still matches the state
// captured by prepare and no replacement state is durable. If publication has
// started, it falls back to the exact verified rollback path. Both transitions
// re-read the complete transaction under the pending and target mutation locks.
//
// A transaction targeting runningVersion is left untouched only when the
// transaction also proves that its replacement release unit is installed and
// its rollback state is intact. Version equality alone is not installation
// evidence: a same-version/manual launch may observe an abandoned prepare.
func ReconcilePendingUpdate(runningVersion string) (PendingUpdateReconcileResult, error) {
	disposition, tx, classifyErr := classifyPendingUpdate()
	if classifyErr != nil {
		return PendingUpdateReconcileResult{Pending: true}, fmt.Errorf("reconcile pending update: %w", classifyErr)
	}
	switch {
	case disposition == pendingUpdateNone:
		return PendingUpdateReconcileResult{}, nil
	case disposition == pendingUpdateDebris:
		// Nothing here can be resumed or rolled back. Leaving it in place is
		// what stranded users: startup kept failing to recover it while
		// preparation kept refusing to write over it.
		digest, digestErr := pendingUpdateMarkerDigest(PendingUpdatePath())
		if digestErr != nil {
			if os.IsNotExist(digestErr) {
				return PendingUpdateReconcileResult{}, nil
			}
			return PendingUpdateReconcileResult{Pending: true}, fmt.Errorf("reconcile pending update: read marker: %w", digestErr)
		}
		quarantined, err := quarantinePendingUpdateAfterReconcile("no recoverable transaction to reconcile", digest, false)
		if err != nil {
			return PendingUpdateReconcileResult{Pending: true}, fmt.Errorf("reconcile pending update: quarantine unusable transaction: %w", err)
		}
		if !quarantined {
			return PendingUpdateReconcileResult{}, nil
		}
		return PendingUpdateReconcileResult{Pending: true, Cleared: true}, nil
	case tx == nil:
		// Self-describing but not valid for this installation: the launcher and
		// target directories no longer match (install layout moved to
		// versions\<version>\ or the old install directory is gone). Nothing
		// here can be resumed or rolled back by this installation, and leaving
		// the marker blocks every future update permanently — recovery fails
		// here before preparation can act, and the marker lives in the state
		// directory so a reinstall does not clear it (#7391, #7416, #7407).
		// Quarantine the marker, never a delete: the target and rollback
		// material are untouched, so a genuine transaction observed from the
		// wrong install loses nothing and remains recoverable from the
		// .unusable-* file by hand.
		digest, digestErr := pendingUpdateMarkerDigest(PendingUpdatePath())
		if digestErr != nil {
			if os.IsNotExist(digestErr) {
				return PendingUpdateReconcileResult{}, nil
			}
			return PendingUpdateReconcileResult{Pending: true}, fmt.Errorf("reconcile pending update: read marker: %w", digestErr)
		}
		quarantined, err := quarantinePendingUpdateAfterReconcile("not valid for this installation", digest, true)
		if err != nil {
			return PendingUpdateReconcileResult{Pending: true}, fmt.Errorf("reconcile pending update: quarantine unusable transaction: %w", err)
		}
		if !quarantined {
			return PendingUpdateReconcileResult{}, nil
		}
		return PendingUpdateReconcileResult{Pending: true, Cleared: true}, nil
	}
	result := PendingUpdateReconcileResult{
		Pending:     true,
		FromVersion: tx.FromVersion,
		ToVersion:   tx.ToVersion,
		TargetPath:  tx.TargetPath,
	}
	if UpdateVersionsEqual(runningVersion, tx.ToVersion) &&
		pendingUpdateInstalledForHealth(tx) {
		// After the stale window, auto-commit a still-running probationary target.
		if pendingUpdateHealthIsStale(tx) {
			if healErr := MarkUpdateHealthy(runningVersion); healErr == nil && !PendingUpdateExists() {
				result.Pending = false
				result.Healthy = true
				result.Cleared = true
				return result, nil
			} else if healErr != nil {
				slog.Warn("repair: stale probationary update could not be committed automatically",
					"toVersion", tx.ToVersion, "err", healErr)
			}
		}
		result.AwaitingHealth = true
		return result, ErrPendingUpdateAwaitingHealth
	}

	// Cancel is deliberately attempted before rollback. For app bundles it
	// requires the original tree and an absent backup; for file release units it
	// requires every prepared target and no installed-state sidecar. A failed
	// cancel never mutates the transaction or release unit.
	if cancelErr := CancelPendingUpdateExact(tx); cancelErr == nil {
		// Exact cancellation historically treats a different target version or
		// creation time as an inert success. Re-check the public postcondition so
		// reconciliation never reports a newer transaction as cleared.
		current, currentErr := ReadPendingUpdate()
		if os.IsNotExist(currentErr) {
			result.Cleared = true
			cleanupPendingUpdateStaging(tx)
			return result, nil
		}
		if currentErr != nil {
			return result, fmt.Errorf("reconcile pending update: verify cancellation: %w", currentErr)
		}
		if !reflect.DeepEqual(tx, current) {
			return result, fmt.Errorf("reconcile pending update: pending transaction changed during cancellation")
		}
		return result, fmt.Errorf("reconcile pending update: transaction remained after cancellation")
	}

	rollback, rollbackErr := RollbackPendingUpdateExact(tx)
	if rollbackErr != nil {
		result.RolledBack = rollback.RolledBack
		result.MixedInstall = rollback.MixedInstall
		return result, fmt.Errorf("reconcile pending update: %w", rollbackErr)
	}
	if !rollback.RolledBack {
		// Another exact owner may have committed or cancelled the transaction
		// between the invocation snapshot and the locked transition.
		if _, currentErr := ReadPendingUpdate(); os.IsNotExist(currentErr) {
			return PendingUpdateReconcileResult{}, nil
		}
		return result, fmt.Errorf("reconcile pending update: transaction could not be cancelled or rolled back")
	}
	result.RolledBack = true
	cleanupPendingUpdateStaging(tx)
	return result, nil
}

// CommitProbationaryPendingUpdate commits a still-running probationary update
// when install evidence matches. Returns true when the marker is gone.
func CommitProbationaryPendingUpdate(runningVersion string) (bool, error) {
	if strings.TrimSpace(runningVersion) == "" {
		return false, nil
	}
	tx, err := ReadPendingUpdate()
	if err != nil {
		if os.IsNotExist(err) {
			return true, nil
		}
		return false, err
	}
	if !UpdateVersionsEqual(runningVersion, tx.ToVersion) || !pendingUpdateInstalledForHealth(tx) {
		return false, nil
	}
	if err := MarkUpdateHealthy(runningVersion); err != nil {
		return false, err
	}
	return !PendingUpdateExists(), nil
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

// forceRetireProbationaryPendingUpdate retires a probationary marker when the
// live target is installed but MarkUpdateHealthy cannot finish (e.g. bad backup).
func forceRetireProbationaryPendingUpdate(runningVersion string) (bool, error) {
	tx, err := ReadPendingUpdate()
	if err != nil {
		if os.IsNotExist(err) {
			return true, nil
		}
		return false, err
	}
	// Target-only evidence: broken rollback backups must not block discard.
	if !UpdateVersionsEqual(runningVersion, tx.ToVersion) || !pendingUpdateTargetInstalled(tx) {
		return false, nil
	}
	unlock, err := acquirePendingUpdateLock()
	if err != nil {
		return false, fmt.Errorf("force retire probationary update: lock pending transaction: %w", err)
	}
	defer unlock()
	current, err := ReadPendingUpdate()
	if err != nil {
		if os.IsNotExist(err) {
			return true, nil
		}
		return false, err
	}
	if UpdateTransactionID(current) != UpdateTransactionID(tx) {
		return false, fmt.Errorf("force retire probationary update: pending transaction changed")
	}
	if !UpdateVersionsEqual(runningVersion, current.ToVersion) || !pendingUpdateTargetInstalled(current) {
		return false, nil
	}
	unlockTargets, lockErr := lockRepairMutations(pendingUpdateTargetPaths(current)...)
	if lockErr != nil {
		return false, fmt.Errorf("force retire probationary update: lock targets: %w", lockErr)
	}
	defer unlockTargets()
	recheck, err := ReadPendingUpdate()
	if err != nil {
		if os.IsNotExist(err) {
			return true, nil
		}
		return false, err
	}
	if !reflect.DeepEqual(current, recheck) {
		return false, fmt.Errorf("force retire probationary update: pending transaction changed while waiting")
	}
	verifyInstalled := func() error {
		if !pendingUpdateTargetInstalled(recheck) {
			return fmt.Errorf("installed target no longer matches the pending transaction")
		}
		return nil
	}
	if err := removePendingUpdateExactVerified(recheck, verifyInstalled); err != nil {
		return false, err
	}
	removeUpdateBackups(recheck)
	slog.Warn("repair: force-retired a probationary pending update after explicit abandon",
		"toVersion", recheck.ToVersion, "target", recheck.TargetPath)
	return true, nil
}

// pendingUpdateTargetInstalled reports live replacement install evidence only
// (no rollback backup requirement). Used by force-retire on explicit abandon.
func pendingUpdateTargetInstalled(tx *UpdateTransaction) bool {
	if tx == nil {
		return false
	}
	switch tx.TargetKind {
	case "app-bundle":
		return VerifyAppBundleUpdateHandoffTarget(tx) == nil
	case "file":
		_, bound, err := installedFileUpdateTargets(tx, true)
		return err == nil && bound
	default:
		return false
	}
}

// pendingUpdateInstalledForHealth requires transaction-bound evidence for the
// complete replacement and rollback unit. It intentionally treats missing or
// drifted evidence as uninstalled so reconciliation can take the existing
// exact cancel/rollback paths instead of trusting a version string.
func pendingUpdateInstalledForHealth(tx *UpdateTransaction) bool {
	if tx == nil {
		return false
	}
	switch tx.TargetKind {
	case "app-bundle":
		return VerifyAppBundleUpdateHandoffTarget(tx) == nil &&
			VerifyAppBundleUpdateHandoffBackup(tx) == nil
	case "file":
		_, bound, err := installedFileUpdateTargets(tx, true)
		return err == nil && bound
	default:
		return false
	}
}

// cleanupPendingUpdateStaging is best-effort after the pending transaction has
// been safely committed away. CleanupAppBundleUpdateHandoffStaging verifies the
// complete recorded tree before removal, so drifted or recreated paths survive.
func cleanupPendingUpdateStaging(tx *UpdateTransaction) {
	if tx == nil || tx.TargetKind != "app-bundle" ||
		strings.TrimSpace(tx.HandoffStagingPath) == "" ||
		strings.TrimSpace(tx.HandoffStagingTreeID) == "" {
		return
	}
	_ = CleanupAppBundleUpdateHandoffStaging(tx)
}

func readPendingUpdateInvocation() (*UpdateTransaction, string, map[string]string, error) {
	tx, err := ReadPendingUpdate()
	if err != nil {
		return nil, "", nil, err
	}
	stateID, states := pendingUpdateBoundPreview(tx)
	return tx, stateID, states, nil
}

// MarkUpdateHealthy commits a probationary update and removes its backup. A
// version mismatch is ignored so an older process cannot bless a newer update.
func MarkUpdateHealthy(runningVersion string) error {
	return markUpdateHealthyInvocation(runningVersion, "", "")
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

// MarkUpdateHealthyExact commits only the complete transaction captured before
// the replacement process started.
func MarkUpdateHealthyExact(runningVersion, expectedCreatedAt, expectedTransactionID string) error {
	expectedCreatedAt = strings.TrimSpace(expectedCreatedAt)
	expectedTransactionID = strings.TrimSpace(expectedTransactionID)
	if expectedCreatedAt == "" || expectedTransactionID == "" {
		return nil
	}
	return markUpdateHealthyInvocation(runningVersion, expectedCreatedAt, expectedTransactionID)
}

func markUpdateHealthyInvocation(runningVersion, expectedCreatedAt, expectedTransactionID string) error {
	tx, stateID, _, err := readPendingUpdateInvocation()
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if !UpdateVersionsEqual(runningVersion, tx.ToVersion) {
		return nil
	}
	if expected := strings.TrimSpace(expectedCreatedAt); expected != "" && expected != strings.TrimSpace(tx.CreatedAt) {
		return nil
	}
	if expected := strings.TrimSpace(expectedTransactionID); expected != "" && expected != UpdateTransactionID(tx) {
		return fmt.Errorf("mark update healthy: pending transaction changed")
	}
	return markUpdateHealthyMatching(
		runningVersion,
		tx.CreatedAt,
		UpdateTransactionID(tx),
		stateID,
	)
}

func markUpdateHealthyMatching(runningVersion, expectedCreatedAt, expectedTransactionID, expectedStateID string) error {
	unlock, err := acquirePendingUpdateLock()
	if err != nil {
		return fmt.Errorf("mark update healthy: lock pending transaction: %w", err)
	}
	defer unlock()
	tx, err := ReadPendingUpdate()
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if !UpdateVersionsEqual(runningVersion, tx.ToVersion) {
		return nil
	}
	if expected := strings.TrimSpace(expectedCreatedAt); expected != "" && expected != strings.TrimSpace(tx.CreatedAt) {
		return nil
	}
	if expected := strings.TrimSpace(expectedTransactionID); expected != "" && expected != UpdateTransactionID(tx) {
		return fmt.Errorf("mark update healthy: pending transaction changed")
	}
	unlockTargets, lockErr := lockRepairMutations(pendingUpdateTargetPaths(tx)...)
	if lockErr != nil {
		return fmt.Errorf("mark update healthy: lock targets: %w", lockErr)
	}
	defer unlockTargets()
	current, err := ReadPendingUpdate()
	if err != nil {
		return fmt.Errorf("mark update healthy: re-read pending transaction: %w", err)
	}
	if !reflect.DeepEqual(tx, current) {
		return fmt.Errorf("mark update healthy: pending transaction changed while waiting")
	}
	tx = current
	verifyInvocationState := func() error {
		actual, _ := pendingUpdateBoundPreview(tx)
		if strings.TrimSpace(expectedStateID) != actual {
			return fmt.Errorf("mark update healthy: pending update state changed while waiting")
		}
		return nil
	}
	verifyHealthyState := func() error {
		if err := verifyInvocationState(); err != nil {
			return err
		}
		switch tx.TargetKind {
		case "app-bundle":
			if strings.TrimSpace(tx.HandoffAppTreeID) == "" {
				return fmt.Errorf("mark update healthy: installed bundle state is missing")
			}
			if err := VerifyAppBundleUpdateHandoffTarget(tx); err != nil {
				return fmt.Errorf("mark update healthy: %w", err)
			}
			if strings.TrimSpace(tx.BackupTreeID) == "" {
				return fmt.Errorf("mark update healthy: rollback backup state is missing")
			}
			if err := VerifyAppBundleUpdateHandoffBackup(tx); err != nil {
				return fmt.Errorf("mark update healthy: %w", err)
			}
		case "file":
			if err := verifyPreparedFileUpdateBackups(tx); err != nil {
				return fmt.Errorf("mark update healthy: %w", err)
			}
			if _, _, err := installedFileUpdateTargets(tx, true); err != nil {
				return fmt.Errorf("mark update healthy: %w", err)
			}
		}
		return nil
	}
	if err := verifyHealthyState(); err != nil {
		return err
	}
	if err := removePendingUpdateExactVerified(tx, verifyHealthyState); err != nil {
		return err
	}
	removeUpdateBackups(tx)
	_ = removeInstalledFileUpdateState(tx)
	return nil
}

// CancelPendingUpdate removes a transaction that failed before control was
// handed to the replacement build. A version mismatch is intentionally inert.
func CancelPendingUpdate(toVersion string) error {
	return cancelPendingUpdateInvocation(toVersion, "", "")
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

// CancelPendingUpdateExact removes only the complete transaction returned by
// prepare. This is the updater failure path: copied creation timestamps are not
// sufficient authorization if pending-update.json itself was rewritten.
func CancelPendingUpdateExact(expected *UpdateTransaction) error {
	if expected == nil {
		return fmt.Errorf("cancel pending update: transaction identity is incomplete")
	}
	return cancelPendingUpdateInvocation(
		expected.ToVersion,
		expected.CreatedAt,
		repairPlanStateID(expected),
	)
}

func cancelPendingUpdateInvocation(toVersion, expectedCreatedAt, expectedTransactionID string) error {
	tx, stateID, _, err := readPendingUpdateInvocation()
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if strings.TrimSpace(toVersion) != strings.TrimSpace(tx.ToVersion) {
		return nil
	}
	if expected := strings.TrimSpace(expectedCreatedAt); expected != "" && expected != strings.TrimSpace(tx.CreatedAt) {
		return nil
	}
	if expected := strings.TrimSpace(expectedTransactionID); expected != "" && expected != UpdateTransactionID(tx) {
		return fmt.Errorf("cancel pending update: pending transaction changed")
	}
	return cancelPendingUpdateMatching(
		tx.ToVersion,
		tx.CreatedAt,
		UpdateTransactionID(tx),
		stateID,
	)
}

func cancelPendingUpdateMatching(toVersion, expectedCreatedAt, expectedTransactionID, expectedStateID string) error {
	unlock, err := acquirePendingUpdateLock()
	if err != nil {
		return fmt.Errorf("cancel pending update: lock pending transaction: %w", err)
	}
	defer unlock()
	tx, err := ReadPendingUpdate()
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if strings.TrimSpace(toVersion) != strings.TrimSpace(tx.ToVersion) {
		return nil
	}
	if expected := strings.TrimSpace(expectedCreatedAt); expected != "" && expected != strings.TrimSpace(tx.CreatedAt) {
		return nil
	}
	if expected := strings.TrimSpace(expectedTransactionID); expected != "" && expected != repairPlanStateID(tx) {
		return fmt.Errorf("cancel pending update: pending transaction changed")
	}
	unlockTargets, lockErr := lockRepairMutations(pendingUpdateTargetPaths(tx)...)
	if lockErr != nil {
		return fmt.Errorf("cancel pending update: lock targets: %w", lockErr)
	}
	defer unlockTargets()
	current, err := ReadPendingUpdate()
	if err != nil {
		return fmt.Errorf("cancel pending update: re-read pending transaction: %w", err)
	}
	if !reflect.DeepEqual(tx, current) {
		return fmt.Errorf("cancel pending update: pending transaction changed while waiting")
	}
	tx = current
	verifyCancellationState := func() error {
		actual, _ := pendingUpdateBoundPreview(tx)
		if strings.TrimSpace(expectedStateID) != actual {
			return fmt.Errorf("cancel pending update: pending update state changed while waiting")
		}
		switch tx.TargetKind {
		case "app-bundle":
			if err := VerifyAppBundleUpdateHandoffOriginal(tx); err != nil {
				return fmt.Errorf("cancel pending update: %w", err)
			}
			if err := verifyAppBundleUpdateHandoffBackupAbsent(tx); err != nil {
				return fmt.Errorf("cancel pending update: %w", err)
			}
		case "file":
			if _, bound, err := installedFileUpdateTargets(tx, false); err != nil {
				return fmt.Errorf("cancel pending update: %w", err)
			} else if bound {
				return fmt.Errorf("cancel pending update: installed release-unit state is already recorded")
			}
			if err := verifyPreparedFileUpdateTargets(tx); err != nil {
				return fmt.Errorf("cancel pending update: %w", err)
			}
		default:
			return fmt.Errorf("cancel pending update: unsupported target kind %q", tx.TargetKind)
		}
		return nil
	}
	if err := verifyCancellationState(); err != nil {
		return err
	}
	if err := removePendingUpdateExactVerified(tx, verifyCancellationState); err != nil {
		return err
	}
	if tx.TargetKind == "file" {
		removeUpdateBackups(tx)
	}
	return nil
}

func removeUpdateBackups(tx *UpdateTransaction) {
	if tx == nil {
		return
	}
	if tx.TargetKind == "app-bundle" {
		_ = removeUpdateBackupTreeMatching(tx.BackupPath, tx.BackupTreeID)
		return
	}
	seen := map[string]struct{}{}
	for _, f := range pendingUpdateFiles(tx) {
		if f.MissingBefore || strings.TrimSpace(f.BackupPath) == "" {
			continue
		}
		key := canonicalRepairPath(f.BackupPath)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		_ = removeUpdateBackupFileMatching(f.BackupPath, f.SHA256)
	}
}

func removeUpdateBackupFileMatching(path, expectedSHA256 string) error {
	expectedSHA256 = strings.TrimSpace(expectedSHA256)
	if path == "" || expectedSHA256 == "" {
		return nil
	}
	return removeUpdateNodeMatching(path, func(moved string) error {
		info, err := os.Lstat(moved)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("update backup changed type")
		}
		actual, err := hashFile(moved)
		if err != nil {
			return err
		}
		if !strings.EqualFold(actual, expectedSHA256) {
			return fmt.Errorf("update backup hash changed")
		}
		return nil
	}, false)
}

func removeUpdateBackupTreeMatching(path, expectedTreeID string) error {
	expectedTreeID = strings.TrimSpace(expectedTreeID)
	if path == "" || expectedTreeID == "" {
		return nil
	}
	return removeUpdateNodeMatching(path, func(moved string) error {
		actual, err := repairPlanTreeContentStateID(moved)
		if err != nil {
			return err
		}
		if actual != expectedTreeID {
			return fmt.Errorf("update backup tree changed")
		}
		return nil
	}, true)
}

func removeUpdateNodeMatching(path string, verify func(string) error, directory bool) error {
	cleanup, err := moveRepairNodeToUniqueCleanup(path)
	if err != nil || cleanup == "" {
		return err
	}
	updateCleanupAfterRename(path, cleanup)
	restore := func(cause error) error {
		if restoreErr := renameRepairNodeNoReplace(cleanup, path); restoreErr != nil {
			return fmt.Errorf("%w; changed update node retained at %s: %w", cause, cleanup, restoreErr)
		}
		return cause
	}
	if err := verify(cleanup); err != nil {
		return restore(err)
	}
	if directory {
		return os.RemoveAll(cleanup)
	}
	if err := os.Remove(cleanup); err != nil {
		return restore(err)
	}
	return nil
}

func RollbackPendingUpdate() (UpdateRollbackResult, error) {
	return rollbackPendingUpdateInvocation("", "", "")
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

func rollbackPendingUpdateState(expectedStateID string, expectedStates map[string]string) (UpdateRollbackResult, error) {
	return rollbackPendingUpdateMatching("", "", expectedStateID, expectedStates, "", true)
}

// RollbackPendingUpdateExact restores only the complete transaction returned by
// prepare. It is used after a platform apply failure where a later same-version
// transaction must remain untouched.
func RollbackPendingUpdateExact(expected *UpdateTransaction) (UpdateRollbackResult, error) {
	if expected == nil {
		return UpdateRollbackResult{}, fmt.Errorf("rollback update: transaction identity is incomplete")
	}
	return rollbackPendingUpdateInvocation(
		expected.ToVersion,
		expected.CreatedAt,
		repairPlanStateID(expected),
	)
}

func rollbackPendingUpdateInvocation(
	expectedToVersion, expectedCreatedAt, expectedTransactionID string,
) (UpdateRollbackResult, error) {
	tx, stateID, states, err := readPendingUpdateInvocation()
	if err != nil {
		if os.IsNotExist(err) {
			return UpdateRollbackResult{}, nil
		}
		return UpdateRollbackResult{}, err
	}
	if expected := strings.TrimSpace(expectedToVersion); expected != "" && expected != strings.TrimSpace(tx.ToVersion) {
		return UpdateRollbackResult{}, nil
	}
	if expected := strings.TrimSpace(expectedCreatedAt); expected != "" && expected != strings.TrimSpace(tx.CreatedAt) {
		return UpdateRollbackResult{}, nil
	}
	if expected := strings.TrimSpace(expectedTransactionID); expected != "" && expected != UpdateTransactionID(tx) {
		return UpdateRollbackResult{}, fmt.Errorf("rollback update: pending transaction changed")
	}
	return rollbackPendingUpdateMatching(
		tx.ToVersion,
		tx.CreatedAt,
		stateID,
		states,
		UpdateTransactionID(tx),
		false,
	)
}

func rollbackPendingUpdateMatching(
	expectedToVersion, expectedCreatedAt, expectedStateID string,
	expectedStates map[string]string,
	expectedTransactionID string,
	callerConfirmedState bool,
) (UpdateRollbackResult, error) {
	// The expected-match checks below re-run under the strict lock, so a
	// transaction committed, cancelled, or replaced while waiting here is never
	// acted upon.
	unlock, err := acquirePendingUpdateLock()
	if err != nil {
		return UpdateRollbackResult{}, fmt.Errorf("rollback update: lock pending transaction: %w", err)
	}
	defer unlock()
	return rollbackPendingUpdateMatchingLocked(
		expectedToVersion,
		expectedCreatedAt,
		expectedStateID,
		expectedStates,
		expectedTransactionID,
		callerConfirmedState,
	)
}

// rollbackPendingUpdateMatchingLocked performs the transition while the caller
// holds the pending-update lock. RecoverFailedInstall uses this form so failure
// marker correlation, rollback, and marker cleanup are one serialized state
// transition.
func rollbackPendingUpdateMatchingLocked(
	expectedToVersion, expectedCreatedAt, expectedStateID string,
	expectedStates map[string]string,
	expectedTransactionID string,
	callerConfirmedState bool,
) (UpdateRollbackResult, error) {
	tx, err := ReadPendingUpdate()
	if err != nil {
		if os.IsNotExist(err) {
			return UpdateRollbackResult{}, nil
		}
		return UpdateRollbackResult{}, err
	}
	if expected := strings.TrimSpace(expectedToVersion); expected != "" && expected != strings.TrimSpace(tx.ToVersion) {
		return UpdateRollbackResult{}, nil
	}
	if expected := strings.TrimSpace(expectedCreatedAt); expected != "" && expected != strings.TrimSpace(tx.CreatedAt) {
		return UpdateRollbackResult{}, nil
	}
	if expected := strings.TrimSpace(expectedTransactionID); expected != "" && expected != repairPlanStateID(tx) {
		return UpdateRollbackResult{}, fmt.Errorf("rollback update: pending transaction changed")
	}
	hasBoundState := strings.TrimSpace(expectedStateID) != ""
	if !hasBoundState {
		expectedStateID, expectedStates = pendingUpdateBoundPreview(tx)
	}
	// Share release-unit target locks with other repair mutations so two
	// REASONIX_HOME profiles cannot quarantine or restore the same binaries
	// through different pending-update locks.
	unlockTargets, lockErr := lockRepairMutations(pendingUpdateTargetPaths(tx)...)
	if lockErr != nil {
		return UpdateRollbackResult{}, fmt.Errorf("rollback update: lock targets: %w", lockErr)
	}
	defer unlockTargets()
	current, err := ReadPendingUpdate()
	if err != nil {
		return UpdateRollbackResult{}, fmt.Errorf("rollback update: re-read pending transaction: %w", err)
	}
	if !reflect.DeepEqual(tx, current) {
		return UpdateRollbackResult{}, fmt.Errorf("rollback update: pending transaction changed while waiting")
	}
	tx = current
	verifyBoundState := func() error {
		expected := strings.TrimSpace(expectedStateID)
		if expected == "" {
			return nil
		}
		actual, _ := pendingUpdateBoundPreview(tx)
		if expected != actual {
			return fmt.Errorf("repair plan preview changed since confirmation; re-preview and re-confirm (expected %s, got %s)", expected, actual)
		}
		return nil
	}
	if expected := strings.TrimSpace(expectedStateID); expected != "" {
		if err := verifyBoundState(); err != nil {
			if callerConfirmedState {
				return UpdateRollbackResult{}, nil
			}
			return UpdateRollbackResult{}, err
		}
	}
	confirmedStates := expectedStates
	if !callerConfirmedState {
		// Invocation-local binding proves that the live unit did not drift while
		// this rollback waited for locks. It does not prove that an unbound live
		// node belongs to the pending transaction and therefore cannot authorize
		// deleting the retained aside after restore.
		confirmedStates = nil
	}
	result := UpdateRollbackResult{FromVersion: tx.ToVersion, ToVersion: tx.FromVersion, TargetPath: tx.TargetPath}
	var verifyCommitState func() error
	switch tx.TargetKind {
	case "file":
		files, _, installedErr := installedFileUpdateTargets(tx, false)
		if installedErr != nil {
			return result, fmt.Errorf("rollback update: %w", installedErr)
		}
		// Verify every backup before touching any binary: a partial restore
		// would recreate exactly the mixed-version install rollback exists to
		// prevent. A missing hash is a validation failure, not a bypass —
		// ReadPendingUpdate already rejects hashless file transactions, so
		// this guards hand-crafted callers.
		for _, f := range files {
			if f.MissingBefore {
				continue
			}
			if strings.TrimSpace(f.SHA256) == "" {
				return result, fmt.Errorf("rollback update: backup hash missing for %s", filepath.Base(f.TargetPath))
			}
			got, hashErr := hashFile(f.BackupPath)
			if hashErr != nil || !strings.EqualFold(got, f.SHA256) {
				return result, fmt.Errorf("rollback update: backup hash mismatch for %s", filepath.Base(f.TargetPath))
			}
		}
		mixed, restoreErr := restoreReleaseUnit(files, verifyBoundState, confirmedStates)
		if restoreErr != nil {
			result.MixedInstall = mixed
			return result, fmt.Errorf("rollback update: %w", restoreErr)
		}
		verifyCommitState = func() error {
			return verifyRestoredFileUpdateTargets(files)
		}
	case "app-bundle":
		confirmedBackupState := strings.TrimSpace(confirmedStates[tx.BackupPath])
		if strings.TrimSpace(tx.BackupTreeID) == "" &&
			(!callerConfirmedState || confirmedBackupState == "") {
			return result, fmt.Errorf("rollback update: backup bundle identity is missing; explicit preview confirmation is required")
		}
		backupInfo, err := os.Lstat(tx.BackupPath)
		if err != nil {
			if os.IsNotExist(err) && strings.TrimSpace(tx.BackupTreeID) != "" {
				actual, digestErr := repairPlanTreeContentStateID(tx.TargetPath)
				if digestErr == nil && actual == tx.BackupTreeID {
					result.RolledBack = true
					if removeErr := removePendingUpdateExactVerified(tx, func() error {
						current, currentErr := repairPlanTreeContentStateID(tx.TargetPath)
						if currentErr != nil || current != tx.BackupTreeID {
							return fmt.Errorf("rollback update: restored bundle changed before commit")
						}
						return nil
					}); removeErr != nil {
						return result, fmt.Errorf("rollback update: clear pending transaction: %w", removeErr)
					}
					return result, nil
				}
			}
			return result, fmt.Errorf("rollback update: backup bundle: %w", err)
		}
		if !backupInfo.IsDir() {
			return result, fmt.Errorf("rollback update: backup bundle is not a directory")
		}
		if tx.BackupTreeID != "" {
			actual, digestErr := repairPlanTreeContentStateID(tx.BackupPath)
			if digestErr != nil || actual != tx.BackupTreeID {
				return result, fmt.Errorf("rollback update: backup bundle digest mismatch")
			}
		} else if err := verifyRepairPlanReleaseNodeStateFor(
			tx.BackupPath,
			tx.BackupPath,
			confirmedBackupState,
		); err != nil {
			return result, fmt.Errorf("rollback update: confirmed backup bundle changed: %w", err)
		}
		if err := verifyBoundState(); err != nil {
			return result, err
		}
		failed := ""
		retainedFailed := false
		retainedFailedOwned := false
		retainedFailedState := ""
		if _, statErr := os.Lstat(tx.TargetPath); statErr == nil {
			retainedFailedState = repairPlanReleaseNodeState(tx.TargetPath)
			var retainErr error
			failed, retainErr = retainUpdateRollbackNode(tx.TargetPath, "reasonix-failed")
			if retainErr != nil {
				return result, fmt.Errorf("rollback update: move failed bundle: %w", retainErr)
			}
			retainedFailed = true
			if verifyErr := verifyRepairPlanReleaseNodeStateFor(failed, tx.TargetPath, retainedFailedState); verifyErr != nil {
				if restoreErr := rollbackSwapRename(failed, tx.TargetPath); restoreErr != nil {
					result.MixedInstall = true
					return result, fmt.Errorf("%w; preserve moved live bundle at %s: %w", verifyErr, failed, restoreErr)
				}
				return result, verifyErr
			}
			if strings.TrimSpace(tx.HandoffAppTreeID) != "" {
				retainedFailedOwned = VerifyAppBundleUpdateHandoffReplacement(tx, failed) == nil
			}
			if expected := confirmedStates[tx.TargetPath]; expected != "" {
				if verifyErr := verifyRepairPlanReleaseNodeStateFor(failed, tx.TargetPath, expected); verifyErr != nil {
					if restoreErr := rollbackSwapRename(failed, tx.TargetPath); restoreErr != nil {
						result.MixedInstall = true
						return result, fmt.Errorf("%w; preserve moved live bundle at %s: %w", verifyErr, failed, restoreErr)
					}
					return result, verifyErr
				}
				retainedFailedOwned = true
			}
		} else if !os.IsNotExist(statErr) {
			return result, fmt.Errorf("rollback update: inspect live bundle: %w", statErr)
		}
		if _, statErr := os.Lstat(tx.TargetPath); statErr == nil {
			result.MixedInstall = retainedFailed
			return result, fmt.Errorf("rollback update: target bundle was recreated before restore")
		} else if !os.IsNotExist(statErr) {
			result.MixedInstall = retainedFailed
			return result, fmt.Errorf("rollback update: inspect restore target: %w", statErr)
		}
		if err := rollbackSwapRename(tx.BackupPath, tx.TargetPath); err != nil {
			if retainedFailed {
				if verifyErr := verifyRepairPlanReleaseNodeStateFor(failed, tx.TargetPath, retainedFailedState); verifyErr != nil {
					result.MixedInstall = true
					return result, fmt.Errorf("rollback update: restore bundle: %w (retained live bundle changed at %s: %w)", err, failed, verifyErr)
				}
				if restoreErr := rollbackSwapRename(failed, tx.TargetPath); restoreErr != nil {
					result.MixedInstall = true
					return result, fmt.Errorf("rollback update: restore bundle: %w (preserve replacement at %s: %w)", err, failed, restoreErr)
				}
			}
			return result, fmt.Errorf("rollback update: restore bundle: %w", err)
		}
		restoredTreeID, digestErr := repairPlanTreeContentStateID(tx.TargetPath)
		restoredMatches := digestErr == nil
		if strings.TrimSpace(tx.BackupTreeID) != "" {
			restoredMatches = restoredMatches && restoredTreeID == tx.BackupTreeID
		} else if restoredMatches {
			restoredMatches = verifyRepairPlanReleaseNodeStateFor(
				tx.TargetPath,
				tx.BackupPath,
				confirmedBackupState,
			) == nil
		}
		if !restoredMatches {
			mismatchErr := fmt.Errorf("rollback update: restored bundle digest mismatch")
			rejected, moveErr := moveRepairNodeToUniqueCleanup(tx.TargetPath)
			if moveErr != nil || rejected == "" {
				result.MixedInstall = true
				if moveErr != nil {
					return result, fmt.Errorf("%w; retain rejected bundle: %w", mismatchErr, moveErr)
				}
				return result, fmt.Errorf("%w; rejected bundle disappeared before compensation", mismatchErr)
			}
			if !retainedFailed {
				result.MixedInstall = true
				return result, fmt.Errorf("%w; rejected bundle retained at %s and no prior live bundle is available", mismatchErr, rejected)
			}
			if verifyErr := verifyRepairPlanReleaseNodeStateFor(failed, tx.TargetPath, retainedFailedState); verifyErr != nil {
				result.MixedInstall = true
				return result, fmt.Errorf("%w; rejected bundle retained at %s; prior live bundle changed at %s: %w", mismatchErr, rejected, failed, verifyErr)
			}
			if restoreErr := rollbackSwapRename(failed, tx.TargetPath); restoreErr != nil {
				result.MixedInstall = true
				return result, fmt.Errorf("%w; rejected bundle retained at %s; restore prior live bundle: %w", mismatchErr, rejected, restoreErr)
			}
			if verifyErr := verifyRepairPlanReleaseNodeStateFor(tx.TargetPath, tx.TargetPath, retainedFailedState); verifyErr != nil {
				result.MixedInstall = true
				return result, fmt.Errorf("%w; rejected bundle retained at %s; restored prior live bundle changed: %w", mismatchErr, rejected, verifyErr)
			}
			return result, fmt.Errorf("%w; rejected bundle retained at %s and prior live bundle restored", mismatchErr, rejected)
		}
		verifyCommitState = func() error {
			current, currentErr := repairPlanTreeContentStateID(tx.TargetPath)
			if currentErr != nil {
				return fmt.Errorf("rollback update: read restored bundle before commit: %w", currentErr)
			}
			if current != restoredTreeID {
				return fmt.Errorf("rollback update: restored bundle changed before commit")
			}
			return nil
		}
		if retainedFailed && retainedFailedOwned {
			_ = removeUpdateNodeMatching(failed, func(moved string) error {
				return verifyRepairPlanReleaseNodeStateFor(moved, tx.TargetPath, retainedFailedState)
			}, true)
		}
	default:
		return result, fmt.Errorf("rollback update: unsupported target kind %q", tx.TargetKind)
	}
	if err := verifyCommitState(); err != nil {
		return result, err
	}
	result.RolledBack = true
	if err := removePendingUpdateExactVerified(tx, verifyCommitState); err != nil {
		return result, fmt.Errorf("rollback update: clear pending transaction: %w", err)
	}
	if tx.TargetKind == "file" {
		_ = removeInstalledFileUpdateState(tx)
	}
	return result, nil
}

func retainUpdateRollbackNode(path, suffix string) (string, error) {
	for attempt := range 16 {
		retained := fmt.Sprintf(
			"%s.%s-%d-%d",
			path,
			suffix,
			time.Now().UTC().UnixNano(),
			attempt,
		)
		if err := rollbackSwapRename(path, retained); err != nil {
			if os.IsExist(err) {
				continue
			}
			return "", err
		}
		return retained, nil
	}
	return "", fmt.Errorf("cannot allocate retained update path")
}

func verifyRestoredFileUpdateTargets(files []UpdateTransactionFile) error {
	for _, f := range files {
		info, err := os.Lstat(f.TargetPath)
		if f.MissingBefore {
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return fmt.Errorf("verify restored release unit %s: %w", filepath.Base(f.TargetPath), err)
			}
			return fmt.Errorf("verify restored release unit %s: unexpected file appeared", filepath.Base(f.TargetPath))
		}
		if err != nil {
			return fmt.Errorf("verify restored release unit %s: %w", filepath.Base(f.TargetPath), err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("verify restored release unit %s: file changed type", filepath.Base(f.TargetPath))
		}
		got, hashErr := hashFile(f.TargetPath)
		if hashErr != nil {
			return fmt.Errorf("verify restored release unit %s: %w", filepath.Base(f.TargetPath), hashErr)
		}
		if !strings.EqualFold(got, f.SHA256) {
			return fmt.Errorf("verify restored release unit %s: hash mismatch", filepath.Base(f.TargetPath))
		}
	}
	return nil
}

// Rename/copy indirection so tests can inject mid-unit failures.
var (
	rollbackStageCopy          = copyFileWithHashCreate
	rollbackPublishStage       = renameRepairNodeNoReplace
	rollbackSwapRename         = renameRepairNodeNoReplace
	removePendingUpdateFile    = os.Remove
	pendingUpdateBeforeCleanup = func(string) {}
	updateCleanupAfterRename   = func(string, string) {}
	fileUpdateAfterRetain      = func(string, string) {}
	installedUpdateAfterCreate = func(string) {}
)

// restoreReleaseUnit swaps every backup into place with compensation, so a
// failed rollback never leaves a mixed old/new install. Phase 1 stages each
// backup next to its target — a copy can fail halfway (disk full, unreadable
// backup) and staging keeps the live binaries untouched until every byte is
// on the target filesystem. Phase 2 swaps via renames only: each target moves
// aside first (renaming works even for the running executable, where
// overwriting does not), so a failure renames the asides back and the unit
// stays coherent on the new version for a retried rollback. Only when that
// unwinding itself fails is the install reported as mixed.
func restoreReleaseUnit(
	files []UpdateTransactionFile,
	verifyBeforeSwap func() error,
	expectedStates map[string]string,
) (mixed bool, err error) {
	stages := make([]string, len(files))
	defer func() {
		for i, stage := range stages {
			if stage != "" {
				_ = removeUpdateBackupFileMatching(stage, files[i].SHA256)
			}
		}
	}()
	for i, f := range files {
		if f.MissingBefore {
			continue
		}
		mode := os.FileMode(0o700)
		if st, statErr := os.Stat(f.TargetPath); statErr == nil {
			mode = st.Mode().Perm()
		}
		stage, stagedSHA256, copyErr := stageUpdateRollbackBackup(f, mode)
		if copyErr != nil {
			return false, fmt.Errorf("stage %s: %w", filepath.Base(f.TargetPath), copyErr)
		}
		stages[i] = stage
		// The backup can change after the preflight hash but before or during
		// this copy. Bind the bytes that will actually be installed, not only
		// the source path observed before staging.
		if !strings.EqualFold(stagedSHA256, f.SHA256) {
			return false, fmt.Errorf("stage %s: backup hash mismatch", filepath.Base(f.TargetPath))
		}
	}
	if verifyBeforeSwap != nil {
		if err := verifyBeforeSwap(); err != nil {
			return false, err
		}
	}
	// A crash can leave an old-version target beside the retained new-version
	// aside after the stage-to-target rename. Recognize that exact state before
	// touching any entry so a retry preserves the aside for compensation instead
	// of overwriting it with the already-restored target.
	alreadyRestored := make([]bool, len(files))
	preexistingAside := make([]bool, len(files))
	for i, f := range files {
		aside := f.TargetPath + ".reasonix-rollback-aside"
		asideInfo, err := os.Lstat(aside)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return false, fmt.Errorf("inspect retained %s: %w", filepath.Base(f.TargetPath), err)
		}
		if !asideInfo.Mode().IsRegular() {
			return false, fmt.Errorf("ambiguous rollback state for %s", filepath.Base(f.TargetPath))
		}
		preexistingAside[i] = true
		if _, err := os.Lstat(f.TargetPath); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return false, fmt.Errorf("inspect restored %s: %w", filepath.Base(f.TargetPath), err)
		}
		if f.MissingBefore {
			return false, fmt.Errorf("ambiguous rollback state for %s", filepath.Base(f.TargetPath))
		}
		got, err := hashFile(f.TargetPath)
		if err != nil || !strings.EqualFold(got, f.SHA256) {
			return false, fmt.Errorf("ambiguous rollback state for %s", filepath.Base(f.TargetPath))
		}
		alreadyRestored[i] = true
	}
	asides := make([]string, len(files))
	retainedStates := make([]string, len(files))
	processed := make([]bool, len(files))
	restoreAttempted := make([]bool, len(files))
	preserveAside := make([]bool, len(files))
	ownedRetained := make([]bool, len(files))
	publishedStates := make([]string, len(files))
	failedIndex := -1
	var swapErr error
	for i, f := range files {
		aside := f.TargetPath + ".reasonix-rollback-aside"
		if alreadyRestored[i] {
			asides[i] = aside
			processed[i] = true
			continue
		}
		retainedState := repairPlanReleaseNodeState(f.TargetPath)
		if renameErr := rollbackSwapRename(f.TargetPath, aside); renameErr != nil {
			if os.IsNotExist(renameErr) {
				// A rollback interrupted between renames may have consumed this
				// target while retaining the new binary at the fixed aside path.
				// Preserve that copy for compensation until the retry succeeds.
				if f.MissingBefore {
					aside = ""
				} else if _, statErr := os.Lstat(aside); statErr != nil {
					aside = ""
				}
			} else {
				failedIndex = i
				swapErr = fmt.Errorf("retain %s: %w", filepath.Base(f.TargetPath), renameErr)
				break
			}
		}
		asides[i] = aside
		if aside != "" && !preexistingAside[i] {
			if verifyErr := verifyRepairPlanReleaseNodeStateFor(aside, f.TargetPath, retainedState); verifyErr != nil {
				failedIndex = i
				swapErr = verifyErr
				if restoreErr := restoreRepairNodeIfAbsent(aside, f.TargetPath); restoreErr != nil {
					preserveAside[i] = true
					swapErr = fmt.Errorf("%w; preserve moved live target at %s: %w", verifyErr, aside, restoreErr)
				} else {
					asides[i] = ""
				}
				break
			}
			retainedStates[i] = retainedState
			if installedState := strings.TrimSpace(f.InstalledStateID); installedState != "" {
				if verifyErr := verifyRepairPlanReleaseNodeStateFor(aside, f.TargetPath, installedState); verifyErr != nil {
					failedIndex = i
					swapErr = fmt.Errorf("installed release file %s changed before rollback: %w", filepath.Base(f.TargetPath), verifyErr)
					if restoreErr := restoreRepairNodeIfAbsent(aside, f.TargetPath); restoreErr != nil {
						preserveAside[i] = true
						swapErr = fmt.Errorf("%w; preserve moved live target at %s: %w", swapErr, aside, restoreErr)
					} else {
						asides[i] = ""
					}
					break
				}
				ownedRetained[i] = true
			}
		}
		if expected := expectedStates[f.TargetPath]; expected != "" && aside != "" {
			if verifyErr := verifyRepairPlanStateIDFor(aside, f.TargetPath, expected); verifyErr != nil {
				failedIndex = i
				swapErr = verifyErr
				if restoreErr := restoreRepairNodeIfAbsent(aside, f.TargetPath); restoreErr != nil {
					preserveAside[i] = true
					swapErr = fmt.Errorf("%w; preserve moved live target at %s: %w", verifyErr, aside, restoreErr)
				} else {
					asides[i] = ""
				}
				break
			}
			ownedRetained[i] = true
		}
		if f.MissingBefore {
			// The old release did not contain this path. Retaining the new file
			// at the aside path removes it from the live release atomically; it
			// is deleted only after the whole rollback succeeds.
			processed[i] = true
			continue
		}
		restoreAttempted[i] = true
		// Stage and target share a filesystem. A no-replace rename publishes the
		// fully verified bytes atomically, consumes the writable staging alias,
		// and refuses to overwrite a target recreated after the confirmed node
		// moved aside.
		if publishErr := rollbackPublishStage(stages[i], f.TargetPath); publishErr != nil {
			failedIndex = i
			swapErr = fmt.Errorf("restore %s: %w", filepath.Base(f.TargetPath), publishErr)
			break
		}
		stages[i] = ""
		publishedStates[i] = repairPlanReleaseNodeState(f.TargetPath)
		processed[i] = true
		publishedHash, hashErr := hashFile(f.TargetPath)
		if hashErr != nil || !strings.EqualFold(publishedHash, f.SHA256) {
			failedIndex = i
			if hashErr != nil {
				swapErr = fmt.Errorf("verify restored %s: %w", filepath.Base(f.TargetPath), hashErr)
			} else {
				swapErr = fmt.Errorf("verify restored %s: hash mismatch", filepath.Base(f.TargetPath))
			}
			break
		}
	}
	if swapErr == nil {
		for _, f := range files {
			info, verifyErr := os.Lstat(f.TargetPath)
			if f.MissingBefore {
				if os.IsNotExist(verifyErr) {
					continue
				}
				if verifyErr != nil {
					swapErr = fmt.Errorf("verify restored release unit %s: %w", filepath.Base(f.TargetPath), verifyErr)
				} else {
					swapErr = fmt.Errorf("verify restored release unit %s: unexpected file appeared", filepath.Base(f.TargetPath))
				}
				break
			}
			if verifyErr != nil {
				swapErr = fmt.Errorf("verify restored release unit %s: %w", filepath.Base(f.TargetPath), verifyErr)
				break
			}
			if !info.Mode().IsRegular() {
				swapErr = fmt.Errorf("verify restored release unit %s: file changed type", filepath.Base(f.TargetPath))
				break
			}
			got, hashErr := hashFile(f.TargetPath)
			if hashErr != nil || !strings.EqualFold(got, f.SHA256) {
				if hashErr != nil {
					swapErr = fmt.Errorf("verify restored release unit %s: %w", filepath.Base(f.TargetPath), hashErr)
				} else {
					swapErr = fmt.Errorf("verify restored release unit %s: hash mismatch", filepath.Base(f.TargetPath))
				}
				break
			}
		}
	}
	if swapErr == nil {
		for i, f := range files {
			// Best-effort: on Windows the running executable's aside may linger
			// until the process exits, but it is no longer a live entry point.
			aside := f.TargetPath + ".reasonix-rollback-aside"
			if !preexistingAside[i] && retainedStates[i] != "" && ownedRetained[i] {
				_ = removeUpdateNodeMatching(aside, func(moved string) error {
					return verifyRepairPlanReleaseNodeStateFor(moved, f.TargetPath, retainedStates[i])
				}, false)
			}
		}
		return false, nil
	}
	// Compensate: rename the new-version binaries back over the restored old
	// ones. A missing-before entry is compensated the same way: move the
	// retained new file back to its original path.
	for j, f := range files {
		if !processed[j] && j != failedIndex {
			continue
		}
		if preserveAside[j] {
			mixed = true
			continue
		}
		if preexistingAside[j] {
			// An aside inherited from a crashed process has no durable content
			// binding. Never move it back into an executable path during
			// compensation; leave recovery material in place and fail closed.
			mixed = true
			continue
		}
		if asides[j] != "" {
			if retainedStates[j] != "" {
				if verifyErr := verifyRepairPlanReleaseNodeStateFor(asides[j], f.TargetPath, retainedStates[j]); verifyErr != nil {
					mixed = true
					continue
				}
			}
			if _, statErr := os.Lstat(f.TargetPath); statErr == nil {
				// Atomically displace and verify only bytes this rollback
				// published. Anything else is restored or retained.
				if f.MissingBefore || publishedStates[j] == "" {
					mixed = true
					continue
				}
				if removeErr := removeUpdateNodeMatching(f.TargetPath, func(moved string) error {
					return verifyRepairPlanReleaseNodeStateFor(moved, f.TargetPath, publishedStates[j])
				}, false); removeErr != nil {
					mixed = true
					continue
				}
			} else if !os.IsNotExist(statErr) {
				mixed = true
				continue
			}
			if restoreErr := restoreRepairNodeIfAbsent(asides[j], f.TargetPath); restoreErr != nil {
				mixed = true
			}
			continue
		}
		if !f.MissingBefore && restoreAttempted[j] {
			// No retained new-version copy exists to put back after the old
			// backup was (or may have been) placed.
			mixed = true
		}
	}
	return mixed, swapErr
}

func stageUpdateRollbackBackup(
	file UpdateTransactionFile,
	mode os.FileMode,
) (string, string, error) {
	for attempt := range 16 {
		stage := fmt.Sprintf(
			"%s.reasonix-rollback-stage-%d-%d",
			file.TargetPath,
			time.Now().UTC().UnixNano(),
			attempt,
		)
		stagedSHA256, err := rollbackStageCopy(file.BackupPath, stage, mode)
		if err == nil {
			return stage, stagedSHA256, nil
		}
		if os.IsExist(err) {
			continue
		}
		return "", "", err
	}
	return "", "", fmt.Errorf("cannot allocate rollback staging path")
}

// allowedUpdateTargetBase whitelists the packaged binaries an update
// transaction may name. The main executable names are only valid as the
// primary target; Guard/launcher artifacts only as release-unit siblings.
func allowedUpdateTargetBase(base string, primary bool) bool {
	switch strings.ToLower(base) {
	case "reasonix-desktop", "reasonix-desktop.exe":
		return primary
	case "reasonix.exe":
		return !primary
	case "reasonix", "reasonix-guard", "reasonix-guard.exe", "reasonix-launcher.exe", "reasonix-update-helper.exe", "reasonix-cli.exe":
		return !primary
	default:
		return false
	}
}

func validateUpdateTransaction(tx *UpdateTransaction) error {
	if tx == nil || tx.SchemaVersion != updateTransactionVersion || strings.TrimSpace(tx.ToVersion) == "" {
		return fmt.Errorf("pending update metadata is incomplete")
	}
	launcher, err := repairExecutable()
	if err != nil {
		return fmt.Errorf("pending update launcher path is unavailable")
	}
	return validateUpdateTransactionForLauncher(tx, launcher)
}

func validateUpdateTransactionForLauncher(tx *UpdateTransaction, launcher string) error {
	if tx == nil || tx.SchemaVersion != updateTransactionVersion || strings.TrimSpace(tx.ToVersion) == "" {
		return fmt.Errorf("pending update metadata is incomplete")
	}
	if strings.TrimSpace(tx.Platform) == "" || strings.TrimSpace(tx.CreatedAt) == "" {
		return fmt.Errorf("pending update transaction identity is incomplete")
	}
	if _, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(tx.CreatedAt)); err != nil {
		return fmt.Errorf("pending update creation identity is invalid")
	}
	tx.TargetPath = filepath.Clean(tx.TargetPath)
	tx.BackupPath = filepath.Clean(tx.BackupPath)
	launcher = filepath.Clean(strings.TrimSpace(launcher))
	if launcher == "" || launcher == "." {
		return fmt.Errorf("pending update launcher path is unavailable")
	}
	if resolved, resolveErr := filepath.EvalSymlinks(launcher); resolveErr == nil {
		launcher = resolved
	}
	launcher = filepath.Clean(launcher)
	switch tx.TargetKind {
	case "file":
		if !allowedUpdateTargetBase(filepath.Base(tx.TargetPath), true) {
			return fmt.Errorf("pending update target is not a Reasonix executable")
		}
		launcherKey := canonicalRepairPath(launcher)
		targetKey := canonicalRepairPath(tx.TargetPath)
		if launcherKey == "" || targetKey == "" || filepath.Dir(launcherKey) != filepath.Dir(targetKey) {
			return fmt.Errorf("%w: pending update target is outside the current Guard installation", errPendingUpdateForeignInstall)
		}
		root := filepath.Clean(filepath.Join(config.MemoryUserDir(), "repair"))
		insideRepairDir := func(path string) bool {
			return pathInsideResolvedRoot(root, path)
		}
		if !insideRepairDir(tx.BackupPath) {
			return fmt.Errorf("pending update backup is outside the repair directory")
		}
		// Every restorable file must carry a hash — rollback promises to
		// verify all backups before touching any binary, so an unhashed entry
		// would silently weaken that gate.
		if strings.TrimSpace(tx.BackupSHA256) == "" {
			return fmt.Errorf("pending update backup hash is missing")
		}
		primaryListed := len(tx.Files) == 0
		seenTargets := make(map[string]struct{}, len(tx.Files))
		seenBackups := make(map[string]struct{}, len(tx.Files))
		for i := range tx.Files {
			f := &tx.Files[i]
			f.TargetPath = filepath.Clean(f.TargetPath)
			targetIdentity := canonicalRepairPath(f.TargetPath)
			if targetIdentity == "" {
				return fmt.Errorf("pending update release file path is invalid")
			}
			if _, duplicate := seenTargets[targetIdentity]; duplicate {
				return fmt.Errorf("pending update lists a duplicate release file")
			}
			seenTargets[targetIdentity] = struct{}{}
			primary := f.TargetPath == tx.TargetPath
			primaryListed = primaryListed || primary
			if !allowedUpdateTargetBase(filepath.Base(f.TargetPath), primary) {
				return fmt.Errorf("pending update lists an unexpected release file")
			}
			if filepath.Dir(f.TargetPath) != filepath.Dir(tx.TargetPath) {
				return fmt.Errorf("%w: pending update release file is outside the current Guard installation", errPendingUpdateForeignInstall)
			}
			if f.MissingBefore {
				if primary || strings.TrimSpace(f.BackupPath) != "" || strings.TrimSpace(f.SHA256) != "" {
					return fmt.Errorf("pending update missing release file metadata is invalid")
				}
				continue
			}
			f.BackupPath = filepath.Clean(f.BackupPath)
			if !insideRepairDir(f.BackupPath) {
				return fmt.Errorf("pending update backup is outside the repair directory")
			}
			if strings.TrimSpace(f.SHA256) == "" {
				return fmt.Errorf("pending update release file hash is missing")
			}
			backupIdentity := canonicalRepairPath(f.BackupPath)
			if backupIdentity == "" {
				return fmt.Errorf("pending update backup path is invalid")
			}
			if _, duplicate := seenBackups[backupIdentity]; duplicate {
				return fmt.Errorf("pending update lists a duplicate release backup")
			}
			seenBackups[backupIdentity] = struct{}{}
			if primary &&
				(f.BackupPath != tx.BackupPath || !strings.EqualFold(f.SHA256, tx.BackupSHA256)) {
				return fmt.Errorf("pending update primary backup metadata is inconsistent")
			}
		}
		installedStates := 0
		for _, f := range tx.Files {
			stateID := strings.TrimSpace(f.InstalledStateID)
			if stateID == "" {
				continue
			}
			if len(stateID) != sha256.Size*2 {
				return fmt.Errorf("pending update installed release-unit state is invalid")
			}
			if _, err := hex.DecodeString(stateID); err != nil {
				return fmt.Errorf("pending update installed release-unit state is invalid")
			}
			installedStates++
		}
		if installedStates != 0 && installedStates != len(tx.Files) {
			return fmt.Errorf("pending update installed release-unit state is incomplete")
		}
		if !primaryListed {
			return fmt.Errorf("pending update release unit omits the primary executable")
		}
	case "app-bundle":
		if !strings.HasSuffix(strings.ToLower(tx.TargetPath), ".app") || tx.BackupPath != tx.TargetPath+".reasonix-update-backup" {
			return fmt.Errorf("pending update bundle paths are invalid")
		}
		inside := tx.TargetPath + string(filepath.Separator)
		if !strings.HasPrefix(launcher, inside) {
			return fmt.Errorf("%w: pending update bundle is not the current Guard installation", errPendingUpdateForeignInstall)
		}
		if err := validateAppBundleHandoffMetadata(tx); err != nil {
			return fmt.Errorf("pending update %w", err)
		}
		if err := validateOrphanedAppBundleBackupMetadata(tx); err != nil {
			return fmt.Errorf("pending update %w", err)
		}
	default:
		return fmt.Errorf("pending update target kind is invalid")
	}
	return nil
}

func pathInsideResolvedRoot(root, path string) bool {
	root = filepath.Clean(strings.TrimSpace(root))
	path = filepath.Clean(strings.TrimSpace(path))
	if root == "" || path == "" {
		return false
	}
	lexicalRel, err := filepath.Rel(root, path)
	if err != nil || lexicalRel == ".." || strings.HasPrefix(lexicalRel, ".."+string(filepath.Separator)) {
		return false
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return false
	}
	resolvedPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false
	}
	resolvedRel, err := filepath.Rel(resolvedRoot, resolvedPath)
	return err == nil && resolvedRel != ".." &&
		!strings.HasPrefix(resolvedRel, ".."+string(filepath.Separator))
}

func validateAppBundleHandoffMetadata(tx *UpdateTransaction) error {
	if tx == nil {
		return fmt.Errorf("handoff metadata is incomplete")
	}
	hasAny := strings.TrimSpace(tx.HandoffAppPath) != "" ||
		strings.TrimSpace(tx.HandoffStagingPath) != "" ||
		strings.TrimSpace(tx.HandoffAppTreeID) != "" ||
		strings.TrimSpace(tx.HandoffStagingTreeID) != "" ||
		tx.HandoffOwnerPID != 0
	if !hasAny {
		return nil
	}
	tx.HandoffAppPath = filepath.Clean(strings.TrimSpace(tx.HandoffAppPath))
	tx.HandoffStagingPath = filepath.Clean(strings.TrimSpace(tx.HandoffStagingPath))
	if tx.HandoffOwnerPID <= 0 ||
		!filepath.IsAbs(tx.HandoffAppPath) ||
		!filepath.IsAbs(tx.HandoffStagingPath) ||
		!strings.HasSuffix(strings.ToLower(tx.HandoffAppPath), ".app") {
		return fmt.Errorf("handoff metadata is incomplete")
	}
	if tx.HandoffAppPath == tx.TargetPath || tx.HandoffAppPath == tx.BackupPath {
		return fmt.Errorf("handoff app overlaps the installed bundle")
	}
	rel, err := filepath.Rel(tx.HandoffStagingPath, tx.HandoffAppPath)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("handoff app is outside its staging directory")
	}
	tempRoot := filepath.Clean(os.TempDir())
	stagingRel, err := filepath.Rel(tempRoot, tx.HandoffStagingPath)
	if err != nil || stagingRel == "." || stagingRel == ".." || strings.HasPrefix(stagingRel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("handoff staging directory is outside the system temporary directory")
	}
	stagingBase := strings.Split(stagingRel, string(filepath.Separator))[0]
	if !strings.HasPrefix(stagingBase, "reasonix-mac-update-") {
		return fmt.Errorf("handoff staging directory has an unexpected name")
	}
	return nil
}

func validateOrphanedAppBundleBackupMetadata(tx *UpdateTransaction) error {
	if tx == nil {
		return fmt.Errorf("orphaned backup metadata is incomplete")
	}
	path := strings.TrimSpace(tx.OrphanedBackupPath)
	treeID := strings.TrimSpace(tx.OrphanedBackupTreeID)
	if path == "" && treeID == "" {
		return nil
	}
	if tx.TargetKind != "app-bundle" || path == "" || treeID == "" {
		return fmt.Errorf("orphaned backup metadata is incomplete")
	}
	path = filepath.Clean(path)
	if !filepath.IsAbs(path) || filepath.Dir(path) != filepath.Dir(tx.BackupPath) {
		return fmt.Errorf("orphaned backup path is outside the app installation directory")
	}
	prefix := filepath.Base(tx.BackupPath) + ".reasonix-orphaned-"
	suffix, ok := strings.CutPrefix(filepath.Base(path), prefix)
	if !ok {
		return fmt.Errorf("orphaned backup path has an unexpected name")
	}
	parts := strings.Split(suffix, "-")
	if len(parts) != 2 {
		return fmt.Errorf("orphaned backup path has an unexpected name")
	}
	for _, part := range parts {
		if part == "" || strings.Trim(part, "0123456789") != "" {
			return fmt.Errorf("orphaned backup path has an unexpected name")
		}
	}
	if len(treeID) != sha256.Size*2 {
		return fmt.Errorf("orphaned backup digest is invalid")
	}
	if _, err := hex.DecodeString(treeID); err != nil {
		return fmt.Errorf("orphaned backup digest is invalid")
	}
	tx.OrphanedBackupPath = path
	tx.OrphanedBackupTreeID = treeID
	return nil
}

func copyFileWithHash(src, dst string, mode os.FileMode) (string, error) {
	return copyFileWithHashMode(src, dst, mode, false)
}

func copyFileWithHashCreate(src, dst string, mode os.FileMode) (string, error) {
	return copyFileWithHashMode(src, dst, mode, true)
}

func copyFileWithHashMode(src, dst string, mode os.FileMode, createOnly bool) (string, error) {
	in, err := openRepairRegularRead(src)
	if err != nil {
		return "", err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("source %s is not a regular file", filepath.Base(src))
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".repair-copy-*")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, h), in); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if createOnly {
		if err := renameRepairNodeNoReplace(tmpPath, dst); err != nil {
			return "", err
		}
	} else {
		if err := fileutil.ReplaceFile(tmpPath, dst); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
