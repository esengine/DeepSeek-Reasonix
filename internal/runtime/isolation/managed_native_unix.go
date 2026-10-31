//go:build darwin || linux || freebsd || netbsd || openbsd || dragonfly

package isolation

import (
	"fmt"
	"os"
	"syscall"
)

func candidateDirectoryID(file *os.File) (string, error) {
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() {
		return "", errUnavailable
	}
	return fmt.Sprintf("unix:%d:%d", stat.Dev, stat.Ino), nil
}
