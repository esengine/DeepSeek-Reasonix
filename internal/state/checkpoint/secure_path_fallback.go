//go:build windows || plan9

package checkpoint

import (
	"os"
)

func secureOpenWorkspaceFile(root, abs string) (*os.File, error) {
	if err := validateWorkspacePath(root, abs); err != nil {
		return nil, err
	}
	return os.Open(abs)
}

func secureWriteNew(root, abs string, data []byte, mode os.FileMode) error {
	if err := validateWorkspacePath(root, abs); err != nil {
		return err
	}
	return writeNewFile(abs, data, mode)
}

func secureRename(root, oldAbs, newAbs string) error {
	if err := validateWorkspacePath(root, oldAbs); err != nil {
		return err
	}
	if err := validateWorkspacePath(root, newAbs); err != nil {
		return err
	}
	return os.Rename(oldAbs, newAbs)
}

func secureRemove(root, abs string) error {
	if err := validateWorkspacePath(root, abs); err != nil {
		return err
	}
	return os.Remove(abs)
}

func secureChmod(root, abs string, mode os.FileMode) error {
	if err := validateWorkspacePath(root, abs); err != nil {
		return err
	}
	return os.Chmod(abs, mode)
}

func securePathExists(root, abs string) (bool, error) {
	if err := validateWorkspacePath(root, abs); err != nil {
		return false, err
	}
	_, err := os.Lstat(abs)
	if os.IsNotExist(err) {
		return false, nil
	}
	return err == nil, err
}

func secureReadFile(root, abs string) ([]byte, error) {
	if err := validateWorkspacePath(root, abs); err != nil {
		return nil, err
	}
	return os.ReadFile(abs)
}
