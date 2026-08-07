package repair

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"golang.org/x/mod/semver"

	"reasonix/internal/config"
	"reasonix/internal/installlayout"
)

var supersededUpdateBeforeArchive = func(string) {}
var supersededAppUpdateAfterBackupArchive = func(string) {}

// ArchiveSupersededPendingAppBundleUpdate retires a legacy macOS transaction
// only after a healthy desktop is already running from the transaction's exact
// target bundle. It is intentionally limited to the two unrecoverable legacy
// shapes: the rollback backup identity was never recorded, or the recorded
// backup no longer exists. A surviving backup is content-bound and moved aside;
// the original transaction is archived under Reasonix repair state. Neither is
// deleted, so support can still inspect or manually recover the old bundle.
func ArchiveSupersededPendingAppBundleUpdate(runningVersion string) (bool, error) {
	tx, err := ReadPendingUpdate()
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, nil
	}
	eligible, err := validateSupersededPendingAppBundleUpdate(tx, runningVersion)
	if err != nil || !eligible {
		return false, err
	}
	expectedID := UpdateTransactionID(tx)

	unlock, err := acquirePendingUpdateLock()
	if err != nil {
		return false, fmt.Errorf("archive superseded app update: lock transaction: %w", err)
	}
	defer unlock()
	unlocks, err := lockRepairMutations(pendingUpdateTargetPaths(tx)...)
	if err != nil {
		return false, fmt.Errorf("archive superseded app update: lock bundle paths: %w", err)
	}
	defer unlocks()

	current, err := ReadPendingUpdate()
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("archive superseded app update: re-read transaction: %w", err)
	}
	eligible, err = validateSupersededPendingAppBundleUpdate(current, runningVersion)
	if err != nil || !eligible {
		return false, err
	}
	if UpdateTransactionID(current) != expectedID {
		return false, fmt.Errorf("archive superseded app update: transaction changed while waiting")
	}
	tx = current

	targetTreeID, err := repairPlanTreeContentStateID(tx.TargetPath)
	if err != nil {
		return false, fmt.Errorf("archive superseded app update: read running bundle: %w", err)
	}
	backupArchive, backupTreeID, err := archiveSupersededAppBundleBackup(tx, expectedID)
	if err != nil {
		return false, err
	}
	restoreBackup := func(cause error) error {
		if backupArchive == "" {
			return cause
		}
		actual, digestErr := repairPlanTreeContentStateID(backupArchive)
		if digestErr != nil || actual != backupTreeID {
			return fmt.Errorf("%w; preserved changed backup at %s", cause, backupArchive)
		}
		if _, statErr := os.Lstat(tx.BackupPath); statErr == nil {
			return fmt.Errorf("%w; preserved archived backup at %s because the public path was recreated", cause, backupArchive)
		} else if !os.IsNotExist(statErr) {
			return fmt.Errorf("%w; inspect backup restore path: %w", cause, statErr)
		}
		if restoreErr := renameRepairNodeNoReplace(backupArchive, tx.BackupPath); restoreErr != nil {
			return fmt.Errorf("%w; preserved archived backup at %s: %w", cause, backupArchive, restoreErr)
		}
		return cause
	}
	if backupArchive != "" {
		if _, statErr := os.Lstat(tx.BackupPath); statErr == nil {
			return false, restoreBackup(fmt.Errorf("archive superseded app update: rollback backup path was recreated during recovery"))
		} else if !os.IsNotExist(statErr) {
			return false, restoreBackup(fmt.Errorf("archive superseded app update: inspect rollback backup after archival: %w", statErr))
		}
	}

	supersededAppUpdateAfterBackupArchive(backupArchive)
	archivePath, err := archiveSupersededPendingMarker(tx, expectedID, "app-bundle")
	if err != nil {
		return false, restoreBackup(err)
	}
	restoreMarker := func(cause error) error {
		if restoreErr := renameRepairNodeNoReplace(archivePath, PendingUpdatePath()); restoreErr != nil {
			cause = fmt.Errorf("%w; preserved moved transaction at %s: %w", cause, archivePath, restoreErr)
		}
		return restoreBackup(cause)
	}
	actualTarget, err := repairPlanTreeContentStateID(tx.TargetPath)
	if err != nil || actualTarget != targetTreeID {
		if err == nil {
			err = fmt.Errorf("running bundle changed during recovery")
		}
		return false, restoreMarker(fmt.Errorf("archive superseded app update: %w", err))
	}
	if backupArchive != "" {
		if _, statErr := os.Lstat(tx.BackupPath); statErr == nil {
			return false, restoreMarker(fmt.Errorf("archive superseded app update: rollback backup path was recreated before commit"))
		} else if !os.IsNotExist(statErr) {
			return false, restoreMarker(fmt.Errorf("archive superseded app update: inspect rollback backup before commit: %w", statErr))
		}
	}
	return true, nil
}

