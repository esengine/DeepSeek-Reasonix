package skill

import (
	"fmt"
	"path/filepath"

	"reasonix/internal/base/frontmatter"
	"reasonix/internal/ext/pluginpkg"
)

// PluginWarnings preserves delivery validator identities for frontend projection,
// independently of whether the installed package is enabled.
func PluginWarnings(pkg pluginpkg.Package) []error {
	seen := map[string]bool{}
	var warnings []error
	pkg.WalkProfileSources(func(path string, body []byte) bool {
		if accepted, ok := seen[path]; ok {
			return accepted
		}
		doc, _ := frontmatter.Parse(string(body))
		_, err := deliveryFromDocument(doc)
		seen[path] = err == nil
		if err != nil {
			warnings = append(warnings, fmt.Errorf("%s: %w", filepath.ToSlash(path), err))
		}
		return err == nil
	})
	return warnings
}
