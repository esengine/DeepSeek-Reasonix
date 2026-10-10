package theme

import (
	"fmt"
	"os"
	"path/filepath"

	"reasonix/internal/ext/pluginpkg"
)

// PluginWarnings validates declared theme manifests without consulting the
// enabled plugin registry or loading image assets.
func PluginWarnings(pkg pluginpkg.Package) []string {
	var warnings []string
	for _, ref := range pkg.ThemeFiles() {
		if filepath.Base(ref.Path) != manifestName {
			continue
		}
		rel, err := filepath.Rel(pkg.Root, ref.Path)
		if err != nil {
			warnings = append(warnings, err.Error())
			continue
		}
		path := filepath.ToSlash(rel)
		raw, err := os.ReadFile(ref.Path)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: %v", path, err))
			continue
		}
		id := pluginPrefix + pkg.Manifest.Name + ":" + filepath.Base(filepath.Dir(ref.Path))
		pack, _, err := decodeManifest(raw, id)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: %v", path, err))
			continue
		}
		for _, warning := range pack.Warnings {
			warnings = append(warnings, fmt.Sprintf("%s: %s", path, warning))
		}
	}
	return warnings
}
