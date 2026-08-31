//go:build darwin

package sessioncatalog

import (
	"os"
	"path/filepath"
)

func existingCatalogPathParent(path string) string {
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
