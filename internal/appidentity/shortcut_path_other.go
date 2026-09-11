//go:build !windows

package appidentity

import "path/filepath"

func existingShortcutPath(path string) (string, error) { return filepath.EvalSymlinks(path) }
