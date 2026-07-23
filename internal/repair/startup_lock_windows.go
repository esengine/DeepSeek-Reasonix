//go:build windows

package repair

import (
	"os"

	"golang.org/x/sys/windows"
)

// lockRepairStateFile takes an exclusive cross-process lock (on path+".lock")
// guarding a repair-state read-modify-write cycle, such as the startup tracker
// record or the pending-update transaction (LockFileEx releases on process
// exit, so a crashed holder can never wedge later launches).
func lockRepairStateFile(path string) (func(), error) {
	f, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	handle := windows.Handle(f.Fd())
	var overlapped windows.Overlapped
	if err := windows.LockFileEx(handle, windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, &overlapped); err != nil {
		_ = f.Close()
		return nil, err
	}
	return func() {
		_ = windows.UnlockFileEx(handle, 0, 1, 0, &overlapped)
		_ = f.Close()
	}, nil
}
