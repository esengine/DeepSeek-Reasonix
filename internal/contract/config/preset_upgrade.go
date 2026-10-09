// preset_upgrade.go — bringing an entry still shaped the way we shipped it up
// to the preset that replaced that shape.
package config

import (
	"strings"

	"reasonix/internal/contract/provider"
)

// shippedPreset is a shape this project once shipped. An entry matching one is
// ours to bring forward; anything else its user curated, and curation is the
// one thing an upgrade must never overwrite.
type shippedPreset struct {
	PresetID string
	// Models is the list that shape shipped with. CurrentModels instead means
	// the shape shipped the list the preset still has — what moved was another
	// field, and the untouched list is how the entry is recognised all the same.
	Models        []string
	CurrentModels bool
	// Window is the context window that shape shipped with. Nil identifies the
	// shape by its catalog alone; zero is itself a value one of them shipped, so
	// only a pointer tells "not checked" from "checked against nothing set".
	Window *int
	// Default, when set, has to match too: a shape that shipped one and had it
	// changed was changed by somebody.
	Default string
	// Kind, when set, is the wire that shape shipped on, and the upgrade moves
	// the entry onto the preset's. Empty identifies the shape on the preset's own.
	Kind string
	// Vision is the list that shape shipped. Holding it, or nil, is unspoken;
	// anything else is a choice, an empty list most clearly of all — it says
	// nothing here reads images.
	Vision []string
	// ByName admits an entry written before presets carried an id, matched on
	// its name instead. Declared per preset, because admitting it where it was
	// never admitted would start migrating entries nobody has been migrating.
	ByName bool
}

// The GLM catalogs before GLM-5.3: the shapes an install still holds until the
// entries below move them forward.
var (
	legacyGlmAPIModels    = []string{"glm-5.2", "glm-5.1", "glm-5", "glm-5-turbo", "glm-5v-turbo", "glm-4.7", "glm-4.7-flash", "glm-4.7-flashx", "glm-4.6", "glm-4.5", "glm-4.5-air", "glm-4.5-flash"}
	legacyGlmCodingModels = []string{"glm-5.2", "glm-5.1", "glm-5", "glm-4.7"}
)

// shippedPresets is every shape a curated preset has had. Adding one is how a
// preset change reaches the installs that already have the old shape — the
// alternative is a migration function per vendor per change, which is what this
// replaced, and each of those was one more place for the guard to be subtly
// different.
var shippedPresets = []shippedPreset{
	{PresetID: "kimi-cn", Models: legacyKimiAPIModels, Vision: legacyKimiAPIModels, ByName: true},
	{PresetID: "kimi-global", Models: legacyKimiAPIModels, Vision: legacyKimiAPIModels, ByName: true},
	// The GLM catalogs before GLM-5.3, on both hosts. Four presets shipped one
	// of these two lists; the Anthropic-dialect GLM presets never held them.
	{PresetID: "glm-cn", Models: legacyGlmAPIModels, ByName: true},
	{PresetID: "zai-global", Models: legacyGlmAPIModels, ByName: true},
	{PresetID: "glm-coding-plan-cn", Models: legacyGlmCodingModels, ByName: true},
	{PresetID: "zai-coding-plan-global", Models: legacyGlmCodingModels, ByName: true},
	{PresetID: "opencode-go", Models: legacyOpenCodeGoModels, ByName: true},
	{PresetID: "mimo-api", Models: legacyMimoModels, Window: new(legacyMimoWindow), Default: legacyMimoModels[0], Vision: legacyMimoVision},
	{PresetID: "mimo-anthropic", Models: legacyMimoModels, Window: new(legacyMimoWindow), Default: legacyMimoModels[0], Vision: legacyMimoVision},
	{PresetID: "mimo-token-plan-cn", Models: legacyMimoModels, Window: new(legacyMimoWindow), Default: legacyMimoModels[0], Vision: legacyMimoVision},
	{PresetID: "mimo-token-plan-cn-anthropic", Models: legacyMimoModels, Window: new(legacyMimoWindow), Default: legacyMimoModels[0], Vision: legacyMimoVision},
	{PresetID: "mimo-token-plan-sgp", Models: legacyMimoModels, Window: new(legacyMimoWindow), Default: legacyMimoModels[0], Vision: legacyMimoVision},
	{PresetID: "mimo-token-plan-sgp-anthropic", Models: legacyMimoModels, Window: new(legacyMimoWindow), Default: legacyMimoModels[0], Vision: legacyMimoVision},
	{PresetID: "mimo-token-plan-ams", Models: legacyMimoModels, Window: new(legacyMimoWindow), Default: legacyMimoModels[0], Vision: legacyMimoVision},
	{PresetID: "mimo-token-plan-ams-anthropic", Models: legacyMimoModels, Window: new(legacyMimoWindow), Default: legacyMimoModels[0], Vision: legacyMimoVision},
	{PresetID: "longcat-openai", Models: longCat20Models, Window: new(legacyLongCat20ContextWindow), Default: longCat20Models[0]},
	{PresetID: "longcat-anthropic", Models: longCat20Models, Window: new(legacyLongCat20ContextWindow), Default: longCat20Models[0]},
	{PresetID: "qwen-cn", CurrentModels: true, Window: new(0), ByName: true},
	{PresetID: "qwen-global", CurrentModels: true, Window: new(0), ByName: true},
	{PresetID: "qwen-coding-plan-cn", CurrentModels: true, Window: new(0), ByName: true},
	{PresetID: "qwen-coding-plan-cn-anthropic", CurrentModels: true, Window: new(0), ByName: true},
	{PresetID: "qwen-coding-plan-global", CurrentModels: true, Window: new(0), ByName: true},
	{PresetID: "qwen-coding-plan-global-anthropic", CurrentModels: true, Window: new(0), ByName: true},
	{PresetID: "openai", CurrentModels: true, Window: new(openAIWindow), Default: "gpt-6-sol", Kind: kindOpenAI},
}

