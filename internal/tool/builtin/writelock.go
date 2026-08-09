package builtin

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// officeLockFile reports the sibling lock file that Microsoft Office creates
// next to an open document: "~$" followed by the document's base name. Word,
// Excel and PowerPoint hold the document with deny-write sharing while the
// lock file exists, so a failed write alongside a matching lock file almost
// always means the document is open in the office app. The match is
// case-insensitive, mirroring Windows filename semantics.
func officeLockFile(path string) (string, bool) {
	base := filepath.Base(path)
	if base == "." || base == string(filepath.Separator) || strings.HasPrefix(base, "~$") {
		return "", false
	}
	lock := "~$" + base
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		return "", false
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.EqualFold(entry.Name(), lock) {
			return entry.Name(), true
		}
	}
	return "", false
}

// annotateWriteLockError appends an actionable hint when a file write fails
// because another process holds the target open. Without it the agent sees a
// bare "Access is denied" from the atomic rename and cannot tell the user what
// to do (issue #5599). The check runs only after a failed write: a stale lock
// file left by a crashed Office session must not block a write that would
// otherwise succeed.
func annotateWriteLockError(path string, err error) error {
	if lock, ok := officeLockFile(path); ok {
		return fmt.Errorf("%w; the file appears to be locked by an Office app (lock file %s found) — close the document in Word/Excel/PowerPoint and retry", err, lock)
	}
	if isWindowsFileLocked(err) {
		return fmt.Errorf("%w; the file is in use by another process or not writable — close the program holding it and retry", err)
	}
	return err
}
