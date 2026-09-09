//go:build !windows && !plan9

package fileutil

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestOpenFileBeneathDoesNotBlockOnFIFO(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "context.fifo")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		file, err := OpenFileBeneath(root, "context.fifo")
		if file != nil {
			_ = file.Close()
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("OpenFileBeneath() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		writer, _ := os.OpenFile(path, os.O_WRONLY|syscall.O_NONBLOCK, 0)
		if writer != nil {
			_ = writer.Close()
		}
		t.Fatal("OpenFileBeneath blocked while opening a FIFO")
	}
}
