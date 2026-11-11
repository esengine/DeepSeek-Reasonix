//go:build windows

package cli

import (
	"os"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

const fileNameInfo = 2

// onCygwinPty reports whether f is one end of a Cygwin or MSYS terminal.
func onCygwinPty(f *os.File) bool {
	h := windows.Handle(f.Fd())
	if t, err := windows.GetFileType(h); err != nil || t != windows.FILE_TYPE_PIPE {
		return false
	}
	var buf [4 + 2*windows.MAX_PATH]byte
	if windows.GetFileInformationByHandleEx(h, fileNameInfo, &buf[0], uint32(len(buf))) != nil {
		return false
	}
	n := *(*uint32)(unsafe.Pointer(&buf[0])) / 2
	if n == 0 || int(n) > windows.MAX_PATH {
		return false
	}
	name := unsafe.Slice((*uint16)(unsafe.Pointer(&buf[4])), n)
	return cygwinPtyName(string(utf16.Decode(name)))
}