// upgradeShippedPresets brings every entry still carrying a shape we shipped up
// to the preset it is today. It reports whether it changed anything, so a load
// for edit persists that and a plain load only holds it in memory.
func upgradeShippedPresets(c *Config) bool {
	if c == nil {
		return false
	}
	changed := false
	for i := range c.Providers {
		p := &c.Providers[i]
		for _, shape := range shippedPresets {
			canonical, ok := shape.matches(p)
			if !ok {
				continue
			}
			changed = shape.upgrade(p, canonical) || changed
			break
		}
	}
	return changed
}

// matches reports whether the entry is still the shape this preset shipped, and
// returns the preset it would be brought up to.
func (s shippedPreset) matches(p *ProviderEntry) (ProviderEntry, bool) {
	if p == nil || !s.identifies(p) {
		return ProviderEntry{}, false
	}
	preset, ok := CuratedProviderPreset(s.PresetID)
	if !ok || len(preset.Entries) != 1 {
		return ProviderEntry{}, false
	}
	canonical := preset.Entries[0]
	models := s.Models
	if s.CurrentModels {
		models = canonical.Models
	}
	kind := canonical.Kind
	if s.Kind != "" {
		kind = s.Kind
	}
	// A single model in `model` rather than `models` is a shape nothing here
	// shipped, and replacing its catalog would answer a narrowing with a list.
	if !strings.EqualFold(strings.TrimSpace(p.Kind), strings.TrimSpace(kind)) ||
		normalizedBaseURLForMigration(p.BaseURL) != normalizedBaseURLForMigration(canonical.BaseURL) ||
		!stringSlicesEqual(p.Models, models) ||
		strings.TrimSpace(p.Model) != "" {
		return ProviderEntry{}, false
	}
	if s.Window != nil && p.ContextWindow != *s.Window {
		return ProviderEntry{}, false
	}
	if s.Default != "" && p.Default != s.Default {
		return ProviderEntry{}, false
	}
	return canonical, true
}

func (s shippedPreset) identifies(p *ProviderEntry) bool {
	presetID := strings.TrimSpace(p.PresetID)
	if presetID != "" {
		return presetID == s.PresetID
	}
	return s.ByName && strings.TrimSpace(p.Name) == s.PresetID
}

// upgradeToPreset moves one entry onto the preset. The catalog is replaced —
// the guard established the entry still lists what we put there — while the
// per-model settings are only filled in where they are missing, never over a
// value somebody chose.
func (s shippedPreset) upgrade(p *ProviderEntry, canonical ProviderEntry) bool {
	added := addedModels(p.Models, canonical.Models)
	changed := false
	if s.Kind != "" && !strings.EqualFold(strings.TrimSpace(p.Kind), canonical.Kind) {
		p.Kind = canonical.Kind
		changed = true
	}
	if !stringSlicesEqual(p.Models, canonical.Models) {
		p.Models = append([]string(nil), canonical.Models...)
		changed = true
	}
	// Only a shape that named its window moves one. Where the window was not
	// part of what identified the shape, the number on the entry is the user's:
	// they narrowed a context on purpose and an upgrade may not widen it back.
	if s.Window != nil && canonical.ContextWindow > 0 && p.ContextWindow != canonical.ContextWindow {
		p.ContextWindow = canonical.ContextWindow
		changed = true
	}
	// The guard held the shipped default, so it moves with the shape; any other
	// default is a choice and stays while its model is still listed.
	shippedDefault := s.Default != "" && p.Default == s.Default
	if canonical.Default != "" && p.Default != canonical.Default && (shippedDefault || !p.HasModel(p.Default)) {
		p.Default = canonical.Default
		changed = true
	}
	// Only the models this upgrade adds are marked image-taking, and only where
	// the preset says they are. vision_models narrows; widening it to the
	// preset's whole list would send images to models nobody ticked.
	if vision, ok := s.upgradedVisionModels(p, canonical, added); ok {
		p.VisionModels = vision
		changed = true
	}
	if mergeMissingModelOverrides(p, canonical.ModelOverrides) {
		changed = true
	}
	if mergeMissingPrices(p, canonical.Prices, added) {
		changed = true
	}
	return changed
}

