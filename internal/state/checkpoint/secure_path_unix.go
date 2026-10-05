//go:build !windows && !plan9

package checkpoint

import (
	"errors"
	"fmt"
	"io"
	"os"

	"golang.org/x/sys/unix"
)

func secureOpenParent(root, abs string, create bool) (int, string, error) {
	rel, err := workspaceRelative(root, abs)
	if err != nil {
		return -1, "", err
	}
	parts := splitLocalPath(rel)
	if len(parts) == 0 {
		return -1, "", fmt.Errorf("workspace root is not a file target")
	}
	// The configured workspace root is the trust boundary and may itself be a
	// user-selected symlink. Every component below it is opened with O_NOFOLLOW.
	fd, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, "", fmt.Errorf("open workspace root: %w", err)
	}
	for _, part := range parts[:len(parts)-1] {
		next, openErr := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if errors.Is(openErr, unix.ENOENT) && create {
			if mkdirErr := unix.Mkdirat(fd, part, 0o755); mkdirErr != nil && !errors.Is(mkdirErr, unix.EEXIST) {
				unix.Close(fd)
				return -1, "", fmt.Errorf("create workspace directory %q: %w", part, mkdirErr)
			}
			next, openErr = unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		}
		if openErr != nil {
			unix.Close(fd)
			return -1, "", fmt.Errorf("open workspace directory %q: %w", part, openErr)
		}
		unix.Close(fd)
		fd = next
	}
	return fd, parts[len(parts)-1], nil
}

func secureOpenWorkspaceFile(root, abs string) (*os.File, error) {
	if root == "" {
		return os.Open(abs)
	}
	parent, base, err := secureOpenParent(root, abs, false)
	if err != nil {
		return nil, err
	}
	defer unix.Close(parent)
	fd, err := unix.Openat(parent, base, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: abs, Err: err}
	}
	return os.NewFile(uintptr(fd), abs), nil
}

func secureWriteNew(root, abs string, data []byte, mode os.FileMode) error {
	if root == "" {
		return writeNewFile(abs, data, mode)
	}
	parent, base, err := secureOpenParent(root, abs, true)
	if err != nil {
		return err
	}
	defer unix.Close(parent)
	fd, err := unix.Openat(parent, base, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, uint32(mode.Perm()))
	if err != nil {
		return &os.PathError{Op: "create", Path: abs, Err: err}
	}
	file := os.NewFile(uintptr(fd), abs)
	remove := true
	defer func() {
		_ = file.Close()
		if remove {
			_ = unix.Unlinkat(parent, base, 0)
		}
	}()
	if _, err := file.Write(data); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	remove = false
	return nil
}

func secureRename(root, oldAbs, newAbs string) error {
	if root == "" {
		return os.Rename(oldAbs, newAbs)
	}
	oldParent, oldBase, err := secureOpenParent(root, oldAbs, false)
	if err != nil {
		return err
	}
	defer unix.Close(oldParent)
	newParent, newBase, err := secureOpenParent(root, newAbs, true)
	if err != nil {
		return err
	}
	defer unix.Close(newParent)
	if err := unix.Renameat(oldParent, oldBase, newParent, newBase); err != nil {
		return &os.LinkError{Op: "rename", Old: oldAbs, New: newAbs, Err: err}
	}
	return nil
}

func secureRemove(root, abs string) error {
	if root == "" {
		return os.Remove(abs)
	}
	parent, base, err := secureOpenParent(root, abs, false)
	if err != nil {
		return err
	}
	defer unix.Close(parent)
	if err := unix.Unlinkat(parent, base, 0); err != nil {
		return &os.PathError{Op: "remove", Path: abs, Err: err}
	}
	return nil
}

func secureChmod(root, abs string, mode os.FileMode) error {
	file, err := secureOpenWorkspaceFile(root, abs)
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Chmod(mode)
}

func securePathExists(root, abs string) (bool, error) {
	file, err := secureOpenWorkspaceFile(root, abs)
	if err == nil {
		_ = file.Close()
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func secureReadFile(root, abs string) ([]byte, error) {
	file, err := secureOpenWorkspaceFile(root, abs)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return io.ReadAll(file)
}
