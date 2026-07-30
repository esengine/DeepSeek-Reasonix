//go:build windows

package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestWindowsTerminalProcessConPTYSmoke(t *testing.T) {
	available, reason := terminalPlatformAvailable()
	if !available {
		t.Skip(reason)
	}
	commandPath, err := exec.LookPath("cmd.exe")
	if err != nil {
		t.Fatal(err)
	}
	proc, err := startTerminalProcess(terminalStartSpec{
		command: commandForShellPath(commandPath, "Command Prompt"),
		dir:     t.TempDir(),
		env:     os.Environ(),
		cols:    defaultTerminalColumns,
		rows:    defaultTerminalRows,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	if err := proc.Resize(100, 30); err != nil {
		t.Fatalf("resize ConPTY: %v", err)
	}

	const marker = "reasonix-conpty-smoke"
	readResult := make(chan error, 1)
	go func() {
		var output bytes.Buffer
		buf := make([]byte, 4096)
		for {
			n, readErr := proc.Read(buf)
			if n > 0 {
				output.Write(buf[:n])
				if bytes.Contains(output.Bytes(), []byte(marker)) {
					readResult <- nil
					return
				}
			}
			if readErr != nil {
				readResult <- fmt.Errorf("read ConPTY output: %w", readErr)
				return
			}
		}
	}()

	if _, err := proc.Write([]byte("echo " + marker + "\r\n")); err != nil {
		t.Fatalf("write ConPTY command: %v", err)
	}
	select {
	case err := <-readResult:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for ConPTY output")
	}

	if _, err := proc.Write([]byte("exit\r\n")); err != nil {
		t.Fatalf("write ConPTY exit: %v", err)
	}
	waitResult := make(chan error, 1)
	go func() {
		_, waitErr := proc.Wait()
		waitResult <- waitErr
	}()
	select {
	case err := <-waitResult:
		if err != nil {
			t.Fatalf("wait for ConPTY exit: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for ConPTY process exit")
	}
}
