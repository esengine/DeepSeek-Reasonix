//go:build windows

package agent

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sync/atomic"
	"unsafe"

	"reasonix/internal/store"

	"golang.org/x/sys/windows"
)

// tryLockSessionFile attempts the compatibility save lock once without
// blocking. The shared wrapper in save.go supplies the bounded retry window.
func tryLockSessionFile(path string) (func(), error) {
	f, err := os.OpenFile(store.SessionLockFile(path), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		if errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
			return nil, ErrSessionFileLockHeld
		}
		return nil, err
	}
	handle := windows.Handle(f.Fd())
	var overlapped windows.Overlapped
	flags := uint32(windows.LOCKFILE_EXCLUSIVE_LOCK | windows.LOCKFILE_FAIL_IMMEDIATELY)
	if err := windows.LockFileEx(handle, flags, 0, 1, 0, &overlapped); err != nil {
		_ = f.Close()
		if errors.Is(err, windows.ERROR_LOCK_VIOLATION) || errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
			return nil, ErrSessionFileLockHeld
		}
		return nil, err
	}
	return func() {
		_ = windows.UnlockFileEx(handle, 0, 1, 0, &overlapped)
		_ = f.Close()
	}, nil
}

// sessionLockFile is a non-blocking exclusive lock on a lock file itself,
// used by cleanup paths that may need to delete the file they locked.
type sessionLockFile struct {
	handle     windows.Handle
	path       string
	overlapped windows.Overlapped
}

// sessionLockDispositionFallbacks counts RemoveAndUnlock calls that could not
// delete through the held handle and fell back to a path-based remove. The
// fallback reopens the cleanup-vs-saver window, so tests pin it at zero.
var sessionLockDispositionFallbacks atomic.Int64

// tryTakeSessionLockFile opens lockPath and takes exclusive LockFileEx
// without blocking. The handle requests DELETE so RemoveAndUnlock can mark
// disposition on the same handle that owns the lock.
func tryTakeSessionLockFile(lockPath string) (*sessionLockFile, error) {
	return tryTakeSessionLockFileAt(lockPath)
}

func tryTakeSessionLeaseLockFile(lockPath string) (*sessionLockFile, error) {
	return tryTakeSessionLockFileAt(lockPath)
}

func tryTakeSessionLockFileAt(lockPath string) (*sessionLockFile, error) {
	pathp, err := windows.UTF16PtrFromString(lockPath)
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(pathp,
		windows.GENERIC_READ|windows.GENERIC_WRITE|windows.DELETE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_ALWAYS, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		if errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
			return nil, ErrSessionFileLockHeld
		}
		return nil, err
	}
	l := &sessionLockFile{handle: handle, path: lockPath}
	flags := uint32(windows.LOCKFILE_EXCLUSIVE_LOCK | windows.LOCKFILE_FAIL_IMMEDIATELY)
	if err := windows.LockFileEx(handle, flags, 0, 1, 0, &l.overlapped); err != nil {
		_ = windows.CloseHandle(handle)
		if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return nil, ErrSessionFileLockHeld
		}
		return nil, err
	}
	return l, nil
}

func (l *sessionLockFile) Unlock() {
	_ = windows.UnlockFileEx(l.handle, 0, 1, 0, &l.overlapped)
	_ = windows.CloseHandle(l.handle)
}

// writeOwnerInfo replaces the lock file contents through the held handle
// so owner identity lives inside .lease.lock and dies with RemoveAndUnlock.
func (l *sessionLockFile) writeOwnerInfo(b []byte) error {
	if l == nil || l.handle == 0 {
		return errors.New("lease lock not held")
	}
	// SetFilePointerEx is not in golang.org/x/sys/windows. Truncate via
	// seek-to-zero + SetEndOfFile, then write the replacement document.
	if _, err := windows.SetFilePointer(l.handle, 0, nil, windows.FILE_BEGIN); err != nil {
		return err
	}
	if err := windows.SetEndOfFile(l.handle); err != nil {
		return err
	}
	payload := sessionLeaseOwnerBytes(b)
	var written uint32
	if err := windows.WriteFile(l.handle, payload, &written, nil); err != nil {
		return err
	}
	if int(written) != len(payload) {
		return fmt.Errorf("lease owner info: short write %d of %d bytes", written, len(payload))
	}
	return windows.FlushFileBuffers(l.handle)
}

// RemoveAndUnlock marks delete-disposition on the held handle, then unlocks
// and closes so the name dies with the handle.
func (l *sessionLockFile) RemoveAndUnlock() error {
	// FILE_DISPOSITION_INFO with its BOOLEAN widened to a full word.
	info := struct{ DeleteFile uint32 }{DeleteFile: 1}
	dispErr := windows.SetFileInformationByHandle(l.handle, windows.FileDispositionInfo,
		(*byte)(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)))
	l.Unlock()
	if dispErr != nil {
		// Delete disposition unsupported (exotic filesystem): fall back to a
		// path-based remove after the release. A short adoption window beats
		// leaving the sidecar behind forever.
		sessionLockDispositionFallbacks.Add(1)
		if err := os.Remove(l.path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func tryLockSessionLeaseFile(path string) (func(), error) {
	lockPath := store.SessionLeaseLock(path)
	pathp, err := windows.UTF16PtrFromString(lockPath)
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(pathp,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_ALWAYS, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		if errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
			return nil, ErrSessionLeaseHeld
		}
		return nil, err
	}
	var overlapped windows.Overlapped
	flags := uint32(windows.LOCKFILE_EXCLUSIVE_LOCK | windows.LOCKFILE_FAIL_IMMEDIATELY)
	if err := windows.LockFileEx(handle, flags, 0, 1, 0, &overlapped); err != nil {
		_ = windows.CloseHandle(handle)
		if errors.Is(err, windows.ERROR_LOCK_VIOLATION) || errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
			return nil, ErrSessionLeaseHeld
		}
		return nil, err
	}
	return func() {
		_ = windows.UnlockFileEx(handle, 0, 1, 0, &overlapped)
		_ = windows.CloseHandle(handle)
	}, nil
}

func readSessionLeaseLockFile(path string) ([]byte, error) {
	pathp, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(pathp,
		windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(handle), path)
	if f == nil {
		_ = windows.CloseHandle(handle)
		return nil, os.ErrInvalid
	}
	defer f.Close()
	if _, err := f.Seek(sessionLeaseOwnerOffset, io.SeekStart); err != nil {
		return nil, err
	}
	return io.ReadAll(f)
}
