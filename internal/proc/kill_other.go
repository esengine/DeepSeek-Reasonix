//go:build !windows

package proc

import (
	"os/exec"
	"syscall"
)

// KillTree kills cmd's whole process group. StartTracked (and
// SetProcessGroupKill for children started outside it) put the child in a new
// session/process group, so the negative-pid signal reaches every descendant,
// including a launcher whose sub-daemon survives the parent, where a plain
// Process.Kill would only hit the direct child and orphan the grandchild.
func KillTree(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
		_ = cmd.Process.Kill() // not a group leader — at least kill the child
	}
}

// SetProcessGroupKill makes cmd start in its own session/process group so
// KillTree can reap its whole tree. The new session also keeps interactive
// children from taking over the caller's controlling terminal. Use it for
// children started outside StartTracked (e.g. a one-shot CombinedOutput). It is
// a no-op on Windows, where the Job Object that TrackTree/StartTracked assigns
// handles the tree instead.
func SetProcessGroupKill(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setsid = true
}

// StartTracked starts cmd in its own session/process group so TrackedJob.Kill /
// KillTree can reap its whole tree. Off Windows the process group is the
// equivalent of the Windows Job Object; it owns no handle, and TrackedJob.Kill
// falls back to KillTree.
func StartTracked(cmd *exec.Cmd) (*TrackedJob, error) {
	SetProcessGroupKill(cmd)
	return &TrackedJob{}, cmd.Start()
}

// killTracked terminates cmd's process tree; the handle is unused off Windows.
func killTracked(cmd *exec.Cmd, _ uintptr) { KillTree(cmd) }

// finishTracked is a no-op off Windows, where StartTracked owns no OS handle.
func finishTracked(uintptr) {}
