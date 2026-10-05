//go:build darwin || windows

package repair

import (
	"os"
	"path/filepath"
)

// existingRepairPathParent returns the closest existing directory that governs
// name lookup for path. It also handles create-only targets whose immediate
// parent has not been created yet.
func existingRepairPathParent(path string) string {
	dir := filepath.Dir(path)
	for {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir
		}
		next := filepath.Dir(dir)
		if next == dir {
			return ""
		}
		dir = next
	}
}
