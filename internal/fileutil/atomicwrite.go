package fileutil

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

var (
	maxReplaceRetries = 8
	replaceRetryBase  = 20 * time.Millisecond
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

// ReplaceFile renames tmp onto dest, falling back to a copy when the rename
// fails — Windows encryption-software filter drivers report a cross-device link
// (EXDEV) for a same-dir rename. A second Windows failure mode is a transient
// lock on dest (antivirus, the search indexer, a concurrent reader) that makes
// the rename fail with a sharing violation for a few hundred ms.
//
// Order matters: the rename is retried with backoff FIRST, and the copy runs
// only after every retry failed. copyOnto truncates dest in place, so a reader
// racing it can observe an empty or half-written file — reaching for it on the
// first transient failure would break the atomicity AtomicWriteFile promises.
// On filter-driver hosts where the rename never succeeds this costs the full
// retry backoff before each copy; correctness on every other host wins. A
// missing tmp means the write itself failed and no retry can help.
func ReplaceFile(tmp, dest string) error {
	var err error
	for attempt := 0; ; attempt++ {
		if err = os.Rename(tmp, dest); err == nil {
			return nil
		}
		if attempt >= maxReplaceRetries || !fileExists(tmp) {
			break
		}
		time.Sleep(time.Duration(attempt+1) * replaceRetryBase)
	}
	if copyOnto(tmp, dest) == nil {
		return nil
	}
	return err
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

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