// mergeMissingPrices prices the models an upgrade adds, only where the entry
// holds no price for them. A rate somebody wrote down is theirs to keep.
func mergeMissingPrices(p *ProviderEntry, defaults map[string]*provider.Pricing, added []string) bool {
	changed := false
	for _, model := range added {
		fallback := defaults[model]
		if fallback == nil || p.Prices[model] != nil {
			continue
		}
		if p.Prices == nil {
			p.Prices = make(map[string]*provider.Pricing, len(added))
		}
		p.Prices[model] = clonePricing(fallback)
		changed = true
	}
	return changed
}

// mergeMissingModelOverrides fills in what the preset states and the entry does
// not — field by field, never over a value already set. An override is where a
// user says what a model may do, and an upgrade that overwrote one would answer
// a deliberate narrowing by widening it straight back.
func mergeMissingModelOverrides(p *ProviderEntry, defaults map[string]ProviderModelOverride) bool {
	if p == nil || len(defaults) == 0 {
		return false
	}
	changed := false
	for key, fallback := range defaults {
		// Matched case-insensitively against what is already there: a model id
		// spelled differently would otherwise get a second key, and then the
		// model has two answers and no rule for which one wins.
		target := key
		for existing := range p.ModelOverrides {
			if strings.EqualFold(strings.TrimSpace(existing), key) {
				target = existing
				break
			}
		}
		override, touched := p.ModelOverrides[target], false
		if strings.TrimSpace(override.ReasoningProtocol) == "" && strings.TrimSpace(fallback.ReasoningProtocol) != "" {
			override.ReasoningProtocol, touched = fallback.ReasoningProtocol, true
		}
		if override.SupportedEfforts == nil && len(fallback.SupportedEfforts) > 0 {
			override.SupportedEfforts, touched = append([]string(nil), fallback.SupportedEfforts...), true
		}
		if strings.TrimSpace(override.DefaultEffort) == "" && strings.TrimSpace(fallback.DefaultEffort) != "" &&
			containsString(normalizedEffortLevels(override.SupportedEfforts), fallback.DefaultEffort) {
			override.DefaultEffort, touched = fallback.DefaultEffort, true
		}
		if override.ContextWindow <= 0 && fallback.ContextWindow > 0 {
			override.ContextWindow, touched = fallback.ContextWindow, true
		}
		if override.MaxOutputTokens == 0 && fallback.MaxOutputTokens != 0 {
			override.MaxOutputTokens, touched = fallback.MaxOutputTokens, true
		}
		if override.Vision == nil && fallback.Vision != nil {
			override.Vision, touched = fallback.Vision, true
		}
		if !touched {
			continue
		}
		if p.ModelOverrides == nil {
			p.ModelOverrides = make(map[string]ProviderModelOverride, len(defaults))
		}
		p.ModelOverrides[target] = override
		changed = true
	}
	return changed
}

// upgradedVisionModels marks the added models the preset calls image-taking,
// ordering the result the way the preset states them so a second pass reads the
// same as the first. Anything the user added that the preset does not know
// keeps its place after them. An upgrade that adds nothing leaves the list
// exactly as it was, reordering included.
func (s shippedPreset) upgradedVisionModels(p *ProviderEntry, canonical ProviderEntry, added []string) ([]string, bool) {
	// nil is "never said" and is ours to fill; the list we shipped is ours to
	// move. Anything else they chose — an explicitly empty list most of all,
	// which says no model here reads images and must survive the upgrade.
	if p.VisionModels != nil && !(s.Vision != nil && stringSlicesEqual(p.VisionModels, s.Vision)) {
		return nil, false
	}
	marks := false
	for _, model := range added {
		if containsString(canonical.VisionModels, model) && !p.HasVisionModel(model) {
			marks = true
		}
	}
	if !marks {
		return nil, false
	}
	out := make([]string, 0, len(p.VisionModels)+len(added))
	for _, model := range canonical.VisionModels {
		if p.HasVisionModel(model) || containsString(added, model) {
			out = append(out, model)
		}
	}
	for _, model := range p.VisionModels {
		if !containsString(out, model) {
			out = append(out, model)
		}
	}
	return out, true
}

// addedModels is what the preset lists and the entry did not.
func addedModels(before, after []string) []string {
	var out []string
	for _, model := range after {
		if !containsString(before, model) {
			out = append(out, model)
		}
	}
	return out
}
