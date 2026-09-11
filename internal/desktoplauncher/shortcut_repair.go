package desktoplauncher

import (
	"fmt"
	"path/filepath"
	"strings"
)

func repairInstallerShortcuts(paths []string, resolveRoot func() (string, error), repair func(string, []string) error) error {
	if len(paths) == 0 {
		return fmt.Errorf("--repair-shortcuts requires at least one absolute .lnk path")
	}
	for _, path := range paths {
		if !filepath.IsAbs(path) || !strings.EqualFold(filepath.Ext(path), ".lnk") {
			return fmt.Errorf("--repair-shortcuts requires absolute .lnk paths: %q", path)
		}
	}
	root, err := resolveRoot()
	if err != nil {
		return err
	}
	return repair(root, paths)
}
