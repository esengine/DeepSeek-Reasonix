package config

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"reasonix/internal/base/testenv"
)

func TestGlobalProviderPresetsAreOffered(t *testing.T) {
	offered := map[string]ProviderPreset{}
	for _, preset := range CuratedProviderPresets() {
		offered[preset.ID] = preset
	}
	for _, id := range []string{"openrouter", "openai", "gemini", "volcengine-coding-plan"} {
		preset, ok := offered[id]
		if !ok {
			t.Fatalf("preset %q is not offered", id)
		}
		if preset.KeyEnv == "" || len(preset.Entries) != 1 || preset.Entries[0].APIKeyEnv != preset.KeyEnv {
			t.Fatalf("preset %q key wiring = %q / %+v", id, preset.KeyEnv, preset.Entries)
		}
	}
}

func TestOpenRouterPresetAttributesTrafficToReasonix(t *testing.T) {
	preset, ok := CuratedProviderPreset("openrouter")
	if !ok {
		t.Fatal("openrouter preset missing")
	}
	var cfg Config
	if err := cfg.UpsertProvider(preset.Entries[0]); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	entry, ok := cfg.Provider("openrouter")
	if !ok {
		t.Fatal("openrouter provider missing after upsert")
	}
	if entry.Headers["HTTP-Referer"] != "https://reasonix.io" || entry.Headers["X-OpenRouter-Title"] != "Reasonix" {
		t.Fatalf("attribution headers = %v", entry.Headers)
	}
	if entry.DefaultModel() != "deepseek/deepseek-v4-flash" {
		t.Fatalf("default model = %q", entry.DefaultModel())
	}
	if entry.HasVisionModel("deepseek/deepseek-v4-pro") || !entry.HasVisionModel("openai/gpt-6-sol") {
		t.Fatalf("vision declaration = %v", entry.VisionModels)
	}
}

func TestOpenAIPresetDeclaresTheGPT6EffortLadder(t *testing.T) {
	preset, ok := CuratedProviderPreset("openai")
	if !ok {
		t.Fatal("openai preset missing")
	}
	var cfg Config
	if err := cfg.UpsertProvider(preset.Entries[0]); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	resolved, ok := cfg.ResolveModel("openai/gpt-6-sol")
	if !ok {
		t.Fatal("openai/gpt-6-sol did not resolve")
	}
	capability := EffortCapabilityForEntry(resolved)
	if !stringSlicesEqual(capability.Levels, []string{"auto", "low", "medium", "high", "xhigh", "max"}) || capability.Default != "medium" {
		t.Fatalf("effort capability = %+v", capability)
	}
	if !EffectiveVision(resolved) {
		t.Fatal("gpt-6-sol should read images")
	}
}

// A curated entry serving a model whose table row names its wires serves it on
// one of them, with efforts inside that model's ladder. A row without Kinds
// states a ladder for Chat Completions only, not where the model is usable.
func TestCuratedPresetsAgreeWithTheModelCapabilityTable(t *testing.T) {
	for _, preset := range CuratedProviderPresets() {
		for _, entry := range preset.Entries {
			for _, model := range entry.Models {
				capability, known := modelReasoningCapabilities[strings.ToLower(model)]
				if !known {
					continue
				}
				if capability.Kinds != nil && !containsString(capability.Kinds, entry.Kind) {
					t.Errorf("preset %q serves %s on kind %q; the model table declares it only on %v", preset.ID, model, entry.Kind, capability.Kinds)
					continue
				}
				resolved := entry
				resolved.Model = model
				ladder, ok := resolvedModelEffortLadder(&resolved)
				if !ok {
					continue
				}
				for _, level := range normalizedSupportedEfforts(&resolved) {
					if !containsString(ladder.Levels, level) {
						t.Errorf("preset %q declares %q for %s, outside its ladder %v", preset.ID, level, model, ladder.Levels)
					}
				}
				if def := normalizeEffortLevel(entry.DefaultEffort); def != "" && !containsString(ladder.Levels, def) {
					t.Errorf("preset %q defaults %s to %q, outside its ladder %v", preset.ID, model, def, ladder.Levels)
				}
			}
		}
	}
}

