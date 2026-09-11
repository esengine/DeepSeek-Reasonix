//go:build windows

package appidentity

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

func (s *loadedShortcut) setString(method uintptr, value string) error {
	pointer, err := windows.UTF16PtrFromString(value)
	if err != nil {
		return err
	}
	hr, _, _ := syscall.SyscallN(method, uintptr(unsafe.Pointer(s.link)), uintptr(unsafe.Pointer(pointer)))
	return checkHRESULT("IShellLinkW string update", hr)
}

func (s *loadedShortcut) iconLocation() (string, int32, error) {
	buffer := make([]uint16, windowsPathBuffer)
	var index int32
	hr, _, _ := syscall.SyscallN(s.link.VTable.GetIconLocation, uintptr(unsafe.Pointer(s.link)),
		uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)), uintptr(unsafe.Pointer(&index)))
	return windows.UTF16ToString(buffer), index, checkHRESULT("IShellLinkW.GetIconLocation", hr)
}

func (s *loadedShortcut) setIconLocation(path string, index int32) error {
	pointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	hr, _, _ := syscall.SyscallN(s.link.VTable.SetIconLocation, uintptr(unsafe.Pointer(s.link)),
		uintptr(unsafe.Pointer(pointer)), uintptr(index))
	return checkHRESULT("IShellLinkW.SetIconLocation", hr)
}