func validateSupersededPendingAppBundleUpdate(tx *UpdateTransaction, runningVersion string) (bool, error) {
	if tx == nil || tx.TargetKind != "app-bundle" {
		return false, nil
	}
	platform := strings.TrimSpace(tx.Platform)
	if slash := strings.IndexByte(platform, '/'); slash >= 0 {
		platform = platform[:slash]
	}
	if platform != runtime.GOOS {
		return false, fmt.Errorf("archive superseded app update: transaction platform %q does not match %q", tx.Platform, runtime.GOOS)
	}
	if err := validateUpdateTransaction(tx); err != nil {
		return false, fmt.Errorf("archive superseded app update: invalid transaction: %w", err)
	}
	// A normal update prepares the transaction before this process shuts down;
	// its backup is intentionally absent until the detached helper performs the
	// swap. Never mistake that fresh handoff for a stale missing-backup record.
	if tx.HandoffOwnerPID == os.Getpid() {
		return false, nil
	}
	running := canonicalSemver(runningVersion)
	from := canonicalSemver(tx.FromVersion)
	to := canonicalSemver(tx.ToVersion)
	if !semver.IsValid(running) || !semver.IsValid(to) {
		return false, fmt.Errorf("archive superseded app update: invalid running or target version")
	}
	if running != from && semver.Compare(running, to) < 0 {
		return false, fmt.Errorf("archive superseded app update: running version %q is neither the prior release nor at least %q", running, to)
	}
	if strings.TrimSpace(tx.BackupTreeID) == "" {
		return true, nil
	}
	if _, err := os.Lstat(tx.BackupPath); os.IsNotExist(err) {
		return true, nil
	} else if err != nil {
		return false, fmt.Errorf("archive superseded app update: inspect rollback backup: %w", err)
	}
	return false, nil
}

