//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

func openWorkspaceTallyFile(root *os.Root, path string) (*os.File, error) {
	parent, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	parts := strings.Split(filepath.Clean(path), string(filepath.Separator))
	for i, part := range parts {
		flags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK
		if i < len(parts)-1 {
			flags |= unix.O_DIRECTORY
		}
		fd, openErr := unix.Openat(int(parent.Fd()), part, flags, 0)
		_ = parent.Close()
		if openErr != nil {
			return nil, openErr
		}
		parent = os.NewFile(uintptr(fd), path)
	}
	return parent, nil
}
