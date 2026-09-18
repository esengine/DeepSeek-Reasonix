//go:build windows

package fileutil

import (
	"os"

	"golang.org/x/sys/windows"
)

func RenameNoReplace(oldPath, newPath string) error {
	from, err := windows.UTF16PtrFromString(oldPath)
	if err != nil {
		return &os.LinkError{Op: "rename", Old: oldPath, New: newPath, Err: err}
	}
	to, err := windows.UTF16PtrFromString(newPath)
	if err != nil {
		return &os.LinkError{Op: "rename", Old: oldPath, New: newPath, Err: err}
	}
	// MoveFileEx without MOVEFILE_REPLACE_EXISTING fails atomically when newPath
	// already exists. os.Rename cannot be used here: Go passes the replace flag
	// on Windows.
	if err := windows.MoveFileEx(from, to, windows.MOVEFILE_WRITE_THROUGH); err != nil {
		return &os.LinkError{Op: "rename", Old: oldPath, New: newPath, Err: err}
	}
	return nil
}
