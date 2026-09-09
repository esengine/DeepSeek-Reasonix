//go:build !windows && !plan9

package main

import (
	"errors"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestReadPinnedWorkspaceFileRejectsFIFOWithoutBlocking(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "context.fifo")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, _, _, err := readPinnedWorkspaceFile(root, "context.fifo")
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, errPinnedNotRegular) {
			t.Fatalf("readPinnedWorkspaceFile() error = %v, want errPinnedNotRegular", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("readPinnedWorkspaceFile blocked while opening a FIFO")
	}
}