func archiveSupersededAppBundleBackup(tx *UpdateTransaction, transactionID string) (string, string, error) {
	info, err := os.Lstat(tx.BackupPath)
	if os.IsNotExist(err) {
		return "", "", nil
	}
	if err != nil {
		return "", "", fmt.Errorf("archive superseded app update: inspect rollback backup: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", "", fmt.Errorf("archive superseded app update: rollback backup is not a real directory")
	}
	treeID, err := repairPlanTreeContentStateID(tx.BackupPath)
	if err != nil {
		return "", "", fmt.Errorf("archive superseded app update: read rollback backup: %w", err)
	}
	shortID := transactionID
	if len(shortID) > 16 {
		shortID = shortID[:16]
	}
	base := fmt.Sprintf("%s.reasonix-retired-%s-%s", tx.BackupPath, shortID, time.Now().UTC().Format("20060102T150405.000000000Z"))
	for attempt := 0; attempt < 16; attempt++ {
		archive := fmt.Sprintf("%s-%d", base, attempt)
		if err := renameRepairNodeNoReplace(tx.BackupPath, archive); err != nil {
			if os.IsExist(err) {
				continue
			}
			return "", "", fmt.Errorf("archive superseded app update: move rollback backup: %w", err)
		}
		actual, digestErr := repairPlanTreeContentStateID(archive)
		if digestErr == nil && actual == treeID {
			return archive, treeID, nil
		}
		cause := fmt.Errorf("archive superseded app update: rollback backup changed during archival")
		if restoreErr := renameRepairNodeNoReplace(archive, tx.BackupPath); restoreErr != nil {
			return "", "", fmt.Errorf("%w; preserved moved backup at %s: %w", cause, archive, restoreErr)
		}
		return "", "", cause
	}
	return "", "", fmt.Errorf("archive superseded app update: cannot allocate rollback backup archive path")
}

func archiveSupersededPendingMarker(tx *UpdateTransaction, transactionID, kind string) (string, error) {
	pendingPath := PendingUpdatePath()
	archiveDir := filepath.Join(filepath.Dir(pendingPath), "legacy-updates")
	if err := os.MkdirAll(archiveDir, 0o700); err != nil {
		return "", fmt.Errorf("archive superseded update: create archive: %w", err)
	}
	if !pathInsideResolvedRoot(filepath.Join(config.MemoryUserDir(), "repair"), archiveDir) {
		return "", fmt.Errorf("archive superseded update: archive directory resolves outside the repair directory")
	}
	shortID := transactionID
	if len(shortID) > 16 {
		shortID = shortID[:16]
	}
	base := filepath.Join(archiveDir, fmt.Sprintf("%s-%s-%s", time.Now().UTC().Format("20060102T150405.000000000Z"), shortID, kind))
	for attempt := 0; attempt < 16; attempt++ {
		archivePath := fmt.Sprintf("%s-%d.json", base, attempt)
		if err := renameRepairNodeNoReplace(pendingPath, archivePath); err != nil {
			if os.IsExist(err) {
				continue
			}
			return "", fmt.Errorf("archive superseded update: move transaction: %w", err)
		}
		restore := func(cause error) error {
			if restoreErr := renameRepairNodeNoReplace(archivePath, pendingPath); restoreErr != nil {
				return fmt.Errorf("%w; preserved moved transaction at %s: %w", cause, archivePath, restoreErr)
			}
			return cause
		}
		body, readErr := os.ReadFile(archivePath)
		if readErr != nil {
			return "", restore(fmt.Errorf("archive superseded update: verify moved transaction: %w", readErr))
		}
		var archived UpdateTransaction
		if unmarshalErr := json.Unmarshal(body, &archived); unmarshalErr != nil {
			return "", restore(fmt.Errorf("archive superseded update: verify moved transaction: %w", unmarshalErr))
		}
		if validateErr := validateUpdateTransaction(&archived); validateErr != nil {
			return "", restore(fmt.Errorf("archive superseded update: verify moved transaction: %w", validateErr))
		}
		if UpdateTransactionID(&archived) != transactionID || UpdateTransactionID(tx) != transactionID {
			return "", restore(fmt.Errorf("archive superseded update: transaction changed before archival"))
		}
		return archivePath, nil
	}
	return "", fmt.Errorf("archive superseded update: cannot allocate archive path")
}

// ArchiveSupersededPendingFileUpdate retires a superseded file-update transaction
// after the same or a newer versioned installation has started successfully.
// It never deletes the transaction or trusts version text alone: the current
// process must be the active desktop named by a valid current.json, every
// recorded target must belong to the superseded flat installRoot, and the exact
// transaction is revalidated under the pending-update lock.
//
// This is the recovery path for users whose v1.18-v1.19 update completed but
// whose old Guard never committed startup health. The original JSON is moved to
// repair/legacy-updates for diagnostics; rollback backups are left untouched.
func ArchiveSupersededPendingFileUpdate(runningVersion, installRoot string) (bool, error) {
	tx, err := readSupersededPendingFileUpdate(runningVersion, installRoot)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("archive superseded pending update: %w", err)
	}
	expectedID := UpdateTransactionID(tx)

	unlock, err := acquirePendingUpdateLock()
	if err != nil {
		return false, fmt.Errorf("archive superseded pending update: lock transaction: %w", err)
	}
	defer unlock()
	current, err := readSupersededPendingFileUpdate(runningVersion, installRoot)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("archive superseded pending update: re-read transaction: %w", err)
	}
	if UpdateTransactionID(current) != expectedID {
		return false, fmt.Errorf("archive superseded pending update: transaction changed while waiting")
	}

	pendingPath := PendingUpdatePath()
	archiveDir := filepath.Join(filepath.Dir(pendingPath), "legacy-updates")
	if err := os.MkdirAll(archiveDir, 0o700); err != nil {
		return false, fmt.Errorf("archive superseded pending update: create archive: %w", err)
	}
	if !pathInsideResolvedRoot(filepath.Join(config.MemoryUserDir(), "repair"), archiveDir) {
		return false, fmt.Errorf("archive superseded pending update: archive directory resolves outside the repair directory")
	}
	shortID := expectedID
	if len(shortID) > 16 {
		shortID = shortID[:16]
	}
	archiveBase := filepath.Join(archiveDir, fmt.Sprintf("%s-%s", time.Now().UTC().Format("20060102T150405.000000000Z"), shortID))
	supersededUpdateBeforeArchive(pendingPath)
	for attempt := 0; attempt < 16; attempt++ {
		archivePath := fmt.Sprintf("%s-%d.json", archiveBase, attempt)
		if err := renameRepairNodeNoReplace(pendingPath, archivePath); err != nil {
			if os.IsExist(err) {
				continue
			}
			return false, fmt.Errorf("archive superseded pending update: move transaction: %w", err)
		}
		restore := func(cause error) error {
			if restoreErr := renameRepairNodeNoReplace(archivePath, pendingPath); restoreErr != nil {
				return fmt.Errorf("%w; preserved moved transaction at %s: %w", cause, archivePath, restoreErr)
			}
			return cause
		}
		body, readErr := os.ReadFile(archivePath)
		if readErr != nil {
			return false, restore(fmt.Errorf("archive superseded pending update: verify moved transaction: %w", readErr))
		}
		var archived UpdateTransaction
		if unmarshalErr := json.Unmarshal(body, &archived); unmarshalErr != nil {
			return false, restore(fmt.Errorf("archive superseded pending update: verify moved transaction: %w", unmarshalErr))
		}
		if UpdateTransactionID(&archived) != expectedID {
			return false, restore(fmt.Errorf("archive superseded pending update: transaction changed before archival"))
		}
		return true, nil
	}
	return false, fmt.Errorf("archive superseded pending update: cannot allocate archive path")
}

