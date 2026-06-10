//go:build !windows

package proc

import (
	"log/slog"
	"os/exec"
	"syscall"
)

// SetProcessGroupKill configures cmd to start in its own process group so that
// KillTree (via negative PID) can reap the whole tree on context cancellation
// or exit. The Cancel handler catches the context-cancellation path; KillTree
// covers the explicit-close path.
func SetProcessGroupKill(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}

// KillTree terminates cmd's whole process group using the negative-pid trick:
// the child was started with Setpgid so it leads its own process group, and
// killing -PID reaches every descendant (not just the direct child). Without
// this, a launcher shell with a managed sub-daemon (e.g. codegraph's bundled
// node runtime) leaves orphan grandchildren alive after reasonix exits.
func KillTree(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
		slog.Warn("proc: kill process group failed (Setpgid may be missing; the child must start its own group)", "pid", cmd.Process.Pid, "err", err)
	}
}

// StartTracked starts cmd. Off Windows there is no Job Object to track it in —
// the platform reaps the child directly — so it just starts and returns a 0
// handle, and KillTracked falls back to KillTree.
func StartTracked(cmd *exec.Cmd) (uintptr, error) {
	return 0, cmd.Start()
}

// KillTracked terminates cmd's process tree; the handle is unused off Windows.
func KillTracked(cmd *exec.Cmd, _ uintptr) { KillTree(cmd) }
