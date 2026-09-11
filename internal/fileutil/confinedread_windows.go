//go:build windows

package fileutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

const maxConfinedFinalPathUTF16 = 1 << 16

// OpenFileBeneath validates the final path represented by the same handle that
// is returned for reading. Directory junction or symlink swaps after CreateFile
// therefore cannot redirect the subsequent read.
func OpenFileBeneath(root, rel string) (*os.File, error) {
	root = filepath.Clean(strings.TrimSpace(root))
	if root == "" || root == "." {
		return nil, fmt.Errorf("workspace root is empty")
	}
	rel, err := confinedRelativePath(rel)
	if err != nil {
		return nil, err
	}
	rootFile, err := os.Open(root)
	if err != nil {
		return nil, fmt.Errorf("open workspace root: %w", err)
	}
	defer rootFile.Close()
	rootFinal, err := confinedWindowsFinalPath(windows.Handle(rootFile.Fd()))
	if err != nil {
		return nil, fmt.Errorf("resolve workspace root: %w", err)
	}

	file, err := os.Open(filepath.Join(root, rel))
	if err != nil {
		return nil, err
	}
	targetFinal, err := confinedWindowsFinalPath(windows.Handle(file.Fd()))
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("resolve workspace file: %w", err)
	}
	relFinal, err := filepath.Rel(rootFinal, targetFinal)
	if err != nil || relFinal == ".." || strings.HasPrefix(relFinal, ".."+string(filepath.Separator)) {
		file.Close()
		return nil, fmt.Errorf("file resolves outside workspace root")
	}
	return file, nil
}

func confinedWindowsFinalPath(handle windows.Handle) (string, error) {
	size := uint32(256)
	for {
		buf := make([]uint16, size)
		n, err := windows.GetFinalPathNameByHandle(handle, &buf[0], size, 0)
		if err != nil {
			return "", err
		}
		if n < size {
			path := windows.UTF16ToString(buf[:n])
			path = strings.TrimPrefix(path, `\\?\`)
			if strings.HasPrefix(strings.ToUpper(path), `UNC\`) {
				path = `\\` + path[len(`UNC\`):]
			}
			return filepath.Clean(path), nil
		}
		if n >= maxConfinedFinalPathUTF16 {
			return "", fmt.Errorf("resolved path is too large")
		}
		size = n + 1
	}
}
