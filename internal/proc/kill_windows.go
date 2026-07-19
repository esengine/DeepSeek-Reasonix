//go:build windows

package proc

import (
	"errors"
	"fmt"
	"os"
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
	killTreeWithTaskkill(cmd, func(pid int) {
		kill := exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(pid))
		HideWindow(kill)
		_ = kill.Run()
	})
	_ = cmd.Process.Kill()
}

// killTreeWithTaskkill runs the PID-based tree walk only while os.Process still
// owns (or can transiently borrow) its native process handle. That handle pins
// the process object and therefore its numeric PID until taskkill returns. If
// cmd.Wait has already released the handle, the PID may be reusable and the
// taskkill pass is deliberately skipped.
func killTreeWithTaskkill(cmd *exec.Cmd, taskkill func(int)) {
	if cmd == nil || cmd.Process == nil || taskkill == nil {
		return
	}
	_ = cmd.Process.WithHandle(func(uintptr) {
		taskkill(cmd.Process.Pid)
	})
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
	return startTracked(cmd, false)
}

// StartTrackedRequired is the fail-closed form used when orphaned descendants
// would violate the caller's lifecycle contract. If a Job Object cannot be
// assigned, the still-suspended child is terminated and reaped before it can
// spawn anything.
func StartTrackedRequired(cmd *exec.Cmd) (uintptr, error) {
	return startTracked(cmd, true)
}

func startTracked(cmd *exec.Cmd, requireJob bool) (uintptr, error) {
	return startTrackedWithJobAssigner(cmd, requireJob, assignJob)
}

func startTrackedWithJobAssigner(cmd *exec.Cmd, requireJob bool, assign func(*exec.Cmd) uintptr) (uintptr, error) {
	return startTrackedWithHooks(cmd, requireJob, assign, resumeProcess, terminateAndReapStartedProcess)
}

// resume must return an error only while the child remains suspended. The
// cleanup callback is consequently a pre-resume rollback, not a termination
// path for a process tree that has already begun running.
func startTrackedWithHooks(
	cmd *exec.Cmd,
	requireJob bool,
	assign func(*exec.Cmd) uintptr,
	resume func(uint32) error,
	cleanup func(*exec.Cmd, uintptr) error,
) (uintptr, error) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_SUSPENDED
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	job := assign(cmd)
	if requireJob && job == 0 {
		return 0, errors.Join(ErrProcessTrackingUnavailable, cleanup(cmd, 0))
	}
	if err := resume(uint32(cmd.Process.Pid)); err != nil {
		resumeErr := fmt.Errorf("resume suspended process %d: %w", cmd.Process.Pid, err)
		cleanupErr := cleanup(cmd, job)
		if requireJob {
			return 0, errors.Join(ErrProcessTrackingUnavailable, resumeErr, cleanupErr)
		}
		return 0, errors.Join(resumeErr, cleanupErr)
	}
	return job, nil
}

// terminateAndReapStartedProcess is used only before a suspended child has
// been returned to a caller. It closes any assigned Job Object, directly kills
// the root as a fallback, and synchronously reaps it so failure cannot strand a
// suspended child or leak a process handle.
func terminateAndReapStartedProcess(cmd *exec.Cmd, job uintptr) error {
	var cleanupErrors []error
	if job != 0 {
		handle := windows.Handle(job)
		if err := windows.TerminateJobObject(handle, 1); err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("terminate job object: %w", err))
		}
		if err := windows.CloseHandle(handle); err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("close job object: %w", err))
		}
	}
	if cmd == nil || cmd.Process == nil {
		cleanupErrors = append(cleanupErrors, errors.New("started process is unavailable for termination"))
		return errors.Join(cleanupErrors...)
	}
	if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		cleanupErrors = append(cleanupErrors, fmt.Errorf("terminate suspended process: %w", err))
	}
	waitErr := cmd.Wait()
	var exitErr *exec.ExitError
	if waitErr != nil && !errors.As(waitErr, &exitErr) && !errors.Is(waitErr, os.ErrProcessDone) {
		cleanupErrors = append(cleanupErrors, fmt.Errorf("reap suspended process: %w", waitErr))
	}
	if cmd.ProcessState == nil {
		cleanupErrors = append(cleanupErrors, errors.New("suspended process was not reaped"))
	}
	return errors.Join(cleanupErrors...)
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

type resumeProcessOperations struct {
	createSnapshot func() (windows.Handle, error)
	threadFirst    func(windows.Handle, *windows.ThreadEntry32) error
	threadNext     func(windows.Handle, *windows.ThreadEntry32) error
	openThread     func(uint32) (windows.Handle, error)
	threadOwner    func(windows.Handle) (uint32, error)
	resumeThread   func(windows.Handle) (uint32, error)
	closeHandle    func(windows.Handle) error
}

var getProcessIDOfThread = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetProcessIdOfThread")

func nativeThreadOwner(thread windows.Handle) (uint32, error) {
	owner, _, callErr := getProcessIDOfThread.Call(uintptr(thread))
	if owner != 0 {
		return uint32(owner), nil
	}
	if callErr != nil && !errors.Is(callErr, windows.ERROR_SUCCESS) {
		return 0, callErr
	}
	return 0, errors.New("GetProcessIdOfThread returned zero")
}

