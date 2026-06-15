//go:build windows

package proc

import (
	"errors"
	"io"
	"os/exec"
	"testing"
	"time"
)

// A launcher (cmd.exe) that spawns a long-lived grandchild (ping) which inherits
// the stdout pipe: killing only the direct child leaves the grandchild holding
// the pipe, so cmd.Wait blocks until the grandchild exits. KillTree must take
// the whole tree down so Wait returns promptly.
func TestKillTreeUnblocksWaitOnSurvivingGrandchild(t *testing.T) {
	cmd := exec.Command("cmd", "/c", "ping", "-n", "30", "127.0.0.1")
	HideWindow(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("StdoutPipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	go func() { _, _ = io.Copy(io.Discard, stdout) }()
	time.Sleep(500 * time.Millisecond) // let cmd.exe exec the ping grandchild

	KillTree(cmd)

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(8 * time.Second):
		t.Fatal("cmd.Wait blocked after KillTree — grandchild survived holding the pipe")
	}
}

// StartTracked must create a Job Object for the started process, and KillTracked
// must take the whole tracked tree down (the job reaps even descendants a plain
// taskkill /T would miss — see the codegraph daemon leak this guards against).
func TestKillTrackedReapsTrackedTree(t *testing.T) {
	cmd := exec.Command("cmd", "/c", "ping", "-n", "30", "127.0.0.1")
	HideWindow(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("StdoutPipe: %v", err)
	}
	job, err := StartTracked(cmd)
	if err != nil {
		t.Fatalf("StartTracked: %v", err)
	}
	if job == 0 {
		t.Fatal("StartTracked returned 0 — job object not created")
	}
	go func() { _, _ = io.Copy(io.Discard, stdout) }()
	time.Sleep(500 * time.Millisecond) // let cmd.exe exec the ping grandchild into the job

	KillTracked(cmd, job)

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(8 * time.Second):
		t.Fatal("cmd.Wait blocked after KillTracked — tracked tree survived")
	}
}

// StartTracked creates the child suspended to win the job-assignment race, so it
// must resume it or the process hangs forever. A child that exits with a known
// code proves the resume fired: Wait returns that code instead of timing out.
func TestStartTrackedResumesSuspendedChild(t *testing.T) {
	cmd := exec.Command("cmd", "/c", "exit", "7")
	HideWindow(cmd)
	if _, err := StartTracked(cmd); err != nil {
		t.Fatalf("StartTracked: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		var ee *exec.ExitError
		if !errors.As(err, &ee) || ee.ExitCode() != 7 {
			t.Fatalf("exit = %v, want exit status 7", err)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("cmd.Wait blocked — StartTracked left the child suspended")
	}
}
