package config

import (
	"fmt"
	"slices"
	"strings"
)

// mergeDisabledToolPolicies preserves the user's disables when project
// configuration replaces a same-named server. A repository may add restrictions
// but may not quietly turn a tool the user disabled back on.
func mergeDisabledToolPolicies(base, override []string) []string {
	merged := slices.Clone(base)
	for _, name := range override {
		if !slices.Contains(merged, name) {
			merged = append(merged, name)
		}
	}
	return merged
}

func validateDisabledTools(e PluginEntry) error {
	for _, name := range e.DisabledTools {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("plugin %q: disabled_tools contains an empty tool name", e.Name)
		}
	}
	return nil
}
