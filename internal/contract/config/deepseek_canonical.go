// deepseek_canonical.go — folding the official DeepSeek source into one entry.
package config

import (
	"strings"

	"reasonix/internal/contract/provider"
)

// canonicalizeOfficialDeepSeekSource folds deepseek-flash and deepseek-pro into
// the one "deepseek" entry carrying both models: one endpoint, one key, one
// account, which a panel listing sources otherwise shows as two. A curated model
// list is left alone — the merged entry carries both models and would re-add the
// one the user removed.
func canonicalizeOfficialDeepSeekSource(c *Config) bool {
	if c == nil || !canCanonicalizeLegacyDeepSeekProviders(c) {
		return false
	}
	legacy := officialLegacyDeepSeekProviders(c)
	if len(legacy) == 0 {
		return false
	}
	drop := make(map[string]bool, len(legacy))
	for _, p := range legacy {
		if len(p.Models) > 0 {
			return false
		}
		drop[p.Name] = true
	}
	ensureDeepSeekOfficialProvider(c)
	if p, ok := c.Provider("deepseek"); !ok || officialProviderKind(p) != "deepseek" {
		return false
	}
	// The canonical entry takes the position of the first entry it replaces.
	// Appending it instead would reorder the list, and provider order decides
	// which entry answers a bare model name.
	var canonical ProviderEntry
	at := -1
	kept := make([]ProviderEntry, 0, len(c.Providers))
	for _, p := range c.Providers {
		if p.Name != "deepseek" && !drop[p.Name] {
			kept = append(kept, p)
			continue
		}
		if p.Name == "deepseek" {
			canonical = p
		}
		if at < 0 {
			at = len(kept)
		}
	}
	kept = append(kept, ProviderEntry{})
	copy(kept[at+1:], kept[at:])
	kept[at] = canonical
	c.Providers = kept
	foldDesktopProviderAccess(c, drop)
	return true
}

// foldLegacyDeepSeekPeers merges the legacy per-model entries into the first of
// them when they cannot reach the canonical entry, which speaks another protocol
// at another address. One endpoint, one key and one account still show up as two
// sources otherwise. The survivor keeps that entry's name and slot: provider
// order answers bare model names, and a new name breaks refs outside this file.
func foldLegacyDeepSeekPeers(c *Config) bool {
	if c == nil || canCanonicalizeLegacyDeepSeekProviders(c) {
		// The canonical path owns this case and drops the legacy entries whole.
		return false
	}
	legacy := officialLegacyDeepSeekProviders(c)
	if len(legacy) < 2 {
		return false
	}
	for _, p := range legacy {
		// A curated model list is left alone: the merge carries every model and
		// would re-add the one the user removed.
		if len(p.Models) > 0 || strings.TrimSpace(p.Model) == "" {
			return false
		}
	}
	for i := 1; i < len(legacy); i++ {
		if !legacyDeepSeekProviderWideFieldsEqual(legacy[0], legacy[i]) ||
			!legacyDeepSeekModelFieldsCompatible(legacy[0], legacy[i]) {
			return false
		}
	}

	merged := mergedLegacyDeepSeekEntry(legacy)
	drop := make(map[string]bool, len(legacy)-1)
	for _, p := range legacy[1:] {
		drop[p.Name] = true
	}
	kept := make([]ProviderEntry, 0, len(c.Providers))
	for _, p := range c.Providers {
		switch {
		case p.Name == merged.Name:
			kept = append(kept, merged)
		case drop[p.Name]:
		default:
			kept = append(kept, p)
		}
	}
	c.Providers = kept
	foldProviderAccessInto(c, drop, merged.Name)
	return true
}

