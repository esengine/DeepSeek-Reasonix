//go:build windows

package terminal

import (
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

type proc struct {
	pid     uint32
	name    string
	console uint32
}

// helpers are console and subsystem processes that live beside a shell without
// being a command it waits on.
var helpers = map[string]bool{"conhost.exe": true, "openconsole.exe": true, "wslhost.exe": true}

// Busy is whether something other than the shell is attached to the shell's
// console. The console says it, not the parent chain: an MSYS child is not
// parented by the shell that forked it. A shell may also sit behind a launcher
// of its own name (Git's bin\bash.exe starts the real bash), so processes with
// the shell's image name are the shell; a subshell of the same name reads as idle.
func (p *winPty) Busy() bool { return busyOn(uint32(p.c.Pid()), snapshot()) }

func snapshot() []proc {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snap)
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	var all []proc
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		all = append(all, proc{pid: e.ProcessID, name: strings.ToLower(windows.UTF16ToString(e.ExeFile[:])), console: consoleOf(e.ProcessID)})
	}
	return all
}

// consoleOf is the process that hosts the console pid is attached to, or 0.
func consoleOf(pid uint32) uint32 {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return 0
	}
	defer windows.CloseHandle(h)
	var host uintptr
	if err := windows.NtQueryInformationProcess(h, windows.ProcessConsoleHostProcess, unsafe.Pointer(&host), uint32(unsafe.Sizeof(host)), nil); err != nil {
		return 0
	}
	return uint32(host &^ 3)
}

func busyOn(root uint32, all []proc) bool {
	var shell proc
	for _, p := range all {
		if p.pid == root {
			shell = p
		}
	}
	if shell.name == "" || shell.console == 0 {
		return false
	}
	for _, p := range all {
		if p.console == shell.console && p.pid != root && p.name != shell.name && !helpers[p.name] {
			return true
		}
	}
	return false
}
