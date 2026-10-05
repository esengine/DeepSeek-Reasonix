package builtin

import (
	"os"
	"path/filepath"
)

// macOS TCC-protected user dirs: an opendir trips a privacy consent prompt
// ("wants to access Apple Music / media library"), so recursive glob/grep prune
// them. Matched by absolute path, not basename, so a project's own dir is safe.
var protectedDirs = func() map[string]bool {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	m := make(map[string]bool, 4)
	for _, d := range []string{"Music", "Pictures", "Movies", "Library"} {
		m[filepath.Clean(filepath.Join(home, d))] = true
	}
	return m
}()

func isProtectedDir(abs string) bool { return protectedDirs[abs] }