var nativeResumeProcessOperations = resumeProcessOperations{
	createSnapshot: func() (windows.Handle, error) {
		return windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	},
	threadFirst: windows.Thread32First,
	threadNext:  windows.Thread32Next,
	openThread: func(threadID uint32) (windows.Handle, error) {
		return windows.OpenThread(
			windows.THREAD_SUSPEND_RESUME|windows.THREAD_QUERY_LIMITED_INFORMATION,
			false,
			threadID,
		)
	},
	threadOwner:  nativeThreadOwner,
	resumeThread: windows.ResumeThread,
	closeHandle:  windows.CloseHandle,
}

// resumeProcess resumes the single primary thread of pid. A CREATE_SUSPENDED
// process cannot create another thread before that primary thread runs, so more
// than one matching snapshot entry is treated as an unsafe pre-resume state.
func resumeProcess(pid uint32) error {
	return resumeProcessWithOperations(pid, nativeResumeProcessOperations)
}

func resumeProcessWithOperations(pid uint32, ops resumeProcessOperations) error {
	thread, threadID, err := discoverSuspendedProcessThread(pid, ops)
	if err != nil {
		return err
	}

	previousSuspendCount, resumeErr := ops.resumeThread(thread)
	if resumeErr != nil {
		closeErr := ops.closeHandle(thread)
		return errors.Join(
			fmt.Errorf("resume process thread %d: %w", threadID, resumeErr),
			wrapOptionalError("close process thread", closeErr),
		)
	}
	if previousSuspendCount > 1 {
		closeErr := ops.closeHandle(thread)
		return errors.Join(
			fmt.Errorf(
				"resume process thread %d left it suspended (previous suspend count %d)",
				threadID, previousSuspendCount,
			),
			wrapOptionalError("close process thread", closeErr),
		)
	}

	// ResumeThread succeeding is the lifecycle commit point: the child may now
	// run when its previous suspend count was zero or one. A later CloseHandle
	// failure must not be returned to startTracked,
	// whose error rollback is deliberately restricted to a still-suspended
	// child. The native handle is valid and CloseHandle is still attempted.
	_ = ops.closeHandle(thread)
	return nil
}

// discoverSuspendedProcessThread completes every fallible discovery operation,
// including closing the Toolhelp snapshot, before returning a thread handle to
// resumeProcessWithOperations. On any error it also closes an already-opened
// thread handle, so callers only receive a handle when ResumeThread is the sole
// remaining operation that can fail before the child becomes runnable.
func discoverSuspendedProcessThread(
	pid uint32,
	ops resumeProcessOperations,
) (thread windows.Handle, threadID uint32, returnErr error) {
	snap, err := ops.createSnapshot()
	if err != nil {
		return 0, 0, fmt.Errorf("snapshot process threads: %w", err)
	}
	defer func() {
		if err := ops.closeHandle(snap); err != nil {
			returnErr = errors.Join(returnErr, fmt.Errorf("close thread snapshot: %w", err))
		}
		if returnErr != nil && thread != 0 {
			closeErr := ops.closeHandle(thread)
			returnErr = errors.Join(returnErr, wrapOptionalError("close process thread", closeErr))
			thread = 0
			threadID = 0
		}
	}()

	var te windows.ThreadEntry32
	te.Size = uint32(unsafe.Sizeof(te))
	if err := ops.threadFirst(snap, &te); err != nil {
		returnErr = fmt.Errorf("read first process thread: %w", err)
		return
	}
	for {
		if te.OwnerProcessID == pid {
			if thread != 0 {
				returnErr = fmt.Errorf(
					"multiple threads found for suspended process %d: %d and %d",
					pid, threadID, te.ThreadID,
				)
				return
			}
			openedThread, err := ops.openThread(te.ThreadID)
			if err != nil {
				returnErr = fmt.Errorf("open process thread %d: %w", te.ThreadID, err)
				return
			}
			thread = openedThread
			threadID = te.ThreadID
			ownerPID, err := ops.threadOwner(thread)
			if err != nil {
				returnErr = fmt.Errorf("verify process thread %d owner: %w", threadID, err)
				return
			}
			if ownerPID != pid {
				returnErr = fmt.Errorf(
					"process thread %d owner changed from %d to %d",
					threadID, pid, ownerPID,
				)
				return
			}
		}
		err = ops.threadNext(snap, &te)
		if errors.Is(err, windows.ERROR_NO_MORE_FILES) {
			break
		}
		if err != nil {
			returnErr = fmt.Errorf("read next process thread: %w", err)
			return
		}
	}
	if thread == 0 {
		returnErr = fmt.Errorf("no threads found for suspended process %d", pid)
		return
	}
	return
}

func wrapOptionalError(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", operation, err)
}

// KillTracked terminates cmd's whole process tree. A non-zero job was assigned
// while the root was still suspended, so every descendant is job-owned and no
// PID-based taskkill fallback is needed. Avoiding that fallback is essential if
// cmd.Wait raced with cancellation and the reaped root PID has already become
// reusable. Process.Kill is handle/state based and is therefore a safe fallback
// if terminating or closing the Job Object did not stop the root.
func KillTracked(cmd *exec.Cmd, job uintptr) {
	if job == 0 {
		KillTree(cmd)
		return
	}
	FinishTracked(job)
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}

// FinishTracked releases a completed command's Job Object. Closing a job with
// KILL_ON_JOB_CLOSE also terminates any descendants that outlived the tracked
// root, without running taskkill against a PID that cmd.Wait has already made
// eligible for reuse.
func FinishTracked(job uintptr) {
	if job == 0 {
		return
	}
	_ = windows.TerminateJobObject(windows.Handle(job), 1)
	_ = windows.CloseHandle(windows.Handle(job))
}
