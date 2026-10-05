//go:build darwin

package repair

import "golang.org/x/sys/unix"

func renameRepairNodeNoReplace(oldPath, newPath string) error {
	return unix.RenameatxNp(unix.AT_FDCWD, oldPath, unix.AT_FDCWD, newPath, unix.RENAME_EXCL)
}
