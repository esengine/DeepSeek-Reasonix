package fileutil

import (
	"os"
	"syscall"
)

func cacheFileIdentity(info os.FileInfo) (fileIdentity, bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fileIdentity{}, false
	}
	return fileIdentity{uint64(st.Dev), st.Ino, st.Ctim.Nano()}, true
}
