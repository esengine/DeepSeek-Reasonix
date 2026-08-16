package config

import (
	"path/filepath"
	"testing"

	"github.com/BurntSushi/toml"

	"reasonix/internal/provider"
)

func TestNormalizeLegacyOpenCodeGoInstallsAppliesCatalogAndWindowsInOnePass(t *testing.T) {
	legacy := ProviderEntry{
		Name:          "opencode-go",
		Kind:          "openai",
		BaseURL:       "https://opencode.ai/zen/go/v1",
		Models:        append([]string(nil), legacyOpenCodeGoModels...),
		Default:       "glm-5.2",
		ContextWindow: legacyOpenCodeGoChatWindow,
		PresetID:      "opencode-go",
	}
	second := legacy
	second.Name = "opencode-go-secondary"
	c := &Config{Providers: []ProviderEntry{legacy, second}}

	if !normalizeLegacyOpenCodeGoInstalls(c) {
		t.Fatal("combined OpenCode Go migration did not report a change")
	}
	for i := range c.Providers {
		got := c.Providers[i]
		if !got.HasModel("kimi-k3") {
			t.Fatalf("provider %q did not receive the Kimi K3 catalog update", got.Name)
		}
		for model, limits := range provider.OpenCodeGoChatModels() {
			if window := got.ModelOverrides[model].ContextWindow; window != limits.Context {
				t.Fatalf("provider %q model %q window = %d, want %d", got.Name, model, window, limits.Context)
			}
		}
	}
	if normalizeLegacyOpenCodeGoInstalls(c) {
		t.Fatal("combined OpenCode Go migration was not idempotent")
	}
}

func TestNormalizeLegacyOpenCodeGoContextWindowsMigratesOnlyUntouchedPresets(t *testing.T) {
	legacy := ProviderEntry{
		Name:          "opencode-go",
		Kind:          "openai",
		BaseURL:       "https://opencode.ai/zen/go/v1",
		Models:        append([]string(nil), opencodeGoModels...),
		Default:       "glm-5.2",
		ContextWindow: 128000,
		PresetID:      "opencode-go",
		ModelOverrides: map[string]ProviderModelOverride{
			"kimi-k3": {ContextWindow: 1_048_576},
		},
	}
	customEndpoint := legacy
	customEndpoint.Name = "og-proxy"
	customEndpoint.BaseURL = "https://gateway.example/v1"
	customEndpoint.ModelOverrides = cloneModelOverrideMap(legacy.ModelOverrides)
	customWindow := legacy
	customWindow.Name = "og-custom-window"
	customWindow.ContextWindow = 777_000
	customWindow.ModelOverrides = cloneModelOverrideMap(legacy.ModelOverrides)
	customOverride := legacy
	customOverride.Name = "og-custom-override"
	customOverride.ModelOverrides = map[string]ProviderModelOverride{
		"glm-5.2": {ContextWindow: 500_000},
	}
	c := &Config{Providers: []ProviderEntry{legacy, customEndpoint, customWindow, customOverride}}
	if !normalizeLegacyOpenCodeGoContextWindows(c) {
		t.Fatal("expected official legacy preset to migrate")
	}
	if c.Providers[0].ModelOverrides["glm-5.2"].ContextWindow != 1_000_000 {
		t.Fatalf("migrated glm-5.2 = %+v", c.Providers[0].ModelOverrides["glm-5.2"])
	}
	if c.Providers[1].ModelOverrides["glm-5.2"].ContextWindow != 0 {
		t.Fatal("custom endpoint was migrated")
	}
	if c.Providers[2].ContextWindow != 777_000 {
		t.Fatal("custom provider window was overwritten")
	}
	if c.Providers[3].ModelOverrides["glm-5.2"].ContextWindow != 500_000 {
		t.Fatal("positive custom override was overwritten")
	}

	preIdentity := ProviderEntry{
		Name:          "opencode-go",
		Kind:          "openai",
		BaseURL:       "https://opencode.ai/zen/go/v1",
		Models:        append([]string(nil), opencodeGoModels...),
		Default:       "glm-5.2",
		ContextWindow: 128000,
		ModelOverrides: map[string]ProviderModelOverride{
			"kimi-k3": {ContextWindow: 1_048_576},
		},
	}
	pre := &Config{Providers: []ProviderEntry{preIdentity}}
	if !normalizeLegacyOpenCodeGoContextWindows(pre) || pre.Providers[0].ModelOverrides["glm-5.2"].ContextWindow != 1_000_000 {
		t.Fatal("pre-preset-identity OpenCode Go was not migrated")
	}
}

func TestLoadForEditOpenCodeGoWindowsStayInMemoryUntilSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	cfg := Default()
	preset, ok := CuratedProviderPreset("opencode-go")
	if !ok {
		t.Fatal("missing preset")
	}
	entry := preset.Entries[0]
	entry.ContextWindow = 128000
	entry.ModelOverrides = map[string]ProviderModelOverride{"kimi-k3": {ContextWindow: 1_048_576}}
	cfg.Providers = []ProviderEntry{entry}
	if err := cfg.SaveTo(path); err != nil {
		t.Fatal(err)
	}
	loaded := LoadForEdit(path)
	got, _ := loaded.Provider("opencode-go")
	if got.ModelOverrides["glm-5.2"].ContextWindow != 1_000_000 {
		t.Fatalf("in-memory migration missing: %+v", got.ModelOverrides)
	}
	var disk Config
	if _, err := toml.DecodeFile(path, &disk); err != nil {
		t.Fatal(err)
	}
	persisted, _ := disk.Provider("opencode-go")
	if persisted.ModelOverrides["glm-5.2"].ContextWindow != 0 {
		t.Fatalf("read-only load wrote disk: %+v", persisted.ModelOverrides)
	}
}
