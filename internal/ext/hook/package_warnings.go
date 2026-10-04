package hook

import (
	"fmt"
	"maps"
	"slices"

	"reasonix/internal/ext/pluginpkg"
)

// PackageWarnings diagnoses hook declarations without consulting the enabled
// registry or executing commands.
func PackageWarnings(pkg pluginpkg.Package) []string {
	var warnings []string
	for _, eventName := range slices.Sorted(maps.Keys(pkg.Manifest.Hooks)) {
		if !IsKnownEvent(eventName) {
			warnings = append(warnings, fmt.Sprintf("hooks.%s: unknown event; these hooks will not run", eventName))
			continue
		}
		if !UsesToolMatcher(Event(eventName)) {
			continue
		}
		for i, entry := range pkg.Manifest.Hooks[eventName] {
			if warning := ValidateMatcher(entry.Match); warning != "" {
				warnings = append(warnings, fmt.Sprintf("hooks.%s[%d]: %s", eventName, i, warning))
			}
		}
	}
	return warnings
}
