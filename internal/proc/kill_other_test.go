//go:build !windows

package proc

import (
	"bufio"
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestKillTreeTerminatesChild(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	KillTree(cmd)

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("cmd.Wait blocked after KillTree")
	}
}

func TestKillTrackedTerminatesChild(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	job, err := StartTracked(cmd)
	if err != nil {
		t.Fatalf("StartTracked: %v", err)
	}
	if job != 0 {
		t.Fatalf("StartTracked job = %d off Windows; want 0", job)
	}

	KillTracked(cmd, job)

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("cmd.Wait blocked after KillTracked")
	}
}

// A launcher (sh) that backgrounds a grandchild (sleep) and stays alive: with
// the child in its own process group, KillTracked's negative-pid kill must reap
// the grandchild too, not just sh.
func TestKillTrackedReapsProcessGroupGrandchild(t *testing.T) {
	cmd := exec.Command("sh", "-c", "sleep 60 & echo $!; wait")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("StdoutPipe: %v", err)
	}
	if _, err := StartTracked(cmd); err != nil {
		t.Fatalf("StartTracked: %v", err)
	}

	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatalf("read grandchild pid: %v", err)
	}
	gcPid, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || gcPid <= 0 {
		t.Fatalf("bad grandchild pid %q: %v", line, err)
	}
	if syscall.Kill(gcPid, 0) != nil {
		t.Fatalf("grandchild %d not alive before kill", gcPid)
	}

	KillTracked(cmd, 0)
	_ = cmd.Wait()

	deadline := time.Now().Add(5 * time.Second)
	for !errors.Is(syscall.Kill(gcPid, 0), syscall.ESRCH) {
		if time.Now().After(deadline) {
			t.Fatalf("grandchild %d survived KillTracked — process group not reaped", gcPid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
