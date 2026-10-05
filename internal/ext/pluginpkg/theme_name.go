package pluginpkg

import (
	"path/filepath"
	"strings"
)

func themeFileName(path string) string {
	base := filepath.Base(path)
	if base == "theme.json" {
		return filepath.Base(filepath.Dir(path))
	}
	return strings.TrimSuffix(base, filepath.Ext(base))
}
