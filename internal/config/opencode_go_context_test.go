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

func TestNormalizeLegacyOpenCodeGoRouteCatalogSplitsAggregateModelsAndRetargetsHistory(t *testing.T) {
	legacy := ProviderEntry{
		Name:          "opencode-go",
		Kind:          "openai",
		BaseURL:       "https://opencode.ai/zen/go/v1",
		Models:        []string{"glm-5.2", "qwen3.7-plus", "grok-4.5", "private-model"},
		Default:       "grok-4.5",
		APIKeyEnv:     "OPENCODE_GO_API_KEY",
		PresetID:      "opencode-go",
		ContextWindow: legacyOpenCodeGoChatWindow,
		Headers:       map[string]string{"X-User": "keep"},
	}
	c := &Config{
		DefaultModel: "opencode-go/grok-4.5",
		Agent: AgentConfig{
			PlannerModel:   "grok-4.5",
			VisionModel:    "opencode-go/grok-4.5",
			SubagentModel:  "opencode-go/qwen3.7-plus",
			SubagentModels: map[string]string{"review": "grok-4.5", "local": "private-model"},
		},
		Bot:       BotConfig{Model: "opencode-go/grok-4.5"},
		Desktop:   DesktopConfig{ProviderAccess: []string{"opencode-go"}},
		Providers: []ProviderEntry{legacy},
	}
	if !normalizeLegacyOpenCodeGoInstalls(c) {
		t.Fatal("aggregate OpenCode Go route migration did not report a change")
	}
	chat, ok := c.Provider("opencode-go")
	if !ok || !chat.HasModel("glm-5.2") || !chat.HasModel("private-model") || chat.HasModel("grok-4.5") {
		t.Fatalf("chat provider after split = %+v", chat)
	}
	if chat.BillingMode != "subscription_equivalent" || chat.Headers["X-User"] != "keep" {
		t.Fatalf("chat provider lost preserved metadata = %+v", chat)
	}
	responses, ok := c.Provider("opencode-go-responses")
	if !ok || responses.Kind != "responses" || responses.DefaultModel() != "grok-4.5" || !responses.HasModel("grok-4.5") || responses.HasModel("qwen3.7-plus") {
		t.Fatalf("responses provider after split = %+v", responses)
	}
	if c.DefaultModel != "opencode-go-responses/grok-4.5" || c.Agent.PlannerModel != "opencode-go-responses/grok-4.5" || c.Agent.VisionModel != "opencode-go-responses/grok-4.5" || c.Agent.SubagentModel != "opencode-go/qwen3.7-plus" || c.Bot.Model != "opencode-go-responses/grok-4.5" {
		t.Fatalf("historical model refs = default:%q planner:%q vision:%q subagent:%q bot:%q", c.DefaultModel, c.Agent.PlannerModel, c.Agent.VisionModel, c.Agent.SubagentModel, c.Bot.Model)
	}
	if len(c.Agent.SubagentModels) != 2 || c.Agent.SubagentModels["review"] != "opencode-go-responses/grok-4.5" || c.Agent.SubagentModels["local"] != "private-model" {
		t.Fatalf("historical subagent refs = %v", c.Agent.SubagentModels)
	}
	access := desktopProviderAccessMap(c.Desktop.ProviderAccess)
	if !access["opencode-go"] || !access["opencode-go-responses"] {
		t.Fatalf("provider access after split = %v", c.Desktop.ProviderAccess)
	}
	if normalizeLegacyOpenCodeGoInstalls(c) {
		t.Fatal("aggregate OpenCode Go route migration was not idempotent")
	}
}

func TestNormalizeLegacyOpenCodeGoRouteCatalogRepairsWrongSingleRouteInPlace(t *testing.T) {
	c := &Config{Providers: []ProviderEntry{{
		Name: "opencode-go", Kind: "anthropic", BaseURL: "https://opencode.ai/zen/go",
		Models: []string{"grok-4.5"}, Default: "grok-4.5", APIKeyEnv: "OPENCODE_GO_API_KEY",
	}}}
	if !normalizeLegacyOpenCodeGoRouteCatalog(c) {
		t.Fatal("wrong single-route OpenCode Go config was not migrated")
	}
	entry := c.Providers[0]
	if entry.Kind != "responses" || entry.BaseURL != "https://opencode.ai/zen/go/v1" || entry.DefaultModel() != "grok-4.5" || entry.PresetID != "opencode-go-responses" || entry.PresetVersion != ProviderPresetVersion || entry.BillingMode != "" {
		t.Fatalf("wrong single-route migration = %+v", entry)
	}
}

func TestNormalizeLegacyOpenCodeGoRouteCatalogLeavesCustomRequestURLUntouched(t *testing.T) {
	original := ProviderEntry{
		Name: "opencode-go", Kind: "anthropic", BaseURL: "https://opencode.ai/zen/go",
		RequestURL: "https://proxy.example/messages", Models: []string{"grok-4.5"}, Default: "grok-4.5",
	}
	c := &Config{Providers: []ProviderEntry{original}}
	if normalizeLegacyOpenCodeGoRouteCatalog(c) {
		t.Fatal("custom request_url provider was migrated")
	}
	if got := c.Providers[0]; got.Kind != original.Kind || got.BaseURL != original.BaseURL || got.RequestURL != original.RequestURL || !stringSlicesEqual(got.Models, original.Models) {
		t.Fatalf("custom request_url provider changed = %+v", got)
	}
}
