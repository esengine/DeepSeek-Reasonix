//go:build windows

package fileutil

import (
	"errors"
	"fmt"
	"os"
	"syscall"

	"golang.org/x/sys/windows"
)

// renameCrossesDevice reports whether a rename failed because the filesystem
// treats the two paths as different devices. Encryption-software filter
// drivers report ERROR_NOT_SAME_DEVICE even for a same-directory rename
// (#2696); such a rename fails identically on every retry, so ReplaceFile
// goes straight to its copy fallback instead of burning the retry backoff.
func renameCrossesDevice(err error) bool {
	return errors.Is(err, windows.ERROR_NOT_SAME_DEVICE) || errors.Is(err, syscall.EXDEV)
}

// replaceWithBackup is the last-resort fallback on Windows when
// os.Rename has been retried maxReplaceRetries times and the file is
// still locked (typically by the process itself, an antivirus scanner,
// or a search indexer holding a shared handle).
//
// The sequence is:
//
//  1. Backup dest → dest.bak  (best-effort; a failed backup does not abort)
//  2. Read tmp content into memory (before copyOnto consumes it)
//  3. Direct-write tmp content into dest via copyOnto (non-atomic)
//  4. Read back dest and verify it matches the in-memory copy
//  5. If verification passes → remove dest.bak
//  6. If verification fails → restore dest from dest.bak
//
// This is deliberately non-atomic: a racing reader can observe a torn
// file. We accept that trade-off because on Windows the alternative is
// a permanent write failure (the current behaviour) which is worse.
func replaceWithBackup(tmp, dest string, origErr error) error {
	// 1. Best-effort backup of the original destination.
	backup := dest + ".bak"
	bakOK := false
	if srcData, err := os.ReadFile(dest); err == nil {
		if err := os.WriteFile(backup, srcData, 0o644); err == nil {
			bakOK = true
		}
	}

	// 2. Read tmp content into memory before copyOnto consumes it.
	tmpData, tmpErr := os.ReadFile(tmp)
	if tmpErr != nil {
		if bakOK {
			_ = os.Remove(backup)
		}
		return fmt.Errorf("backup-replace: read tmp before copyOnto: %w (original: %v)", tmpErr, origErr)
	}

	// 3. Direct overwrite (copyOnto removes tmp on success).
	if err := copyOnto(tmp, dest); err != nil {
		if bakOK {
			_ = restoreBackup(backup, dest)
			_ = os.Remove(backup)
		}
		return fmt.Errorf("backup-replace: copyOnto failed after %d retries: %w (original: %v)", maxReplaceRetries, err, origErr)
	}

	// 4. Verify: read back dest and compare with in-memory copy.
	destData, destErr := os.ReadFile(dest)
	if destErr != nil || !bytesEqual(tmpData, destData) {
		if bakOK {
			_ = restoreBackup(backup, dest)
			_ = os.Remove(backup)
		}
		return fmt.Errorf("backup-replace: verification failed after %d retries (original: %v)", maxReplaceRetries, origErr)
	}

	// 5. Verification OK, delete backup.
	_ = os.Remove(backup)
	return nil
}

func restoreBackup(backup, dest string) error {
	data, err := os.ReadFile(backup)
	if err != nil {
		return err
	}
	return os.WriteFile(dest, data, 0o644)
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
