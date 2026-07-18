//go:build windows

package main

import (
	"log/slog"
	"os"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// killWebView2Processes terminates lingering msedgewebview2.exe child processes
// that may hold file locks on session data after the main window closes.
// Called from App.shutdown() — runs once, ~3-10 ms.
func killWebView2Processes() {
	myPid := uint32(os.Getpid())

	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		slog.Warn("desktop: killWebView2Processes: CreateToolhelp32Snapshot failed", "err", err)
		return
	}
	defer windows.CloseHandle(snap)

	var pe windows.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))

	for err := windows.Process32First(snap, &pe); err == nil; err = windows.Process32Next(snap, &pe) {
		name := windows.UTF16ToString(pe.ExeFile[:])
		if !strings.EqualFold(name, "msedgewebview2.exe") {
			continue
		}
		if pe.ParentProcessID != myPid {
			continue
		}

		handle, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, pe.ProcessID)
		if err != nil {
			slog.Debug("desktop: killWebView2Processes: OpenProcess failed (already exited?)",
				"pid", pe.ProcessID, "err", err)
			continue
		}

		if err := windows.TerminateProcess(handle, 0); err != nil {
			slog.Warn("desktop: killWebView2Processes: TerminateProcess failed",
				"pid", pe.ProcessID, "err", err)
		} else {
			slog.Debug("desktop: killWebView2Processes: terminated",
				"pid", pe.ProcessID, "name", name)
		}
		windows.CloseHandle(handle)
	}
}

// killOrphanWebView2 cleans up WebView2 processes left behind by a previous
// crash (parent PID no longer exists). Called from App.startup().
func killOrphanWebView2() {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return
	}
	defer windows.CloseHandle(snap)

	var pe windows.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))

	for err := windows.Process32First(snap, &pe); err == nil; err = windows.Process32Next(snap, &pe) {
		name := windows.UTF16ToString(pe.ExeFile[:])
		if !strings.EqualFold(name, "msedgewebview2.exe") {
			continue
		}
		parent := pe.ParentProcessID
		if parent == 0 {
			continue
		}
		h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, parent)
		if err == nil {
			windows.CloseHandle(h)
			continue
		}
		h2, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, pe.ProcessID)
		if err != nil {
			continue
		}
		windows.TerminateProcess(h2, 0)
		windows.CloseHandle(h2)
	}
}