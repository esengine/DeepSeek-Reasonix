//go:build !darwin

package sandbox

import (
	"os"
	"strings"
)

// callerWriteDirs is what every backend makes writable: the caller's roots, a
// session temp directory, and host temp unless the spec asks for minimal writes.
func callerWriteDirs(spec Spec) []string {
	dirs := append([]string{}, spec.WriteRoots...)
	if dir := strings.TrimSpace(spec.SessionTemp); dir != "" {
		dirs = append(dirs, dir)
	}
	if !spec.MinimalWrites {
		if td := os.TempDir(); td != "" {
			dirs = append(dirs, td)
		}
	}
	return dirs
}
