package fileutil

import (
	"fmt"
	"os"
)

// RenameAside renames path to path+suffix, or path+suffix.N when that is taken,
// so a file or folder is set aside without ever overwriting an earlier one.
func RenameAside(path, suffix string) error {
	target := path + suffix
	for i := 1; ; i++ {
		if _, err := os.Lstat(target); os.IsNotExist(err) {
			break
		}
		target = fmt.Sprintf("%s%s.%d", path, suffix, i)
	}
	err := os.Rename(path, target)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
