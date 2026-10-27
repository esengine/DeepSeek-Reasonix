package config

import (
	"regexp"
	"testing"
)

var vendorSlug = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// A client draws a mark from the vendor a preset declares. A preset with none
// would be guessed at from its label or id, which is the judgement this field
// exists to replace.
func TestEveryCuratedPresetDeclaresItsVendor(t *testing.T) {
	for _, preset := range curatedProviderPresets {
		if !vendorSlug.MatchString(preset.Vendor) {
			t.Errorf("preset %q declares vendor %q, want a lowercase slug", preset.ID, preset.Vendor)
		}
	}
}

// Services that share a vendor share a mark, so the gallery can group them
// without reading their names.
func TestPresetsOfOneVendorShareTheSlug(t *testing.T) {
	want := map[string]string{
		"glm-cn": "zhipu", "zai-global": "zhipu", "glm-coding-plan-cn": "zhipu",
		"kimi-cn": "kimi", "kimi-global": "kimi",
		"mimo-api": "mimo", "mimo-token-plan-cn": "mimo",
		"opencode-go": "opencode", "opencode-go-deepseek-responses": "opencode",
		"deepseek-responses": "deepseek", "qwen-cn": "qwen",
	}
	for id, vendor := range want {
		preset, ok := CuratedProviderPreset(id)
		if !ok || preset.Vendor != vendor {
			t.Errorf("preset %q vendor = %q (found %v), want %q", id, preset.Vendor, ok, vendor)
		}
	}
}

func TestRelaysAreDeclaredNotInferred(t *testing.T) {
	for id, want := range map[string]bool{"openrouter": true, "token-rhythm": true, "opencode-go": true, "kimi-cn": false, "deepseek-responses": false} {
		preset, ok := CuratedProviderPreset(id)
		if !ok || preset.Relay != want {
			t.Errorf("preset %q relay = %v (found %v), want %v", id, preset.Relay, ok, want)
		}
	}
}
