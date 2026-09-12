//go:build windows

package serve

import (
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

func platformReasonixSessionHolderProcess(pid int) bool {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(handle)
	buffer := make([]uint16, 32768)
	size := uint32(len(buffer))
	if windows.QueryFullProcessImageName(handle, 0, &buffer[0], &size) != nil {
		return false
	}
	name := strings.ToLower(filepath.Base(windows.UTF16ToString(buffer[:size])))
	return name == "reasonix.exe" || name == "reasonix-cli.exe"
}

func platformTerminateReasonixSessionHolder(pid int) error {
	handle, err := windows.OpenProcess(windows.PROCESS_TERMINATE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)
	return windows.TerminateProcess(handle, 1)
}
