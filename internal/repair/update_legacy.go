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
				return fmt.Errorf("%w; preserved moved transaction at %s: %v", cause, archivePath, restoreErr)
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
