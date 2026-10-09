//go:build !windows

package fileutil

import "path/filepath"

// ResolveExistingPath follows filesystem aliases of an existing path.
func ResolveExistingPath(path string) (string, error) { return filepath.EvalSymlinks(path) }
