//go:build windows

package proc

import (
	"os/exec"
	"strconv"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// SetProcessGroupKill is a no-op on Windows: the Job Object that StartTracked
// assigns reaps the whole tree on close, so Setpgid (which doesn't exist here)
// is unnecessary. It exists so non-Windows callers can request group kill
// uniformly.
func SetProcessGroupKill(*exec.Cmd) {}

// KillTree terminates cmd and every descendant it spawned. Process.Kill only
// signals the direct child, so a launcher (cmd.exe → node.exe) leaves the
// grandchild alive holding the inherited stdout/stderr pipes — which makes
// cmd.Wait block forever. taskkill /T walks the live tree and kills it all.
func KillTree(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	kill := exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(cmd.Process.Pid))
	HideWindow(kill)
	_ = kill.Run()
	_ = cmd.Process.Kill()
}

// StartTracked starts cmd inside a new Job Object whose KILL_ON_JOB_CLOSE flag
// fells the whole tree — including a launcher's detached grandchild (cmd.exe →
// node.exe, as the CodeGraph daemon re-parents itself off the launcher) — when
// the handle closes via KillTracked or an abrupt reasonix exit. The child is
// created suspended and assigned to the job before it runs, so a fast shim can
// no longer exec its grandchild and exit before assignment, orphaning a node
// the job never captured (#3747). It is always resumed before returning, even
// when job assignment fails, so a child is never left wedged suspended. Returns
// the job handle, 0 if it could not be created — then KillTracked relies on
// KillTree alone.
func StartTracked(cmd *exec.Cmd) (uintptr, error) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_SUSPENDED
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	defer resumeProcess(uint32(cmd.Process.Pid))
	return assignJob(cmd), nil
}

func assignJob(cmd *exec.Cmd) uintptr {
	if cmd == nil || cmd.Process == nil {
		return 0
	}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		_ = windows.CloseHandle(job)
		return 0
	}
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		_ = windows.CloseHandle(job)
		return 0
	}
	defer func() { _ = windows.CloseHandle(h) }()
	if err := windows.AssignProcessToJobObject(job, h); err != nil {
		_ = windows.CloseHandle(job)
		return 0
	}
	return uintptr(job)
}

// resumeProcess resumes every thread of pid. A CREATE_SUSPENDED process has a
// single suspended primary thread, so this releases it once the job is assigned.
func resumeProcess(pid uint32) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return
	}
	defer func() { _ = windows.CloseHandle(snap) }()
	var te windows.ThreadEntry32
	te.Size = uint32(unsafe.Sizeof(te))
	for err := windows.Thread32First(snap, &te); err == nil; err = windows.Thread32Next(snap, &te) {
		if te.OwnerProcessID != pid {
			continue
		}
		th, err := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, te.ThreadID)
		if err != nil {
			continue
		}
		_, _ = windows.ResumeThread(th)
		_ = windows.CloseHandle(th)
	}
}

// KillTracked terminates cmd's whole process tree. When job (from StartTracked)
// is non-zero, terminating it kills even detached descendants; the KillTree pass
// then catches anything spawned in the gap before the job was assigned.
func KillTracked(cmd *exec.Cmd, job uintptr) {
	if job != 0 {
		_ = windows.TerminateJobObject(windows.Handle(job), 1)
		_ = windows.CloseHandle(windows.Handle(job))
	}
	KillTree(cmd)
}
