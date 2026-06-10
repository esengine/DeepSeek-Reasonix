//go:build !windows

package proc

import (
	"os/exec"
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
