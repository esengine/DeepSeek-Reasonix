//go:build !windows

package config

import "path/filepath"

func resolveExistingConfigPath(path string) (string, error) {
	return filepath.EvalSymlinks(path)
}

func configPathLockKey(path string) string {
	return path
}
