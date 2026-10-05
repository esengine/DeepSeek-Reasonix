//go:build linux

package projectiondb

import "syscall"

func filesystemRemote(path string) bool {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return false
	}
	switch uint64(stat.Type) {
	case 0x6969, 0x517B, 0xFF534D42, 0x6E667364, 0x00C36400, 0x0BD00BD0:
		return true
	default:
		return false
	}
}
