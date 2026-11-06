package config

import "strings"

// CuratedProviderPresetForAddress returns the curated preset one of whose
// entries serves this address, so a hand-written entry pointing at a declared
// service is read through that declaration rather than through its name.
func CuratedProviderPresetForAddress(baseURL string) (ProviderPreset, bool) {
	want := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if want == "" {
		return ProviderPreset{}, false
	}
	for _, p := range curatedProviderPresets {
		for _, e := range p.Entries {
			if strings.TrimRight(strings.TrimSpace(e.BaseURL), "/") == want {
				return cloneProviderPreset(p), true
			}
		}
	}
	return ProviderPreset{}, false
}
