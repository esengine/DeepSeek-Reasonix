//go:build !darwin && !linux && !windows

package fileutil

import "os"

// Unpackaged ports retain a best-effort existence check. Reasonix's packaged
// Linux, macOS, and Windows targets each have an atomic no-replace primitive.
func RenameNoReplace(oldPath, newPath string) error {
	if _, err := os.Lstat(newPath); err == nil {
		return os.ErrExist
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.Rename(oldPath, newPath)
}
