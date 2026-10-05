package config

import "strings"

// CuratedModelDeclared reports whether the built-in preset catalog declares
// model. A declared id ships in the binary, so naming one carries nothing
// private. It reads the full catalog, including presets the new-provider
// surfaces hide.
func CuratedModelDeclared(model string) bool {
	model = strings.TrimSpace(model)
	if model == "" {
		return false
	}
	for i := range curatedProviderPresets {
		for j := range curatedProviderPresets[i].Entries {
			if curatedProviderPresets[i].Entries[j].HasModel(model) {
				return true
			}
		}
	}
	return false
}
