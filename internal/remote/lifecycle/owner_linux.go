//go:build linux

package lifecycle

import (
	"os"
	"syscall"
)

func currentUID() int { return os.Geteuid() }

func ownerUID(info os.FileInfo) (int, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return int(stat.Uid), true
}
