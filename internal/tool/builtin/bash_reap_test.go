//go:build !windows

package builtin

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"reasonix/internal/proc"
)

// TestReapTreeKillsGroupStragglers covers #3702: a foreground command that
// backgrounds a child (here a long sleep, standing in for `bazel run`'s server)
// leaves it in the process group after Wait reaps the shell leader. reapTree must
// kill it so such processes don't accumulate into an OOM. The child redirects its
// fds and the pid is passed via a file so the inherited stdout can't block Wait.
func TestReapTreeKillsGroupStragglers(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	cmd := exec.CommandContext(context.Background(), "sh", "-c",
		"sleep 60 >/dev/null 2>&1 & echo $! > "+pidFile)
	setKillTree(cmd) // Setpgid — the shell leads its own group
	if err := cmd.Run(); err != nil {
		t.Fatalf("run: %v", err)
	}

	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("read pid file: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatalf("parse backgrounded pid %q: %v", data, err)
	}
	if err := syscall.Kill(pid, 0); err != nil {
		t.Skipf("backgrounded child %d not alive after shell exit (%v)", pid, err)
	}

	reapTree(cmd)

	dead := false
	for i := 0; i < 50; i++ {
		if syscall.Kill(pid, 0) != nil {
			dead = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !dead {
		_ = syscall.Kill(pid, syscall.SIGKILL) // don't leak the sleep in CI
		t.Fatalf("backgrounded child %d survived reapTree", pid)
	}
}

func TestShellPATHProbeDetachesControllingTerminal(t *testing.T) {
	cmd := exec.CommandContext(context.Background(), "sh", "-c", "true")
	proc.PrepareShellPATHProbe(cmd)

	if cmd.SysProcAttr == nil {
		t.Fatal("SysProcAttr is nil")
	}
	if !cmd.SysProcAttr.Setsid {
		t.Fatal("login shell PATH probe should run in a new session so an interactive shell cannot take the TUI foreground")
	}
}
