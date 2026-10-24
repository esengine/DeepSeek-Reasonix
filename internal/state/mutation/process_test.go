package mutation

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestMutationProcessHelper(t *testing.T) {
	if os.Getenv("REASONIX_MUTATION_TEST_HELPER") != "1" {
		return
	}
	c, err := New(os.Getenv("REASONIX_MUTATION_TEST_ROOT"), os.Getenv("REASONIX_MUTATION_TEST_STATE"), nil)
	if err != nil {
		t.Fatal(err)
	}
	l, err := c.Acquire(t.Context(), intent(os.Getenv("REASONIX_MUTATION_TEST_PATH")))
	if err != nil {
		t.Fatal(err)
	}
	defer l.End(NotExecuted)
	fmt.Println("mutation-ready")
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
}

func TestCrossProcessClaimAndCrashRecovery(t *testing.T) {
	root, state := t.TempDir(), t.TempDir()
	path := filepath.Join(root, "a")
	cmd := exec.Command(os.Args[0], "-test.run=^TestMutationProcessHelper$")
	cmd.Env = append(os.Environ(), "REASONIX_MUTATION_TEST_HELPER=1", "REASONIX_MUTATION_TEST_ROOT="+root,
		"REASONIX_MUTATION_TEST_STATE="+state, "REASONIX_MUTATION_TEST_PATH="+path)
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = in.Close(); _ = cmd.Process.Kill(); _ = cmd.Wait() })
	ready := make(chan string, 1)
	go func() { line, _ := bufio.NewReader(out).ReadString('\n'); ready <- line }()
	select {
	case line := <-ready:
		if line != "mutation-ready\n" {
			t.Fatal(line)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("child did not acquire")
	}
	c := testCoordinator(t, root, state)
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if _, err := c.Acquire(ctx, intent(path)); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cross-process overlap: %v", err)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	ctx2, cancel2 := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel2()
	l, err := c.Acquire(ctx2, intent(path))
	if err != nil {
		t.Fatalf("crashed holder was not reclaimed: %v", err)
	}
	l.End(NotExecuted)
}
