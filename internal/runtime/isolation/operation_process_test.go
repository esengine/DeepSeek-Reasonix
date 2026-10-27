package isolation

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	"reasonix/internal/platform/gitcmd"
)

func TestResultOperationProcessHelper(t *testing.T) {
	for i, arg := range os.Args {
		if arg != "--result-operation-helper" {
			continue
		}
		if len(os.Args) != i+3 {
			os.Exit(2)
		}
		s := NewStore(os.Args[i+1], "", gitcmd.Repo{})
		release, err := s.acquireResultOperation(context.Background(), os.Args[i+2], false)
		if err != nil {
			os.Exit(3)
		}
		fmt.Println("held")
		bufio.NewReader(os.Stdin).ReadString('\n')
		release()
		os.Exit(0)
	}
}

func TestResultOperationUsesCrossProcessOwnership(t *testing.T) {
	s, e := finishedOperationResult(t)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "-test.run=^TestResultOperationProcessHelper$", "--", "--result-operation-helper", s.managedRoot, e.ID)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waited := false
	t.Cleanup(func() {
		stdin.Close()
		if !waited {
			cmd.Process.Kill()
			cmd.Wait()
		}
	})
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || line != "held\n" {
		t.Fatalf("process did not publish ownership: %q %v", line, err)
	}
	peer := NewStore(s.managedRoot, s.workspace, s.repo)
	requireResultApplyWaitsAndCancels(t, peer, e.ID)
	if got := read(t, s.workspace, "a.txt"); got != "one\n" {
		t.Errorf("blocked settlement changed source: %q", got)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	err = cmd.Wait()
	waited = true
	if err == nil {
		t.Fatal("helper was not interrupted")
	}
	if _, err := peer.Apply(t.Context(), e.ID); err != nil {
		t.Fatalf("process death did not free the operation lease: %v", err)
	}
	if got := read(t, s.workspace, "a.txt"); got != "candidate\n" {
		t.Fatalf("settlement after process death = %q", got)
	}
}