// readSupersededPendingFileUpdate deliberately bypasses the ordinary
// current-Guard directory check. That check is correct for rollback, but a
// versioned install runs from versions/<version>/ while the superseded
// transaction names flat binaries at InstallRoot. Requiring the ordinary read
// here made this recovery path reject the only state it was designed to heal.
func readSupersededPendingFileUpdate(runningVersion, installRoot string) (*UpdateTransaction, error) {
	tx, err := readPendingUpdateUnchecked()
	if err != nil {
		return nil, err
	}
	if err := validateSupersededPendingFileUpdate(tx, runningVersion, installRoot); err != nil {
		return nil, err
	}
	return tx, nil
}

func validateSupersededPendingFileUpdate(tx *UpdateTransaction, runningVersion, installRoot string) error {
	if tx == nil || tx.TargetKind != "file" {
		return fmt.Errorf("archive superseded pending update: only file transactions are eligible")
	}
	runningVersion = canonicalSemver(runningVersion)
	pendingVersion := canonicalSemver(tx.ToVersion)
	if !semver.IsValid(runningVersion) || !semver.IsValid(pendingVersion) || semver.Compare(runningVersion, pendingVersion) < 0 {
		return fmt.Errorf("archive superseded pending update: running version %q is older than %q", runningVersion, pendingVersion)
	}
	installRoot = canonicalLegacyInstallPath(installRoot)
	if installRoot == "" || !filepath.IsAbs(installRoot) {
		return fmt.Errorf("archive superseded pending update: install root is invalid")
	}
	launcher, err := repairExecutable()
	if err != nil {
		return fmt.Errorf("archive superseded pending update: current executable is unavailable")
	}
	resolvedRoot, err := installlayout.ResolveInstallRoot(launcher)
	if err != nil || canonicalLegacyInstallPath(resolvedRoot) != installRoot {
		return fmt.Errorf("archive superseded pending update: current executable is outside the install root")
	}
	ptr, err := installlayout.ReadCurrent(installRoot)
	if err != nil {
		return fmt.Errorf("archive superseded pending update: current installation is not versioned: %w", err)
	}
	if canonicalSemver(ptr.ActiveVersion) != runningVersion {
		return fmt.Errorf("archive superseded pending update: active install version %q does not match running version %q", ptr.ActiveVersion, runningVersion)
	}
	activeDesktop, err := installlayout.ActiveDesktopPath(installRoot)
	if err != nil {
		return fmt.Errorf("archive superseded pending update: active desktop is unavailable: %w", err)
	}
	if canonicalRepairPath(activeDesktop) != canonicalRepairPath(launcher) {
		return fmt.Errorf("archive superseded pending update: current executable is not the active desktop")
	}
	platform := strings.TrimSpace(tx.Platform)
	if slash := strings.IndexByte(platform, '/'); slash >= 0 {
		platform = platform[:slash]
	}
	if platform != runtime.GOOS {
		return fmt.Errorf("archive superseded pending update: transaction platform %q does not match %q", tx.Platform, runtime.GOOS)
	}
	// Validate every transaction field and backup path while substituting the
	// old primary target as the legacy launcher's location. The only ordinary
	// invariant intentionally relaxed is that this target must sit beside the
	// current versioned desktop.
	if err := validateUpdateTransactionForLauncher(tx, tx.TargetPath); err != nil {
		return fmt.Errorf("archive superseded pending update: invalid transaction: %w", err)
	}
	if canonicalLegacyInstallPath(filepath.Dir(tx.TargetPath)) != installRoot {
		return fmt.Errorf("archive superseded pending update: target is not a flat install member")
	}
	targets := []string{tx.TargetPath}
	for _, file := range tx.Files {
		targets = append(targets, file.TargetPath)
	}
	for _, target := range targets {
		if canonicalLegacyInstallPath(filepath.Dir(target)) != installRoot {
			return fmt.Errorf("archive superseded pending update: release member is not in the flat install root")
		}
	}
	return nil
}

func canonicalSemver(value string) string {
	value = strings.TrimSpace(value)
	if value != "" && !strings.HasPrefix(value, "v") {
		value = "v" + value
	}
	return value
}

func canonicalLegacyInstallPath(path string) string {
	path = filepath.Clean(strings.TrimSpace(path))
	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
	}
	return path
}
