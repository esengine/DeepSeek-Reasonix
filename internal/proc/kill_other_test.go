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
	if TrackTree(nil) != 0 {
		t.Fatal("TrackTree is a no-op off Windows; want 0")
	}
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	KillTracked(cmd, 0)

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("cmd.Wait blocked after KillTracked")
	}
}
