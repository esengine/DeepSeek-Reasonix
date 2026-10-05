//go:build !windows

package builtin

import "path/filepath"

const pathSeparators = "/"

// resolveExisting follows every symlink of an existing path.
func resolveExisting(p string) (string, error) { return filepath.EvalSymlinks(p) }
