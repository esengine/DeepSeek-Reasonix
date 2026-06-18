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
	"reasonix/internal/sandbox"
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

func TestBashPreservesExplicitNoHupDisown(t *testing.T) {
	bashPath, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not found")
	}

	dir := t.TempDir()
	pidFile := filepath.Join(dir, "pid")
	command := "nohup sleep 60 >/dev/null 2>&1 & echo $! > " + shellQuote(pidFile) + "; disown"
	out, err := (bash{
		shell: sandbox.Shell{Kind: sandbox.ShellBash, Path: bashPath},
	}).Execute(context.Background(), argsJSON(t, map[string]any{"command": command}))
	if err != nil {
		t.Fatalf("bash Execute failed: %v (out=%q)", err, out)
	}

	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("read pid file: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatalf("parse backgrounded pid %q: %v", data, err)
	}
	defer func() { _ = syscall.Kill(pid, syscall.SIGKILL) }()

	time.Sleep(200 * time.Millisecond)
	if err := syscall.Kill(pid, 0); err != nil {
		t.Fatalf("nohup/disown child %d did not survive bash completion: %v", pid, err)
	}
}

func TestExplicitBackgroundKeepaliveDetection(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    bool
	}{
		{
			name:    "nohup background",
			command: "nohup python train.py >train.log 2>&1 &",
			want:    true,
		},
		{
			name:    "disown background",
			command: "sleep 60 >/dev/null 2>&1 & disown",
			want:    true,
		},
		{
			name:    "setsid background",
			command: "setsid sleep 60 >/dev/null 2>&1 &",
			want:    true,
		},
		{
			name:    "plain background still reaped",
			command: "sleep 60 >/dev/null 2>&1 &",
			want:    false,
		},
		{
			name:    "nohup without background still reaped",
			command: "nohup sleep 1",
			want:    false,
		},
		{
			name:    "quoted nohup argument ignored",
			command: "echo 'nohup sleep 60 &' &",
			want:    false,
		},
		{
			name:    "redirection ampersand ignored",
			command: "nohup sleep 1 2>&1",
			want:    false,
		},
		{
			name:    "redirect target before command ignored",
			command: "> nohup echo done &",
			want:    false,
		},
		{
			name:    "assignment before nohup",
			command: "CUDA_VISIBLE_DEVICES=0 nohup python train.py >/dev/null 2>&1 &",
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasExplicitBackgroundKeepalive(tt.command); got != tt.want {
				t.Fatalf("hasExplicitBackgroundKeepalive(%q) = %v, want %v", tt.command, got, tt.want)
			}
		})
	}
}

func TestShouldReapAfterRunHonorsExplicitPreserveOnlyOnCompletion(t *testing.T) {
	sh := sandbox.Shell{Kind: sandbox.ShellBash, Path: "bash"}
	if shouldReapAfterRun(context.Background(), sh, "sleep 60 >/dev/null 2>&1 &", true) {
		t.Fatal("preserve_background_processes should skip reap after normal completion")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if !shouldReapAfterRun(ctx, sh, "sleep 60 >/dev/null 2>&1 &", true) {
		t.Fatal("cancelled commands should still reap the process group")
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

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}
