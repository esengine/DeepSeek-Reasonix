//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows"

	"reasonix/internal/base/scratch"
)

const (
	stagePrefix     = "reasonix-update-stage-"
	installerPrefix = "reasonix-update-installer-"
	payloadName     = "payload"
	cleanupStep     = 100 * time.Millisecond
)

// cleanupBudget bounds how long a cleanup waits for Windows to release a file
// (antivirus scanning, a lingering handle) before leaving the directory for the
// next scratch sweep.
var cleanupBudget = 5 * time.Second

// ErrStagingPreserved marks a staging directory the helper could not remove.
// It is unclaimed by then, so the next staging of the same prefix reclaims it.
var ErrStagingPreserved = errors.New("update staging preserved")

// updateStaging is a scratch-owned directory under the temp root. The owner
// lock lives in the root and the installer extracts into payloadDir, so the
// payload stays empty until the installer fills it.
type updateStaging struct {
	dir   *scratch.Dir
	owner dirID
}

// dirID is a directory's volume serial and file index. os.SameFile cannot
// stand in for it: it opens the stat's original path, which is gone once the
// directory has been moved aside.
type dirID struct {
	volume     uint32
	indexHigh  uint32
	indexLow   uint32
	identified bool
}

func directoryID(path string) (dirID, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return dirID{}, err
	}
	h, err := windows.CreateFile(p, 0, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil,
		windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return dirID{}, err
	}
	defer windows.CloseHandle(h)
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &info); err != nil {
		return dirID{}, err
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 || info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return dirID{}, fmt.Errorf("%s is not a plain directory", path)
	}
	return dirID{info.VolumeSerialNumber, info.FileIndexHigh, info.FileIndexLow, true}, nil
}

func createUpdateStaging(prefix string) (*updateStaging, error) {
	dir, err := scratch.Create(prefix)
	if err != nil {
		return nil, err
	}
	if _, err := lstatUpdateStagingFn(dir.Path()); err != nil {
		_ = dir.Remove()
		return nil, err
	}
	owner, err := directoryID(dir.Path())
	if err != nil {
		_ = dir.Remove()
		return nil, err
	}
	return &updateStaging{dir: dir, owner: owner}, nil
}

func (s *updateStaging) root() string { return s.dir.Path() }

func (s *updateStaging) payloadDir() (string, error) {
	path := filepath.Join(s.root(), payloadName)
	return path, os.Mkdir(path, 0o700)
}

func (s *updateStaging) cleanup() error {
	s.dir.Release()
	return cleanupOwnedWindowsUpdateDirectory(s.root(), s.owner)
}

func retryableWindowsRelease(err error) bool {
	return errors.Is(err, windows.ERROR_ACCESS_DENIED) ||
		errors.Is(err, windows.ERROR_SHARING_VIOLATION) ||
		errors.Is(err, windows.ERROR_LOCK_VIOLATION)
}

// cleanupOwnedWindowsUpdateDirectory moves the directory aside, checking it is
// still the one created, then deletes it. Both steps retry while Windows holds
// a file, within cleanupBudget.
func cleanupOwnedWindowsUpdateDirectory(path string, owner dirID) error {
	if path == "" || !owner.identified {
		return fmt.Errorf("Windows update cleanup identity is incomplete")
	}
	deadline := time.Now().Add(cleanupBudget)
	aside, err := moveAsideOwned(path, owner, deadline)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("%w: %s: %w", ErrStagingPreserved, path, err)
	}
	for {
		err := os.RemoveAll(aside)
		if err == nil {
			return nil
		}
		if !time.Now().Before(deadline) {
			return fmt.Errorf("%w: %s: %w", ErrStagingPreserved, aside, err)
		}
		time.Sleep(cleanupStep)
	}
}

func moveAsideOwned(path string, owner dirID, deadline time.Time) (string, error) {
	from, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", err
	}
	for n := 0; ; n++ {
		aside := fmt.Sprintf("%s.reasonix-cleanup-%d-%d", path, time.Now().UTC().UnixNano(), n)
		to, err := windows.UTF16PtrFromString(aside)
		if err != nil {
			return "", err
		}
		err = windows.MoveFileEx(from, to, windows.MOVEFILE_WRITE_THROUGH)
		switch {
		case err == nil:
			return aside, verifyMovedOwner(path, aside, owner)
		case errors.Is(err, os.ErrExist):
			if n >= 16 {
				return "", fmt.Errorf("cannot allocate Windows update cleanup path")
			}
		case retryableWindowsRelease(err) && time.Now().Before(deadline):
			time.Sleep(cleanupStep)
		default:
			return "", err
		}
	}
}

func verifyMovedOwner(path, aside string, owner dirID) error {
	actual, err := directoryID(aside)
	if err != nil {
		return err
	}
	if actual == owner {
		return nil
	}
	back, fromErr := windows.UTF16PtrFromString(aside)
	orig, toErr := windows.UTF16PtrFromString(path)
	if fromErr != nil || toErr != nil {
		return fmt.Errorf("Windows update staging changed before cleanup; preserve replacement at %s", aside)
	}
	if err := windows.MoveFileEx(back, orig, windows.MOVEFILE_WRITE_THROUGH); err != nil {
		return fmt.Errorf("Windows update staging changed before cleanup; preserve replacement at %s: %w", aside, err)
	}
	return fmt.Errorf("Windows update staging changed before cleanup")
}
