//go:build !windows

package proc

import (
	"os/exec"
	"syscall"
)

// KillTree signals cmd's process group, falling back to the direct child.
// Descendants that leave the group are outside this termination boundary.
func KillTree(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
		_ = cmd.Process.Kill() // not a group leader — at least kill the child
	}
}

// SetProcessGroupKill starts cmd in a separate session/process group for
// KillTree and prevents it taking over the caller's controlling terminal.
// Group membership does not contain descendants that start another session.
func SetProcessGroupKill(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setsid = true
}

// StartTracked starts cmd in a separate session/process group. Off Windows it
// owns no OS handle; TrackedJob.Kill uses the process-group boundary of KillTree.
func StartTracked(cmd *exec.Cmd) (*TrackedJob, error) {
	SetProcessGroupKill(cmd)
	return &TrackedJob{}, cmd.Start()
}

// StartTrackedRequired retains StartTracked's process-group boundary on Unix.
// It cannot certify termination of descendants that escape that group.
func StartTrackedRequired(cmd *exec.Cmd) (*TrackedJob, error) { return StartTracked(cmd) }

// killTracked signals cmd's process group; the handle is unused off Windows.
func killTracked(cmd *exec.Cmd, _ uintptr) { KillTree(cmd) }

// finishTracked is a no-op off Windows, where StartTracked owns no OS handle.
func finishTracked(uintptr) {}
