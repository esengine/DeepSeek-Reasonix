//go:build windows

package tree_sitter

/*
#include <windows.h>
HANDLE _ts_dup(HANDLE handle);
*/
import "C"
import "unsafe"

// Wrapper for Windows systems
func dupeFD(handle uintptr) uintptr {
	hHandle := C.HANDLE(unsafe.Pointer(handle))
	return uintptr(unsafe.Pointer(C._ts_dup(hHandle)))
}