// mergedLegacyDeepSeekEntry builds the one entry the peers become. Every field
// the projection treats as per-model is re-attached under the model it came
// from, so a price or an effort list someone edited survives the merge.
func mergedLegacyDeepSeekEntry(legacy []*ProviderEntry) ProviderEntry {
	merged := cloneProviderEntry(*legacy[0])
	merged.Models = make([]string, 0, len(legacy))
	if merged.Prices == nil {
		merged.Prices = map[string]*provider.Pricing{}
	}
	if merged.ModelOverrides == nil {
		merged.ModelOverrides = map[string]ProviderModelOverride{}
	}
	for _, p := range legacy {
		model := strings.TrimSpace(p.Model)
		merged.Models = append(merged.Models, model)
		if p.Price != nil {
			if _, taken := merged.Prices[model]; !taken {
				merged.Prices[model] = p.Price
			}
		}
		override := merged.ModelOverrides[model]
		if len(p.SupportedEfforts) > 0 && len(override.SupportedEfforts) == 0 {
			override.SupportedEfforts = append([]string(nil), p.SupportedEfforts...)
			override.DefaultEffort = strings.TrimSpace(p.DefaultEffort)
		}
		if p.ReasoningProtocol != "" && override.ReasoningProtocol == "" {
			override.ReasoningProtocol = p.ReasoningProtocol
		}
		if len(override.SupportedEfforts) > 0 || override.ReasoningProtocol != "" {
			merged.ModelOverrides[model] = override
		}
	}
	if strings.TrimSpace(merged.Default) == "" {
		merged.Default = merged.Models[0]
	}
	// These now live per model. Leaving the provider-wide copies would make the
	// first entry's price and effort list answer for every model in the merge.
	merged.Model = ""
	merged.Price = nil
	merged.SupportedEfforts = nil
	merged.DefaultEffort = ""
	if len(merged.Prices) == 0 {
		merged.Prices = nil
	}
	if len(merged.ModelOverrides) == 0 {
		merged.ModelOverrides = nil
	}
	return merged
}

// foldProviderAccessInto points the desktop access list at the surviving name.
func foldProviderAccessInto(c *Config, drop map[string]bool, survivor string) {
	if len(c.Desktop.ProviderAccess) == 0 {
		return
	}
	seen := make(map[string]bool, len(c.Desktop.ProviderAccess))
	out := make([]string, 0, len(c.Desktop.ProviderAccess))
	for _, raw := range c.Desktop.ProviderAccess {
		name := strings.TrimSpace(raw)
		if drop[name] {
			name = survivor
		}
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	c.Desktop.ProviderAccess = out
}

// refForFoldedDeepSeek maps a ref naming a folded entry onto the canonical one.
// Rewriting every ref in the config instead would make Default().DefaultModel
// and a loaded one disagree, and would not reach the refs that live outside the
// file anyway — shell history, scripts, a --model flag someone has memorised.
func (c *Config) refForFoldedDeepSeek(ref string) string {
	provider, _, _ := strings.Cut(ref, "/")
	if provider != "deepseek-flash" && provider != "deepseek-pro" {
		return ref
	}
	if _, stillConfigured := c.Provider(provider); stillConfigured {
		return ref
	}
	// A surviving peer answers first. It is asked before the canonical entry
	// because a "deepseek" entry can exist without having absorbed anything —
	// another protocol at another address — and it does not hold this route.
	model := strings.TrimSpace(strings.TrimPrefix(ref, provider+"/"))
	for _, name := range []string{"deepseek-flash", "deepseek-pro"} {
		peer, ok := c.Provider(name)
		if !ok || len(peer.Models) < 2 {
			continue
		}
		if model == "" {
			return name
		}
		if peer.HasModel(model) {
			return name + "/" + model
		}
	}
	if _, folded := c.Provider("deepseek"); folded {
		return retargetDesktopOfficialRef(ref, map[string]bool{"deepseek": true})
	}
	return ref
}

// foldDesktopProviderAccess points the desktop access list at the surviving
// name. Both folded names collapse to one entry, so it dedupes as it goes.
func foldDesktopProviderAccess(c *Config, drop map[string]bool) {
	if len(c.Desktop.ProviderAccess) == 0 {
		return
	}
	seen := make(map[string]bool, len(c.Desktop.ProviderAccess))
	out := make([]string, 0, len(c.Desktop.ProviderAccess))
	for _, raw := range c.Desktop.ProviderAccess {
		name := strings.TrimSpace(raw)
		if drop[name] {
			name = "deepseek"
		}
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	c.Desktop.ProviderAccess = out
}
