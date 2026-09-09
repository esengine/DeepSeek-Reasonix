//go:build windows

package main

import (
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

var errEndpointStarting = errors.New("instance mutex exists without an identifiable endpoint")

var handoffUser32 = windows.NewLazySystemDLL("user32.dll")
var findInstanceWindow = handoffUser32.NewProc("FindWindowExW")
var instanceWindowPID = handoffUser32.NewProc("GetWindowThreadProcessId")

// These names match Wails v2's Windows single-instance backend. HWND_MESSAGE
// is required: ordinary top-level window enumeration does not include them.
func desktopEndpointImage(id string) (string, error) {
	name := "wails-app-" + id
	class, err := windows.UTF16PtrFromString(name + "-sic")
	if err != nil {
		return "", err
	}
	title, err := windows.UTF16PtrFromString(name + "-siw")
	if err != nil {
		return "", err
	}
	hwnd, _, _ := findInstanceWindow.Call(^uintptr(2), 0, uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(title)))
	if hwnd == 0 {
		mutexName, _ := windows.UTF16PtrFromString(name + "sim")
		mutex, err := windows.OpenMutex(windows.SYNCHRONIZE, false, mutexName)
		if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
			return "", nil
		}
		if err != nil {
			return "", err
		}
		_ = windows.CloseHandle(mutex)
		return "", errEndpointStarting
	}
	var pid uint32
	if result, _, err := instanceWindowPID.Call(hwnd, uintptr(unsafe.Pointer(&pid))); result == 0 || pid == 0 {
		return "", fmt.Errorf("identify instance owner: %w", err)
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE, false, pid)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(handle)
	var owner uint32
	instanceWindowPID.Call(hwnd, uintptr(unsafe.Pointer(&owner)))
	if owner != pid {
		return "", fmt.Errorf("instance owner changed during inspection")
	}
	size := uint32(32768)
	buffer := make([]uint16, size)
	if err := windows.QueryFullProcessImageName(handle, 0, &buffer[0], &size); err != nil {
		return "", err
	}
	result, err := windows.WaitForSingleObject(handle, 0)
	if err != nil || result != uint32(windows.WAIT_TIMEOUT) {
		return "", fmt.Errorf("instance exited during inspection")
	}
	return windows.UTF16ToString(buffer[:size]), nil
}

func notifyHandoffBlocked(installed bool) {
	message := "Reasonix could not finish the update restart. Choose Quit in the old Reasonix window, then open Reasonix again. Your running instance was preserved."
	if installed {
		message = "The new version is installed, but restart is incomplete. Choose Quit in the old Reasonix window, then open Reasonix again. Your running instance was preserved."
	}
	message += "\n\n请在旧 Reasonix 窗口中选择退出，再重新打开 Reasonix。现有实例已保留。"
	title, _ := windows.UTF16PtrFromString("Reasonix update / 更新")
	body, _ := windows.UTF16PtrFromString(message)
	// The helper runs after the desktop has exited; log-only failures are invisible.
	handoffUser32.NewProc("MessageBoxW").Call(0, uintptr(unsafe.Pointer(body)), uintptr(unsafe.Pointer(title)), 0x00000030|0x00010000)
}
