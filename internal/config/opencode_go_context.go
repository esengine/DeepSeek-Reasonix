package config

import (
	"strings"

	"reasonix/internal/provider"
)

const (
	legacyOpenCodeGoChatWindow      = 128000
	legacyOpenCodeGoAnthropicWindow = 262144
)

func normalizeLegacyOpenCodeGoInstalls(c *Config) bool {
	changed := normalizeLegacyOpenCodeGoKimiK3Catalog(c)
	changed = normalizeLegacyOpenCodeGoContextWindows(c) || changed
	return changed
}

func officialOpenCodeGoKindURL(kind, baseURL string) bool {
	_, ok := provider.OfficialOpenCodeGoRoute(kind, baseURL)
	return ok
}

func openCodeGoPresetIDForMigration(p ProviderEntry) string {
	presetID := strings.TrimSpace(p.PresetID)
	if presetID == "" {
		presetID = strings.TrimSpace(p.Name)
	}
	switch presetID {
	case "opencode-go", "opencode-go-anthropic", "opencode-go-deepseek-anthropic", "opencode-go-deepseek-responses":
		return presetID
	default:
		return ""
	}
}

func mergeMissingOpenCodeGoContextOverrides(p *ProviderEntry, defaults map[string]ProviderModelOverride) bool {
	if p == nil || len(defaults) == 0 {
		return false
	}
	if p.ModelOverrides == nil {
		p.ModelOverrides = map[string]ProviderModelOverride{}
	}
	changed := false
	for defaultKey, defaultOverride := range defaults {
		overrideKey := defaultKey
		for key := range p.ModelOverrides {
			if strings.EqualFold(strings.TrimSpace(key), defaultKey) {
				overrideKey = key
				break
			}
		}
		override := p.ModelOverrides[overrideKey]
		if override.ContextWindow == 0 && defaultOverride.ContextWindow > 0 {
			override.ContextWindow = defaultOverride.ContextWindow
			p.ModelOverrides[overrideKey] = override
			changed = true
		}
		if override.MaxOutputTokens == 0 && defaultOverride.MaxOutputTokens != 0 {
			override.MaxOutputTokens = defaultOverride.MaxOutputTokens
			p.ModelOverrides[overrideKey] = override
			changed = true
		}
	}
	return changed
}

func openCodeGoCatalogMatches(presetID string, p, canonical ProviderEntry) bool {
	if strings.TrimSpace(p.Model) != "" {
		return false
	}
	if stringSlicesEqual(p.Models, canonical.Models) {
		return true
	}
	return presetID == "opencode-go" && (stringSlicesEqual(p.Models, opencodeGoModels) || stringSlicesEqual(p.Models, legacyOpenCodeGoModels))
}

func migrateOpenCodeGoProviderWindow(p *ProviderEntry, canonical ProviderEntry, legacy int) bool {
	if p.ContextWindow != 0 && p.ContextWindow != legacy {
		return false
	}
	if p.ContextWindow == canonical.ContextWindow {
		return mergeMissingOpenCodeGoContextOverrides(p, canonical.ModelOverrides)
	}
	p.ContextWindow = canonical.ContextWindow
	_ = mergeMissingOpenCodeGoContextOverrides(p, canonical.ModelOverrides)
	return true
}

// normalizeLegacyOpenCodeGoContextWindows upgrades only unmodified official
// OpenCode Go presets that still carry the old conservative provider window.
// Custom catalogs, endpoints, and positive context/output overrides stay put.
func normalizeLegacyOpenCodeGoContextWindows(c *Config) bool {
	if c == nil {
		return false
	}
	changed := false
	for i := range c.Providers {
		p := &c.Providers[i]
		presetID := openCodeGoPresetIDForMigration(*p)
		preset, ok := CuratedProviderPreset(presetID)
		if presetID == "" || !ok || len(preset.Entries) != 1 {
			continue
		}
		canonical := preset.Entries[0]
		if !strings.EqualFold(strings.TrimSpace(p.Kind), strings.TrimSpace(canonical.Kind)) ||
			!officialOpenCodeGoKindURL(canonical.Kind, p.BaseURL) ||
			normalizedBaseURLForMigration(p.BaseURL) != normalizedBaseURLForMigration(canonical.BaseURL) ||
			!openCodeGoCatalogMatches(presetID, *p, canonical) {
			continue
		}
		legacy := legacyOpenCodeGoChatWindow
		if presetID == "opencode-go-anthropic" || presetID == "opencode-go-deepseek-anthropic" {
			legacy = legacyOpenCodeGoAnthropicWindow
		}
		if migrateOpenCodeGoProviderWindow(p, canonical, legacy) {
			changed = true
		}
	}
	return changed
}
