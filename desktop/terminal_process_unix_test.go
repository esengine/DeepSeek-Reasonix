//go:build !windows

package main

import (
	"bytes"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestUnixTerminalProcessPTYSmoke(t *testing.T) {
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Skipf("sh is unavailable: %v", err)
	}
	proc, err := startTerminalProcess(terminalStartSpec{
		command: commandForShellPath(shell, "sh"),
		dir:     t.TempDir(),
		env:     terminalEnvironment([]string{"PATH=" + os.Getenv("PATH"), "HOME=" + t.TempDir()}),
		cols:    defaultTerminalColumns,
		rows:    defaultTerminalRows,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	if err := proc.Resize(100, 30); err != nil {
		t.Fatalf("resize PTY: %v", err)
	}

	const marker = "reasonix-pty-smoke"
	readResult := make(chan error, 1)
	markerSeen := make(chan struct{})
	go func() {
		var output bytes.Buffer
		buf := make([]byte, 4096)
		seen := false
		for {
			n, readErr := proc.Read(buf)
			if n > 0 {
				output.Write(buf[:n])
				if !seen && bytes.Contains(output.Bytes(), []byte(marker)) {
					seen = true
					close(markerSeen)
				}
			}
			if readErr != nil {
				if !seen {
					readResult <- readErr
				} else {
					readResult <- nil
				}
				return
			}
		}
	}()

	if _, err := proc.Write([]byte("printf '" + marker + "\\n'\n")); err != nil {
		t.Fatalf("write PTY command: %v", err)
	}
	select {
	case <-markerSeen:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for PTY output")
	}

	if _, err := proc.Write([]byte("exit\n")); err != nil {
		t.Fatalf("write PTY exit: %v", err)
	}
	waitResult := make(chan error, 1)
	go func() {
		_, waitErr := proc.Wait()
		waitResult <- waitErr
	}()
	select {
	case err := <-waitResult:
		if err != nil {
			t.Fatalf("wait for PTY exit: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for PTY process exit")
	}
	select {
	case err := <-readResult:
		if err != nil {
			t.Fatalf("read PTY output: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for PTY reader to finish")
	}
}
