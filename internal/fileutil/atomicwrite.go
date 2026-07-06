package fileutil

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

var (
	maxReplaceRetries = 12
	replaceRetryBase  = 20 * time.Millisecond

	// renameFile is a test seam: the two rename failure classes ReplaceFile
	// distinguishes (transient lock vs cross-device) cannot be provoked
	// portably on a real filesystem.
	renameFile = os.Rename
)

// AtomicWriteFile writes data to path crash-safely: it writes to a sibling tmp
// file, fsyncs it so the bytes reach disk (guarding against power loss, not just
// process crash — see #4615), then atomically renames it onto path via
// ReplaceFile. A crash or power cut at any point leaves either the old file or
// the complete new file, never a truncated one. perm applies to the final file.
func AtomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	dirPerm := os.FileMode(0o755)
	if perm&0o077 == 0 {
		dirPerm = 0o700
	}
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return fmt.Errorf("create dir for %s: %w", path, err)
	}
	tmp, err := os.CreateTemp(dir, ".atomic-*.tmp")
	if err != nil {
		return fmt.Errorf("create tmp for %s: %w", path, err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write tmp for %s: %w", path, err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("fsync tmp for %s: %w", path, err)
	}
	// Chmod the still-open handle, before Close, so there is no window between
	// close and a path-based chmod for another process (Windows AV / search
	// indexer) to grab or move the tmp and make the chmod fail with "file not
	// found". CreateTemp makes a 0600 file, so this only widens when perm asks.
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("chmod tmp for %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close tmp for %s: %w", path, err)
	}
	if err := ReplaceFile(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
}

// ReplaceFile renames tmp onto dest, publishing the new content atomically: a
// reader concurrent with the replace sees either the old file or the complete
// new one. The rename can fail in two ways, and they are handled differently:
//
//   - A transient lock on dest (antivirus, the search indexer, a concurrent
//     reader without delete sharing) fails the rename for a few hundred ms.
//     The rename is retried with backoff, and the last error is returned if
//     the lock never clears. The failure is loud on purpose: falling back to
//     an in-place copy here would truncate dest first, letting a racing
//     reader observe an empty or half-written file — exactly the torn state
//     AtomicWriteFile promises its callers (session leases, credentials,
//     plugin state) can never happen.
//   - Windows encryption-software filter drivers report a cross-device link
//     (ERROR_NOT_SAME_DEVICE / EXDEV) even for a same-dir rename (#2696), and
//     every retry fails identically. Only this class falls back to the
//     non-atomic copy, and immediately — retrying a structurally impossible
//     rename would only delay it. Torn reads remain possible in that degraded
//     mode; it is the only way to write at all on such hosts, and
//     rename-capable filesystems never take it.
//
// A missing tmp means the write itself failed and no retry can help.
func ReplaceFile(tmp, dest string) error {
	var err error
	for attempt := 0; ; attempt++ {
		if err = renameFile(tmp, dest); err == nil {
			return nil
		}
		if renameCrossesDevice(err) {
			if copyOnto(tmp, dest) == nil {
				return nil
			}
			return err
		}
		if attempt >= maxReplaceRetries || !fileExists(tmp) {
			return err
		}
		time.Sleep(time.Duration(attempt+1) * replaceRetryBase)
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// copyOnto is the non-atomic last resort for hosts whose filesystem cannot
// rename tmp onto dest at all (see ReplaceFile). It truncates dest in place,
// so a concurrent reader can observe an empty or half-written file — it must
// never run for failures a retry could clear.
func copyOnto(tmp, dest string) error {
	info, err := os.Stat(tmp)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(tmp)
	if err != nil {
		return err
	}
	if err := os.WriteFile(dest, data, info.Mode().Perm()); err != nil {
		return err
	}
	// WriteFile keeps an existing dest's mode, so re-apply tmp's mode to match
	// what the rename would have done (a 0600 config tmp must not widen to 0644).
	_ = os.Chmod(dest, info.Mode().Perm())
	_ = os.Remove(tmp)
	return nil
}
