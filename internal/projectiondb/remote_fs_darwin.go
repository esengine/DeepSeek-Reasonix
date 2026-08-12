//go:build darwin

package projectiondb

import (
	"strings"
	"syscall"
)

func filesystemRemote(path string) bool {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return false
	}
	name := make([]byte, 0, len(stat.Fstypename))
	for _, char := range stat.Fstypename {
		if char == 0 {
			break
		}
		name = append(name, byte(char))
	}
	switch strings.ToLower(string(name)) {
	case "nfs", "smbfs", "webdav", "afpfs", "cifs":
		return true
	default:
		return false
	}
}
