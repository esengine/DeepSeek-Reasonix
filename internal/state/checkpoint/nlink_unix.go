//go:build !windows

package checkpoint

import (
	"os"
	"syscall"
)

func fileNlink(fi os.FileInfo) uint64 {
	if fi == nil {
		return 1
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return uint64(st.Nlink)
	}
	return 1
}
