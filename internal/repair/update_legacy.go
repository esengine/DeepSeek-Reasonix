package repair

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"golang.org/x/mod/semver"
)

// ArchiveSupersededPendingFileUpdate retires an older file-update transaction
// after a newer versioned installation has started successfully. It never
// deletes the transaction or trusts version text alone: every recorded target
// must belong to installRoot, the running version must be strictly newer, and
// the exact transaction is revalidated under the pending-update lock.
//
// This is the recovery path for users whose v1.18-v1.19 update completed but
// whose old Guard never committed startup health. The original JSON is moved to
// repair/legacy-updates for diagnostics; rollback backups are left untouched.
func ArchiveSupersededPendingFileUpdate(runningVersion, installRoot string) (bool, error) {
	tx, err := ReadPendingUpdate()
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("archive superseded pending update: %w", err)
	}
	expectedID := UpdateTransactionID(tx)
	if err := validateSupersededPendingFileUpdate(tx, runningVersion, installRoot); err != nil {
		return false, err
	}

	unlock, err := acquirePendingUpdateLock()
	if err != nil {
		return false, fmt.Errorf("archive superseded pending update: lock transaction: %w", err)
	}
	defer unlock()
	current, err := ReadPendingUpdate()
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("archive superseded pending update: re-read transaction: %w", err)
	}
	if UpdateTransactionID(current) != expectedID {
		return false, fmt.Errorf("archive superseded pending update: transaction changed while waiting")
	}
	if err := validateSupersededPendingFileUpdate(current, runningVersion, installRoot); err != nil {
		return false, err
	}

	pendingPath := PendingUpdatePath()
	archiveDir := filepath.Join(filepath.Dir(pendingPath), "legacy-updates")
	if err := os.MkdirAll(archiveDir, 0o700); err != nil {
		return false, fmt.Errorf("archive superseded pending update: create archive: %w", err)
	}
	shortID := expectedID
	if len(shortID) > 16 {
		shortID = shortID[:16]
	}
	archivePath := filepath.Join(archiveDir, fmt.Sprintf("%s-%s.json", time.Now().UTC().Format("20060102T150405.000000000Z"), shortID))
	if err := os.Rename(pendingPath, archivePath); err != nil {
		return false, fmt.Errorf("archive superseded pending update: move transaction: %w", err)
	}
	return true, nil
}

func validateSupersededPendingFileUpdate(tx *UpdateTransaction, runningVersion, installRoot string) error {
	if tx == nil || tx.TargetKind != "file" {
		return fmt.Errorf("archive superseded pending update: only file transactions are eligible")
	}
	runningVersion = canonicalSemver(runningVersion)
	pendingVersion := canonicalSemver(tx.ToVersion)
	if !semver.IsValid(runningVersion) || !semver.IsValid(pendingVersion) || semver.Compare(runningVersion, pendingVersion) <= 0 {
		return fmt.Errorf("archive superseded pending update: running version %q is not newer than %q", runningVersion, pendingVersion)
	}
	installRoot = canonicalLegacyInstallPath(installRoot)
	if installRoot == "" || !filepath.IsAbs(installRoot) {
		return fmt.Errorf("archive superseded pending update: install root is invalid")
	}
	targets := []string{tx.TargetPath}
	for _, file := range tx.Files {
		targets = append(targets, file.TargetPath)
	}
	for _, target := range targets {
		if !legacyTargetWithinRoot(installRoot, target) {
			return fmt.Errorf("archive superseded pending update: target escapes install root")
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

func legacyTargetWithinRoot(root, target string) bool {
	target = canonicalLegacyInstallPath(target)
	if target == "" || !filepath.IsAbs(target) {
		return false
	}
	rel, err := filepath.Rel(root, target)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel)
}
