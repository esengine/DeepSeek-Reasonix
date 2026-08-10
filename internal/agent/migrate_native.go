package agent

import (
	"errors"
	"os"
	"path/filepath"
	"time"

	"reasonix/internal/store"
)

// moveFlatImport re-homes a session left in the global directory by a flat
// import. An mtime match distinguishes it from a same-named native session.
func moveFlatImport(oldPath, newPath string, srcInfo os.FileInfo) bool {
	if srcInfo == nil {
		return false
	}
	info, err := os.Stat(oldPath)
	if err != nil {
		return false
	}
	d := info.ModTime().Sub(srcInfo.ModTime())
	if d < -2*time.Second || d > 2*time.Second {
		return false
	}
	if moved, decided := moveNativeFlatImport(oldPath, newPath); decided {
		return moved
	}
	if err := os.MkdirAll(filepath.Dir(newPath), 0o755); err != nil {
		return false
	}
	if err := linkFileNoReplace(oldPath, newPath); err != nil {
		if errors.Is(err, os.ErrExist) {
			return false
		}
		if err := transformAndCopyJsonl(oldPath, newPath); err != nil {
			return false
		}
	}
	if err := os.Remove(oldPath); err != nil {
		return false
	}
	return true
}

// A native flat import must be re-homed from its WAL-backed view, not its
// possibly older compatibility checkpoint.
func moveNativeFlatImport(oldPath, newPath string) (moved, decided bool) {
	unlockOld := lockSessionSavePath(oldPath)
	defer unlockOld()
	probe, err := probeSessionEventLog(oldPath)
	if err != nil || !probe.native || probe.size == 0 {
		return false, false
	}
	oldFileUnlock, err := lockSessionFile(oldPath)
	if err != nil {
		return false, true
	}
	defer oldFileUnlock()

	unlockNew := lockSessionSavePath(newPath)
	defer unlockNew()
	if err := os.MkdirAll(filepath.Dir(newPath), 0o755); err != nil {
		return false, true
	}
	newFileUnlock, err := lockSessionFile(newPath)
	if err != nil {
		return false, true
	}
	defer newFileUnlock()
	if sessionArtifactExists(newPath) {
		return false, true
	}

	session, err := loadSessionUnlocked(oldPath)
	if err != nil {
		return false, true
	}
	if err := session.saveLocked(newPath, sessionSaveSnapshot); err != nil {
		return false, true
	}
	if meta, ok, metaErr := LoadBranchMeta(oldPath); metaErr != nil {
		return false, true
	} else if ok {
		meta.ID = BranchID(newPath)
		if err := SaveBranchMeta(newPath, meta); err != nil {
			return false, true
		}
	}
	for _, artifact := range store.SessionSidecarFiles(oldPath) {
		if err := os.Remove(artifact); err != nil && !os.IsNotExist(err) {
			return false, true
		}
	}
	if err := os.Remove(oldPath); err != nil && !os.IsNotExist(err) {
		return false, true
	}
	return true, true
}
