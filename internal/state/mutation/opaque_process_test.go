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

func TestOpaqueProcessHelper(t *testing.T) {
	if os.Getenv("REASONIX_OPAQUE_PROCESS") != "1" {
		return
	}
	c, err := New(os.Getenv("REASONIX_OPAQUE_ROOT"), os.Getenv("REASONIX_OPAQUE_STATE"), nil)
	if err != nil {
		t.Fatal(err)
	}
	request := opaqueIntent()
	if os.Getenv("REASONIX_OPAQUE_NAMED") == "1" {
		request = intent("file")
	}
	lease, err := c.Acquire(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.End(NotExecuted)
	fmt.Println("opaque-ready")
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
}

func TestOpaqueWriteCrossProcessAndDeath(t *testing.T) {
	for _, namedChild := range []string{"0", "1"} {
		t.Run("named_child_"+namedChild, func(t *testing.T) {
			root, extra, state := t.TempDir(), t.TempDir(), t.TempDir()
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestOpaqueProcessHelper$")
			cmd.Env = append(os.Environ(), "REASONIX_OPAQUE_PROCESS=1", "REASONIX_OPAQUE_ROOT="+root,
				"REASONIX_OPAQUE_STATE="+state, "REASONIX_OPAQUE_NAMED="+namedChild)
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
				if line != "opaque-ready\n" {
					t.Fatal(line)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			c := testCoordinator(t, extra, state)
			request := intent(filepath.Join(extra, "unrelated"))
			if namedChild == "1" {
				request = opaqueIntent()
			}
			if lease, err := c.TryAcquire(ctx, request); !errors.Is(err, ErrBusy) {
				if lease != nil {
					lease.End(NotExecuted)
				}
				t.Fatalf("cross-process global exclusion absent: %v", err)
			}
			if err := cmd.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			_ = cmd.Wait()
			lease, err := c.Acquire(ctx, request)
			if err != nil {
				t.Fatalf("dead owner not reclaimed: %v", err)
			}
			lease.End(NotExecuted)
		})
	}
}