// shippedOpenAIChatEntry is the OpenAI entry as the first shape installed it.
func shippedOpenAIChatEntry() ProviderEntry {
	preset, _ := CuratedProviderPreset("openai")
	e := cloneProviderEntry(preset.Entries[0])
	e.Kind = kindOpenAI
	return e
}

func TestShippedPresetUpgradeMovesTheChatCompletionsOpenAIPresetToResponses(t *testing.T) {
	c := &Config{Providers: []ProviderEntry{shippedOpenAIChatEntry()}}
	c.Providers[0].Effort = "high"
	if !upgradeShippedPresets(c) {
		t.Fatal("the shipped Chat Completions shape was not upgraded")
	}
	got := c.Providers[0]
	if got.Kind != kindResponses {
		t.Fatalf("kind = %q, want %q", got.Kind, kindResponses)
	}
	if got.Effort != "high" || got.Default != "gpt-6-sol" || got.ContextWindow != openAIWindow ||
		!stringSlicesEqual(got.Models, openAIModels) || !stringSlicesEqual(got.SupportedEfforts, []string{"low", "medium", "high", "xhigh", "max"}) {
		t.Fatalf("upgrade moved more than the wire: %+v", got)
	}
	if upgradeShippedPresets(c) {
		t.Fatalf("a second pass changed the upgraded entry again: %+v", c.Providers[0])
	}
}

func TestShippedPresetUpgradeLeavesCuratedOpenAIEntriesAlone(t *testing.T) {
	for name, edit := range map[string]func(*ProviderEntry){
		"default changed":   func(e *ProviderEntry) { e.Default = "gpt-6-luna" },
		"window narrowed":   func(e *ProviderEntry) { e.ContextWindow = 400_000 },
		"catalog trimmed":   func(e *ProviderEntry) { e.Models = []string{"gpt-6-sol"} },
		"single model":      func(e *ProviderEntry) { e.Model = "gpt-6-sol" },
		"other endpoint":    func(e *ProviderEntry) { e.BaseURL = "https://relay.example.com/v1" },
		"no preset id":      func(e *ProviderEntry) { e.PresetID = "" },
		"anthropic dialect": func(e *ProviderEntry) { e.Kind = "anthropic" },
	} {
		t.Run(name, func(t *testing.T) {
			e := shippedOpenAIChatEntry()
			edit(&e)
			before := cloneProviderEntry(e)
			c := &Config{Providers: []ProviderEntry{e}}
			if upgradeShippedPresets(c) {
				t.Fatalf("a curated entry was upgraded: %+v", c.Providers[0])
			}
			if c.Providers[0].Kind != before.Kind {
				t.Fatalf("kind moved from %q to %q", before.Kind, c.Providers[0].Kind)
			}
		})
	}
}

func TestLoadForEditPersistsTheOpenAIPresetMoveToResponses(t *testing.T) {
	path := filepath.Join(testenv.TempDir(t), "config.toml")
	cfg := Default()
	cfg.Providers = append(cfg.Providers, shippedOpenAIChatEntry())
	if err := cfg.SaveTo(path); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}
	loaded := LoadForEditWithoutCredentials(path)
	if got, ok := loaded.Provider("openai"); !ok || got.Kind != kindResponses {
		t.Fatalf("loaded openai = %+v, want kind %q", got, kindResponses)
	}
	if err := editConfigFile(path, false, func(*Config) error { return nil }); err != nil {
		t.Fatalf("editConfigFile: %v", err)
	}
	var disk Config
	if _, err := toml.DecodeFile(path, &disk); err != nil {
		t.Fatalf("decode config after edit: %v", err)
	}
	if persisted, ok := disk.Provider("openai"); !ok || persisted.Kind != kindResponses {
		t.Fatalf("persisted openai = %+v, want kind %q", persisted, kindResponses)
	}
}
