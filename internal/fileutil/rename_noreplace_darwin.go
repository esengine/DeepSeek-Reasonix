//go:build darwin

package fileutil

import "golang.org/x/sys/unix"

func RenameNoReplace(oldPath, newPath string) error {
	return unix.RenameatxNp(unix.AT_FDCWD, oldPath, unix.AT_FDCWD, newPath, unix.RENAME_EXCL)
}
