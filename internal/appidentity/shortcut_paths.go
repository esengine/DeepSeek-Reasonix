package appidentity

import (
	"fmt"
	"path/filepath"
	"strings"
)

func validateShortcutPaths(root string, paths []string) error {
	if !filepath.IsAbs(root) {
		return fmt.Errorf("shortcut repair requires an absolute install root")
	}
	for _, path := range paths {
		if !filepath.IsAbs(path) || !strings.EqualFold(filepath.Ext(path), ".lnk") {
			return fmt.Errorf("shortcut repair requires absolute .lnk paths: %q", path)
		}
	}
	return nil
}
