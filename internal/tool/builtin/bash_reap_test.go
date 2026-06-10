//go:build !windows

package builtin

import (
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestReapTreeKillsGroupStragglers covers #3702: a foreground command that
// backgrounds a child (here a long sleep, standing in for `bazel run`'s server)
// leaves it in the process group after Wait reaps the shell leader. reapTree must
// kill it so such processes don't accumulate into an OOM.
func TestReapTreeKillsGroupStragglers(t *testing.T) {
	cmd := exec.Command("sh", "-c", "sleep 60 & echo $!")
	setKillTree(cmd) // Setpgid — the shell leads its own group
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		t.Fatalf("parse backgrounded pid %q: %v", out, err)
	}
	if err := syscall.Kill(pid, 0); err != nil {
		t.Skipf("backgrounded child %d not alive after shell exit (%v)", pid, err)
	}

	reapTree(cmd)

	dead := false
	for i := 0; i < 50; i++ {
		if err := syscall.Kill(pid, 0); err != nil {
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
