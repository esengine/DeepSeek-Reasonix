package observation

import (
	"io/fs"
	"syscall"
)

func stampOf(info fs.FileInfo) stamp {
	s := stamp{Size: info.Size(), Mtime: info.ModTime().UnixNano()}
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		s.Ctime = st.Ctim.Nano()
		s.Ino = st.Ino
		s.Dev = uint64(st.Dev)
	}
	return s
}
