//go:build linux

package lifecycle

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func openCurrentExecutable() (*os.File, os.FileInfo, error) {
	fd, err := unix.Open("/proc/self/exe", unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("open current process executable inode: %w", err)
	}
	file := os.NewFile(uintptr(fd), "/proc/self/exe")
	if file == nil {
		unix.Close(fd)
		return nil, nil, fmt.Errorf("create current process executable handle: %w", ErrUnsafeArtifact)
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, nil, err
	}
	if !info.Mode().IsRegular() {
		file.Close()
		return nil, nil, fmt.Errorf("current process executable inode is not regular: %w", ErrUnsafeArtifact)
	}
	return file, info, nil
}
