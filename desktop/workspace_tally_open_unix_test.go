//go:build !windows

package main

import (
	"context"
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

func TestWorkspaceTallyRejectsDirectoriesAndSpecialFiles(t *testing.T) {
	base := t.TempDir()
	root, err := os.OpenRoot(base)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := root.Mkdir("dir", 0700); err != nil {
		t.Fatal(err)
	}
	if err := root.WriteFile("dir/file", []byte("line\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := root.Symlink("dir", "linked-dir"); err != nil {
		t.Fatal(err)
	}
	if f, err := openWorkspaceTallyFile(root, "linked-dir/file"); err == nil {
		f.Close()
		t.Fatal("platform open followed directory symlink")
	}
	if err := unix.Mkfifo(base+"/fifo", 0600); err != nil {
		t.Fatal(err)
	}
	remaining := int64(100)
	if count, partial := workspaceCountFileLines(context.Background(), root, "fifo", &remaining); count != 0 || !partial || remaining != 100 {
		t.Fatal("read from a FIFO")
	}
}
