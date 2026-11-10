//go:build windows

package proc

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
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
// must take the whole tracked tree down, including descendants a plain taskkill
// /T would miss.
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
	if !job.Tracked() {
		t.Fatal("StartTracked reported no handle — job object not created")
	}
	go func() { _, _ = io.Copy(io.Discard, stdout) }()
	time.Sleep(500 * time.Millisecond) // let cmd.exe exec the ping grandchild into the job

	job.Kill(cmd)

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

// slowTaskkill puts a taskkill.exe that never returns first on PATH, the way a
// wedged system tool (a busy WMI, security software holding the call) behaves.
func slowTaskkill(t *testing.T) {
	t.Helper()
	dir := testenv.TempDir(t)
	src := filepath.Join(dir, "main.go")
	if err := os.WriteFile(src, []byte("package main\nimport \"time\"\nfunc main() { time.Sleep(10 * time.Minute) }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	build := exec.Command("go", "build", "-o", filepath.Join(dir, "taskkill.exe"), src)
	build.Dir = dir
	if out, err := build.CombinedOutput(); err != nil {
		t.Skipf("cannot build the fake taskkill: %v\n%s", err, out)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// A taskkill that hangs must not hang the stop: KillTree runs inside
// exec.Cmd's cancel hook, and Wait does not return until that hook has.
func TestKillTreeDoesNotWaitForeverOnAHungTaskkill(t *testing.T) {
	slowTaskkill(t)
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, "cmd", "/c", "ping", "-n", "300", "127.0.0.1")
	cmd.WaitDelay = time.Second
	SetCancelKillsTree(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	start := time.Now()
	cancel()
	select {
	case <-done:
		t.Logf("returned after %v", time.Since(start))
	case <-time.After(40 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("the command was still running 40s after cancel: a hung taskkill held the stop")
	}
}
