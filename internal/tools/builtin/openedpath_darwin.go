//go:build darwin

package builtin

import (
	"os"
	"unsafe"

	"golang.org/x/sys/unix"
)

// openedPath is the path the kernel holds for an open file (F_GETPATH), which
// no rename or link swapped after a check can change.
func openedPath(f *os.File) (string, bool) {
	var buf [1024]byte
	if _, err := unix.FcntlInt(f.Fd(), unix.F_GETPATH, int(uintptr(unsafe.Pointer(&buf[0])))); err != nil {
		return "", false
	}
	n := 0
	for n < len(buf) && buf[n] != 0 {
		n++
	}
	return string(buf[:n]), n > 0
}
