package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"reasonix/internal/provider"
)

func hasModel(c *Config, model string) *ProviderEntry {
	for i := range c.Providers {
		for _, m := range c.Providers[i].ModelList() {
			if m == model {
				return &c.Providers[i]
			}
		}
	}
	return nil
}

func TestBackfillDeepSeekProRestoresPro(t *testing.T) {
	c := &Config{Providers: []ProviderEntry{
		{Name: "deepseek-flash", Kind: "openai", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-flash", APIKeyEnv: "DEEPSEEK_API_KEY"},
	}}
	backfillDeepSeekPro(c)
	pro := hasModel(c, "deepseek-v4-pro")
	if pro == nil {
		t.Fatal("deepseek-v4-pro not restored")
	} else if pro.Price == nil || pro.Price.Output != 0.87 || pro.Price.Currency != "$" {
		t.Errorf("pro price = %+v, want default USD preset", pro.Price)
	}
}

func TestBackfillDeepSeekProUsesConfiguredLanguage(t *testing.T) {
	c := &Config{Language: "zh", Providers: []ProviderEntry{
		{Name: "deepseek-flash", Kind: "openai", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-flash", APIKeyEnv: "DEEPSEEK_API_KEY"},
	}}
	backfillDeepSeekPro(c)
	pro := hasModel(c, "deepseek-v4-pro")
	if pro == nil {
		t.Fatal("deepseek-v4-pro not restored")
	} else if pro.Price == nil || pro.Price.Output != 6 || pro.Price.Currency != "¥" {
		t.Errorf("pro price = %+v, want CNY preset", pro.Price)
	}
}

func TestBackfillDeepSeekProInheritsKeyEnv(t *testing.T) {
	c := &Config{Providers: []ProviderEntry{
		{Name: "deepseek-flash", Kind: "openai", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-flash", APIKeyEnv: "MY_DS_KEY"},
	}}
	backfillDeepSeekPro(c)
	if pro := hasModel(c, "deepseek-v4-pro"); pro == nil || pro.APIKeyEnv != "MY_DS_KEY" {
		t.Errorf("pro should inherit the flash key env, got %+v", pro)
	}
}

func TestBackfillDeepSeekProNoopWhenProPresent(t *testing.T) {
	c := &Config{Providers: []ProviderEntry{
		{Name: "deepseek-flash", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-flash"},
		{Name: "deepseek-pro", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-pro"},
	}}
	backfillDeepSeekPro(c)
	if n := len(c.Providers); n != 2 {
		t.Errorf("providers grew to %d; should be a no-op when pro is present", n)
	}
}

func TestBackfillDeepSeekProSkipsCustomEndpoint(t *testing.T) {
	c := &Config{Providers: []ProviderEntry{
		{Name: "myproxy", BaseURL: "https://proxy.example.com/v1", Model: "deepseek-v4-flash"},
	}}
	backfillDeepSeekPro(c)
	if hasModel(c, "deepseek-v4-pro") != nil {
		t.Error("must not add pro for a non-official endpoint that may not serve it")
	}
}

func TestBackfillDeepSeekProSkipsNonDeepSeek(t *testing.T) {
	c := &Config{Providers: []ProviderEntry{
		{Name: "mimo-flash", BaseURL: "https://token-plan-cn.xiaomimimo.com/v1", Model: "mimo-v2.5"},
	}}
	backfillDeepSeekPro(c)
	if len(c.Providers) != 1 {
		t.Error("unrelated config must be untouched")
	}
}

func TestNormalizeLegacyProviderModelsRepairsOfficialProvider(t *testing.T) {
	c := &Config{Providers: []ProviderEntry{{
		Name:      "deepseek-flash",
		Kind:      "openai",
		BaseURL:   "https://api.deepseek.com",
		APIKeyEnv: "DEEPSEEK_API_KEY",
	}}}
	normalizeLegacyProviderModels(c)
	if got := c.Providers[0].Model; got != "deepseek-v4-flash" {
		t.Fatalf("deepseek-flash model = %q, want deepseek-v4-flash", got)
	}
}

func TestNormalizeLegacyProviderModelsLeavesCustomProviderUntouched(t *testing.T) {
	c := &Config{Providers: []ProviderEntry{{
		Name:    "custom",
		Kind:    "openai",
		BaseURL: "https://proxy.example.com/v1",
	}}}
	normalizeLegacyProviderModels(c)
	if got := c.Providers[0].Model; got != "" {
		t.Fatalf("custom provider model = %q, want empty", got)
	}
}

func TestNormalizeLegacyStepFunBaseURLsMigratesPresetProviders(t *testing.T) {
	c := &Config{Providers: []ProviderEntry{
		{
			Name:      "stepfun",
			Kind:      "openai",
			BaseURL:   legacyStepFunOpenAIBaseURL,
			APIKeyEnv: "STEPFUN_API_KEY",
			PresetID:  "stepfun",
		},
		{
			Name:      "stepfun-anthropic",
			Kind:      "anthropic",
			BaseURL:   legacyStepFunAnthropicBaseURL + "/",
			APIKeyEnv: "STEPFUN_API_KEY",
			PresetID:  "stepfun-anthropic",
		},
		{
			Name:      "custom-stepfun",
			Kind:      "openai",
			BaseURL:   legacyStepFunOpenAIBaseURL,
			APIKeyEnv: "STEPFUN_API_KEY",
		},
	}}

	if !normalizeLegacyStepFunBaseURLs(c) {
		t.Fatal("legacy StepFun preset URL migration did not report a change")
	}
	if got := c.Providers[0].BaseURL; got != officialStepFunOpenAIBaseURL {
		t.Fatalf("stepfun base_url = %q, want %q", got, officialStepFunOpenAIBaseURL)
	}
	if got := c.Providers[1].BaseURL; got != officialStepFunAnthropicBaseURL {
		t.Fatalf("stepfun-anthropic base_url = %q, want %q", got, officialStepFunAnthropicBaseURL)
	}
	if got := c.Providers[2].BaseURL; got != legacyStepFunOpenAIBaseURL {
		t.Fatalf("custom provider base_url = %q, want untouched legacy URL", got)
	}
}

func TestLoadForEditKeepsLegacyStepFunMigrationInMemoryUntilSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	cfg := Default()
	stepfun, ok := CuratedProviderPreset("stepfun")
	if !ok || len(stepfun.Entries) != 1 {
		t.Fatal("missing stepfun preset")
	}
	stepfunEntry := stepfun.Entries[0]
	stepfunEntry.BaseURL = legacyStepFunOpenAIBaseURL
	stepfunAnthropic, ok := CuratedProviderPreset("stepfun-anthropic")
	if !ok || len(stepfunAnthropic.Entries) != 1 {
		t.Fatal("missing stepfun-anthropic preset")
	}
	stepfunAnthropicEntry := stepfunAnthropic.Entries[0]
	stepfunAnthropicEntry.BaseURL = legacyStepFunAnthropicBaseURL
	cfg.Providers = append(cfg.Providers, stepfunEntry, stepfunAnthropicEntry)
	cfg.Desktop.ProviderAccess = []string{"stepfun", "stepfun-anthropic"}
	if err := cfg.SaveTo(path); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}

	loaded := LoadForEdit(path)
	if got, _ := loaded.Provider("stepfun"); got == nil || got.BaseURL != officialStepFunOpenAIBaseURL {
		t.Fatalf("loaded stepfun = %+v, want official base URL", got)
	}
	if got, _ := loaded.Provider("stepfun-anthropic"); got == nil || got.BaseURL != officialStepFunAnthropicBaseURL {
		t.Fatalf("loaded stepfun-anthropic = %+v, want official base URL", got)
	}

	var disk Config
	if _, err := toml.DecodeFile(path, &disk); err != nil {
		t.Fatalf("decode config after read-only load: %v", err)
	}
	if got, _ := disk.Provider("stepfun"); got == nil || got.BaseURL != legacyStepFunOpenAIBaseURL {
		t.Fatalf("read-only LoadForEdit rewrote stepfun = %+v, want legacy base URL preserved on disk", got)
	}
	if err := loaded.SaveTo(path); err != nil {
		t.Fatalf("explicit SaveTo: %v", err)
	}
	disk = Config{}
	if _, err := toml.DecodeFile(path, &disk); err != nil {
		t.Fatalf("decode explicitly saved config: %v", err)
	}
	if got, _ := disk.Provider("stepfun"); got == nil || got.BaseURL != officialStepFunOpenAIBaseURL {
		t.Fatalf("persisted stepfun = %+v, want official base URL", got)
	}
	if got, _ := disk.Provider("stepfun-anthropic"); got == nil || got.BaseURL != officialStepFunAnthropicBaseURL {
		t.Fatalf("persisted stepfun-anthropic = %+v, want official base URL", got)
	}
}

func TestNormalizeLegacyLongCatContextWindowsMigratesOnlyUntouchedOfficialPresets(t *testing.T) {
	c := &Config{Providers: []ProviderEntry{
		{
			Name:          "longcat-openai",
			Kind:          "openai",
			BaseURL:       longCatOpenAIBaseURL,
			Models:        []string{"LongCat-2.0"},
			Default:       "LongCat-2.0",
			PresetID:      "longcat-openai",
			ContextWindow: legacyLongCat20ContextWindow,
		},
		{
			Name:          "longcat-anthropic",
			Kind:          "anthropic",
			BaseURL:       longCatAnthropicBaseURL + "/",
			Models:        []string{"LongCat-2.0"},
			Default:       "LongCat-2.0",
			PresetID:      "longcat-anthropic",
			ContextWindow: legacyLongCat20ContextWindow,
		},
		{
			Name:          "custom-longcat",
			Kind:          "openai",
			BaseURL:       longCatOpenAIBaseURL,
			Models:        []string{"LongCat-2.0"},
			Default:       "LongCat-2.0",
			ContextWindow: legacyLongCat20ContextWindow,
		},
		{
			Name:          "longcat-custom-window",
			Kind:          "openai",
			BaseURL:       longCatOpenAIBaseURL,
			Models:        []string{"LongCat-2.0"},
			Default:       "LongCat-2.0",
			PresetID:      "longcat-openai",
			ContextWindow: 262_144,
		},
		{
			Name:          "longcat-custom-models",
			Kind:          "openai",
			BaseURL:       longCatOpenAIBaseURL,
			Models:        []string{"LongCat-2.0", "LongCat-Future"},
			Default:       "LongCat-2.0",
			PresetID:      "longcat-openai",
			ContextWindow: legacyLongCat20ContextWindow,
		},
		{
			Name:          "longcat-custom-endpoint",
			Kind:          "openai",
			BaseURL:       "https://api.longcat.chat/custom/v1",
			Models:        []string{"LongCat-2.0"},
			Default:       "LongCat-2.0",
			PresetID:      "longcat-openai",
			ContextWindow: legacyLongCat20ContextWindow,
		},
		{
			Name:          "longcat-custom-default",
			Kind:          "openai",
			BaseURL:       longCatOpenAIBaseURL,
			Models:        []string{"LongCat-2.0"},
			PresetID:      "longcat-openai",
			ContextWindow: legacyLongCat20ContextWindow,
		},
	}}

	if !normalizeLegacyLongCatContextWindows(c) {
		t.Fatal("legacy LongCat context-window migration did not report a change")
	}
	if got := c.Providers[0].ContextWindow; got != longCat20ContextWindow {
		t.Fatalf("longcat-openai context_window = %d, want %d", got, longCat20ContextWindow)
	}
	if got := c.Providers[1].ContextWindow; got != longCat20ContextWindow {
		t.Fatalf("longcat-anthropic context_window = %d, want %d", got, longCat20ContextWindow)
	}
	if got := c.Providers[2].ContextWindow; got != legacyLongCat20ContextWindow {
		t.Fatalf("custom LongCat context_window = %d, want unchanged %d", got, legacyLongCat20ContextWindow)
	}
	if got := c.Providers[3].ContextWindow; got != 262_144 {
		t.Fatalf("customized preset context_window = %d, want unchanged 262144", got)
	}
	if got := c.Providers[4].ContextWindow; got != legacyLongCat20ContextWindow {
		t.Fatalf("customized model catalog context_window = %d, want unchanged %d", got, legacyLongCat20ContextWindow)
	}
	if got := c.Providers[5].ContextWindow; got != legacyLongCat20ContextWindow {
		t.Fatalf("customized endpoint context_window = %d, want unchanged %d", got, legacyLongCat20ContextWindow)
	}
	if got := c.Providers[6].ContextWindow; got != legacyLongCat20ContextWindow {
		t.Fatalf("customized default model context_window = %d, want unchanged %d", got, legacyLongCat20ContextWindow)
	}
}

func TestLoadForEditAppliesLegacyLongCatContextWindowMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	cfg := Default()
	for _, id := range []string{"longcat-openai", "longcat-anthropic"} {
		preset, ok := CuratedProviderPreset(id)
		if !ok || len(preset.Entries) != 1 {
			t.Fatalf("missing %s preset", id)
		}
		entry := preset.Entries[0]
		entry.ContextWindow = legacyLongCat20ContextWindow
		cfg.Providers = append(cfg.Providers, entry)
	}
	if err := cfg.SaveTo(path); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}

	loaded := LoadForEdit(path)
	for _, id := range []string{"longcat-openai", "longcat-anthropic"} {
		if got, _ := loaded.Provider(id); got == nil || got.ContextWindow != longCat20ContextWindow {
			t.Fatalf("loaded %s = %+v, want context_window %d", id, got, longCat20ContextWindow)
		}
	}
}

func TestNormalizeLegacyOpenCodeGoKimiK3CatalogMigratesOnlyUntouchedPreset(t *testing.T) {
	legacyEntry := ProviderEntry{
		Name:          "opencode-go",
		Kind:          "openai",
		BaseURL:       "https://opencode.ai/zen/go/v1/",
		Models:        append([]string(nil), legacyOpenCodeGoModels...),
		Default:       "glm-5.2",
		APIKeyEnv:     "CUSTOM_OPENCODE_GO_KEY",
		ContextWindow: 256_000,
		PresetID:      "opencode-go",
		ModelOverrides: map[string]ProviderModelOverride{
			"deepseek-v4-pro": {
				ReasoningProtocol: ReasoningProtocolDeepSeek,
				SupportedEfforts:  []string{"high", "max"},
				DefaultEffort:     "high",
			},
		},
	}
	customModels := legacyEntry
	customModels.Name = "opencode-go-custom-models"
	customModels.Models = append(append([]string(nil), customModels.Models...), "private-model")
	customModels.ModelOverrides = cloneModelOverrideMap(legacyEntry.ModelOverrides)
	customEndpoint := legacyEntry
	customEndpoint.Name = "opencode-go-custom-endpoint"
	customEndpoint.BaseURL = "https://gateway.example.com/v1"
	customEndpoint.ModelOverrides = cloneModelOverrideMap(legacyEntry.ModelOverrides)
	noPresetIdentity := legacyEntry
	noPresetIdentity.Name = "opencode-go-copy"
	noPresetIdentity.PresetID = ""
	noPresetIdentity.ModelOverrides = cloneModelOverrideMap(legacyEntry.ModelOverrides)
	c := &Config{Providers: []ProviderEntry{legacyEntry, customModels, customEndpoint, noPresetIdentity}}

	if !normalizeLegacyOpenCodeGoKimiK3Catalog(c) {
		t.Fatal("legacy OpenCode Go catalog migration did not report a change")
	}
	got := &c.Providers[0]
	if !got.HasModel("kimi-k3") || !got.HasVisionModel("kimi-k3") {
		t.Fatalf("migrated OpenCode Go catalog = %+v, want Kimi K3 with vision", got)
	}
	k3 := got.ModelOverrides["kimi-k3"]
	if k3.ReasoningProtocol != ReasoningProtocolOpenAI ||
		!stringSlicesEqual(k3.SupportedEfforts, []string{"high", "max"}) ||
		k3.DefaultEffort != "max" ||
		k3.ContextWindow != 1_048_576 {
		t.Fatalf("migrated Kimi K3 override = %+v", k3)
	}
	if got.APIKeyEnv != "CUSTOM_OPENCODE_GO_KEY" || got.ContextWindow != 256_000 {
		t.Fatalf("unrelated provider edits were not preserved: %+v", got)
	}
	if _, ok := got.ModelOverrides["deepseek-v4-pro"]; !ok {
		t.Fatal("existing model override was dropped")
	}
	for i := 1; i < len(c.Providers); i++ {
		if c.Providers[i].HasModel("kimi-k3") {
			t.Fatalf("customized provider %q was unexpectedly migrated", c.Providers[i].Name)
		}
	}

	preIdentity := legacyEntry
	preIdentity.PresetID = ""
	preIdentity.ModelOverrides = cloneModelOverrideMap(legacyEntry.ModelOverrides)
	preIdentityConfig := &Config{Providers: []ProviderEntry{preIdentity}}
	if !normalizeLegacyOpenCodeGoKimiK3Catalog(preIdentityConfig) || !preIdentityConfig.Providers[0].HasModel("kimi-k3") {
		t.Fatal("pre-preset-identity OpenCode Go install was not migrated")
	}

	vision := false
	customK3 := legacyEntry
	customK3.ModelOverrides = cloneModelOverrideMap(legacyEntry.ModelOverrides)
	delete(customK3.ModelOverrides, "kimi-k3")
	wantK3 := ProviderModelOverride{
		ReasoningProtocol: ReasoningProtocolNone,
		SupportedEfforts:  []string{"low"},
		DefaultEffort:     "low",
		Vision:            &vision,
		ContextWindow:     262_144,
	}
	customK3.ModelOverrides["KIMI-K3"] = wantK3
	customK3Config := &Config{Providers: []ProviderEntry{customK3}}
	if !normalizeLegacyOpenCodeGoKimiK3Catalog(customK3Config) {
		t.Fatal("legacy OpenCode Go catalog with custom Kimi K3 override was not migrated")
	}
	gotK3, ok := customK3Config.Providers[0].ModelOverrides["KIMI-K3"]
	if !ok || !reflect.DeepEqual(gotK3, wantK3) {
		t.Fatalf("custom Kimi K3 override = %+v, want preserved %+v", gotK3, wantK3)
	}
	if _, duplicate := customK3Config.Providers[0].ModelOverrides["kimi-k3"]; duplicate {
		t.Fatal("migration added a duplicate case-insensitive Kimi K3 override")
	}
}

func TestNormalizeLegacyKimiK3CatalogMigratesOnlyOfficialUntouchedPresets(t *testing.T) {
	legacyEntry := func(id, baseURL string) ProviderEntry {
		return ProviderEntry{
			Name:              id,
			Kind:              "openai",
			BaseURL:           baseURL + "/",
			Models:            append([]string(nil), legacyKimiAPIModels...),
			VisionModels:      append([]string(nil), legacyKimiAPIModels...),
			Default:           "kimi-k2.7-code",
			APIKeyEnv:         "CUSTOM_KIMI_KEY",
			ContextWindow:     300_000,
			ReasoningProtocol: ReasoningProtocolNone,
			PresetID:          id,
		}
	}
	cn := legacyEntry("kimi-cn", "https://api.moonshot.cn/v1")
	cn.Name = "my-kimi-cn"
	global := legacyEntry("kimi-global", "https://api.moonshot.ai/v1")
	global.PresetID = ""
	customModels := legacyEntry("kimi-cn", "https://api.moonshot.cn/v1")
	customModels.Models = append(customModels.Models, "private-model")
	customEndpoint := legacyEntry("kimi-global", "https://api.moonshot.ai/v1")
	customEndpoint.BaseURL = "https://gateway.example.com/v1"
	selectedModel := legacyEntry("kimi-cn", "https://api.moonshot.cn/v1")
	selectedModel.Model = "kimi-k2.7-code"
	c := &Config{Providers: []ProviderEntry{cn, global, customModels, customEndpoint, selectedModel}}

	if !normalizeLegacyKimiK3Catalog(c) {
		t.Fatal("legacy Kimi direct catalogs did not report a change")
	}
	for i := 0; i < 2; i++ {
		got := &c.Providers[i]
		if !got.HasModel("kimi-k3") || !got.HasVisionModel("kimi-k3") {
			t.Fatalf("migrated Kimi provider %d = %+v, want K3 with vision", i, got)
		}
		k3 := got.ModelOverrides["kimi-k3"]
		if k3.ReasoningProtocol != ReasoningProtocolOpenAI ||
			!stringSlicesEqual(k3.SupportedEfforts, []string{"low", "high", "max"}) ||
			k3.DefaultEffort != "max" || k3.ContextWindow != 1_048_576 {
			t.Fatalf("migrated Kimi K3 override %d = %+v", i, k3)
		}
		if got.APIKeyEnv != "CUSTOM_KIMI_KEY" || got.ContextWindow != 300_000 || got.Default != "kimi-k2.7-code" {
			t.Fatalf("unrelated Kimi provider edits were not preserved: %+v", got)
		}
	}
	for i := 2; i < len(c.Providers); i++ {
		if c.Providers[i].HasModel("kimi-k3") {
			t.Fatalf("customized Kimi provider %q was unexpectedly migrated", c.Providers[i].Name)
		}
	}

	vision := false
	customK3 := legacyEntry("kimi-cn", "https://api.moonshot.cn/v1")
	wantK3 := ProviderModelOverride{
		ReasoningProtocol: ReasoningProtocolNone,
		SupportedEfforts:  []string{"low"},
		DefaultEffort:     "low",
		Vision:            &vision,
		ContextWindow:     262_144,
	}
	customK3.ModelOverrides = map[string]ProviderModelOverride{"KIMI-K3": wantK3}
	customConfig := &Config{Providers: []ProviderEntry{customK3}}
	if !normalizeLegacyKimiK3Catalog(customConfig) {
		t.Fatal("legacy Kimi catalog with custom K3 override was not migrated")
	}
	gotK3, ok := customConfig.Providers[0].ModelOverrides["KIMI-K3"]
	if !ok || !reflect.DeepEqual(gotK3, wantK3) {
		t.Fatalf("custom direct Kimi K3 override = %+v, want preserved %+v", gotK3, wantK3)
	}
	if _, duplicate := customConfig.Providers[0].ModelOverrides["kimi-k3"]; duplicate {
		t.Fatal("migration added a duplicate case-insensitive direct Kimi K3 override")
	}
}

func TestNormalizeLegacyKimiK3CatalogPreservesVisionChoices(t *testing.T) {
	base := ProviderEntry{
		Name:         "kimi-cn",
		Kind:         "openai",
		BaseURL:      "https://api.moonshot.cn/v1",
		Models:       append([]string(nil), legacyKimiAPIModels...),
		Default:      "kimi-k2.7-code",
		PresetID:     "kimi-cn",
		VisionModels: []string{},
	}

	custom := base
	custom.VisionModels = []string{"kimi-k2.5"}
	c := &Config{Providers: []ProviderEntry{base, custom}}

	if !normalizeLegacyKimiK3Catalog(c) {
		t.Fatal("legacy Kimi catalog migration did not report a change")
	}
	if got := c.Providers[0].VisionModels; got == nil || len(got) != 0 {
		t.Fatalf("explicitly disabled vision models = %#v, want explicit empty list", got)
	}
	if got := c.Providers[1].VisionModels; !reflect.DeepEqual(got, []string{"kimi-k2.5"}) {
		t.Fatalf("custom vision models = %v, want [kimi-k2.5]", got)
	}
}

func TestLoadForEditKeepsLegacyKimiK3CatalogMigrationInMemoryUntilSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	cfg := Default()
	preset, ok := CuratedProviderPreset("kimi-cn")
	if !ok || len(preset.Entries) != 1 {
		t.Fatal("missing kimi-cn preset")
	}
	entry := preset.Entries[0]
	entry.Models = append([]string(nil), legacyKimiAPIModels...)
	entry.VisionModels = append([]string(nil), legacyKimiAPIModels...)
	delete(entry.ModelOverrides, "kimi-k3")
	cfg.Providers = append(cfg.Providers, entry)
	if err := cfg.SaveTo(path); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}

	loaded := LoadForEdit(path)
	got, ok := loaded.Provider("kimi-cn")
	if !ok || !got.HasModel("kimi-k3") || !got.HasVisionModel("kimi-k3") {
		t.Fatalf("loaded kimi-cn = %+v, want persisted K3 catalog", got)
	}
	var disk Config
	if _, err := toml.DecodeFile(path, &disk); err != nil {
		t.Fatalf("decode config after read-only load: %v", err)
	}
	persisted, ok := disk.Provider("kimi-cn")
	if !ok || persisted.HasModel("kimi-k3") {
		t.Fatalf("read-only LoadForEdit rewrote kimi-cn = %+v, want legacy catalog preserved on disk", persisted)
	}
	if err := loaded.SaveTo(path); err != nil {
		t.Fatalf("explicit SaveTo: %v", err)
	}
	disk = Config{}
	if _, err := toml.DecodeFile(path, &disk); err != nil {
		t.Fatalf("decode explicitly saved config: %v", err)
	}
	persisted, ok = disk.Provider("kimi-cn")
	if !ok || !persisted.HasModel("kimi-k3") || persisted.ModelOverrides["kimi-k3"].DefaultEffort != "max" {
		t.Fatalf("persisted kimi-cn = %+v, want K3 defaults", persisted)
	}
}

func TestLoadForEditKeepsLegacyOpenCodeGoKimiK3CatalogMigrationInMemoryUntilSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	cfg := Default()
	preset, ok := CuratedProviderPreset("opencode-go")
	if !ok || len(preset.Entries) != 1 {
		t.Fatal("missing opencode-go preset")
	}
	entry := preset.Entries[0]
	entry.Models = append([]string(nil), legacyOpenCodeGoModels...)
	entry.VisionModels = nil
	delete(entry.ModelOverrides, "kimi-k3")
	cfg.Providers = append(cfg.Providers, entry)
	cfg.Desktop.ProviderAccess = []string{"opencode-go"}
	if err := cfg.SaveTo(path); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}

	loaded := LoadForEdit(path)
	got, ok := loaded.Provider("opencode-go")
	if !ok || !got.HasModel("kimi-k3") || !got.HasVisionModel("kimi-k3") {
		t.Fatalf("loaded opencode-go = %+v, want persisted Kimi K3 catalog", got)
	}
	if ov := got.ModelOverrides["kimi-k3"]; ov.DefaultEffort != "max" || ov.ContextWindow != 1_048_576 {
		t.Fatalf("loaded Kimi K3 override = %+v", ov)
	}

	var disk Config
	if _, err := toml.DecodeFile(path, &disk); err != nil {
		t.Fatalf("decode config after read-only load: %v", err)
	}
	persisted, ok := disk.Provider("opencode-go")
	if !ok || persisted.HasModel("kimi-k3") {
		t.Fatalf("read-only LoadForEdit rewrote opencode-go = %+v, want legacy catalog preserved on disk", persisted)
	}
	if err := loaded.SaveTo(path); err != nil {
		t.Fatalf("explicit SaveTo: %v", err)
	}
	disk = Config{}
	if _, err := toml.DecodeFile(path, &disk); err != nil {
		t.Fatalf("decode explicitly saved config: %v", err)
	}
	persisted, ok = disk.Provider("opencode-go")
	if !ok || !persisted.HasModel("kimi-k3") || !persisted.HasVisionModel("kimi-k3") {
		t.Fatalf("persisted opencode-go = %+v, want Kimi K3 catalog", persisted)
	}
}

func TestNormalizeLegacyOpenCodeGoKimiK3CatalogPreservesVisionChoices(t *testing.T) {
	base := ProviderEntry{
		Name:     "opencode-go",
		Kind:     "openai",
		BaseURL:  "https://opencode.ai/zen/go/v1",
		Models:   append([]string(nil), legacyOpenCodeGoModels...),
		Default:  "glm-5.2",
		PresetID: "opencode-go",
	}

	tests := []struct {
		name   string
		vision []string
		want   []string
	}{
		{name: "explicitly disabled", vision: []string{}, want: []string{}},
		{name: "custom list", vision: []string{"mimo-v2.5"}, want: []string{"mimo-v2.5"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := base
			entry.VisionModels = tt.vision
			c := &Config{Providers: []ProviderEntry{entry}}
			if !normalizeLegacyOpenCodeGoKimiK3Catalog(c) {
				t.Fatal("legacy OpenCode Go catalog migration did not report a change")
			}
			if got := c.Providers[0].VisionModels; !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("vision models = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestNormalizeDesktopOfficialProviderAccessCanonicalizesOnlyDeepSeekIDs(t *testing.T) {
	c := Default()
	c.DefaultModel = "deepseek-flash/deepseek-v4-pro"
	c.Desktop.ProviderAccess = []string{"deepseek-flash", "mimo-pro"}
	normalizeDesktopOfficialProviderAccess(c)
	if len(c.Desktop.ProviderAccess) != 2 || c.Desktop.ProviderAccess[0] != "deepseek" || c.Desktop.ProviderAccess[1] != "mimo-pro" {
		t.Fatalf("provider_access = %+v, want only DeepSeek canonicalized", c.Desktop.ProviderAccess)
	}
	if c.DefaultModel != "deepseek/deepseek-v4-pro" {
		t.Fatalf("default_model = %q, want deepseek/deepseek-v4-pro", c.DefaultModel)
	}
	if _, ok := c.Provider("deepseek"); !ok {
		t.Fatal("canonical deepseek provider missing")
	}
	if _, ok := c.Provider("mimo-token-plan"); ok {
		t.Fatal("mimo-token-plan should not be injected as an official provider")
	}
}

func TestNormalizeDesktopOfficialProviderAccessBackfillsOfficialContextWindow(t *testing.T) {
	c := &Config{
		Desktop: DesktopConfig{ProviderAccess: []string{"deepseek-flash", "mimo-api", "mimo-token-plan"}},
		Providers: []ProviderEntry{
			{
				Name:      "deepseek-flash",
				Kind:      "openai",
				BaseURL:   "https://api.deepseek.com",
				Model:     "deepseek-v4-flash",
				APIKeyEnv: "DEEPSEEK_API_KEY",
			},
			{
				Name:      "mimo-api",
				Kind:      "openai",
				BaseURL:   "https://api.xiaomimimo.com/v1",
				Model:     "mimo-v2.5-pro",
				APIKeyEnv: "MIMO_API_KEY",
			},
			{
				Name:      "mimo-token-plan",
				Kind:      "openai",
				BaseURL:   "https://token-plan-cn.xiaomimimo.com/v1",
				Model:     "mimo-v2.5-pro",
				APIKeyEnv: "MIMO_API_KEY",
			},
		},
	}

	normalizeDesktopOfficialProviderAccess(c)

	deepseek, ok := c.Provider("deepseek")
	if !ok {
		t.Fatal("deepseek provider missing")
	}
	if deepseek.ContextWindow != 1_000_000 {
		t.Fatalf("deepseek context_window = %d, want official default", deepseek.ContextWindow)
	}
	mimoAPI, ok := c.Provider("mimo-api")
	if !ok {
		t.Fatal("mimo-api provider missing")
	}
	if mimoAPI.ContextWindow != 1_048_576 {
		t.Fatalf("mimo-api context_window = %d, want migrated MiMo default", mimoAPI.ContextWindow)
	}
	mimoTokenPlan, ok := c.Provider("mimo-token-plan")
	if !ok {
		t.Fatal("mimo-token-plan provider missing")
	}
	if mimoTokenPlan.ContextWindow != 1_048_576 {
		t.Fatalf("mimo-token-plan context_window = %d, want migrated MiMo default", mimoTokenPlan.ContextWindow)
	}
}

func TestNormalizeDesktopOfficialProviderAccessKeepsCustomAlias(t *testing.T) {
	c := &Config{
		Desktop: DesktopConfig{ProviderAccess: []string{"deepseek-flash"}},
		Providers: []ProviderEntry{{
			Name:    "deepseek-flash",
			Kind:    "openai",
			BaseURL: "https://proxy.example/v1",
			Model:   "deepseek-v4-flash",
		}},
	}

	normalizeDesktopOfficialProviderAccess(c)

	if len(c.Desktop.ProviderAccess) != 1 || c.Desktop.ProviderAccess[0] != "deepseek-flash" {
		t.Fatalf("provider_access = %+v, want custom alias preserved", c.Desktop.ProviderAccess)
	}
	if _, ok := c.Provider("deepseek"); ok {
		t.Fatal("custom deepseek-flash proxy should not create canonical deepseek provider")
	}
}

func TestNormalizeOfficialDeepSeekModelsRepairsCanonicalProvider(t *testing.T) {
	c := &Config{
		DefaultModel: "deepseek-flash/deepseek-v4-flash",
		Desktop:      DesktopConfig{ProviderAccess: []string{"deepseek"}},
		Providers: []ProviderEntry{{
			Name:      "deepseek",
			Kind:      "openai",
			BaseURL:   "https://api.deepseek.com",
			Model:     "glm-5",
			APIKeyEnv: "DEEPSEEK_API_KEY",
		}},
	}
	normalizeDesktopOfficialProviderAccess(c)
	normalizeOfficialDeepSeekModels(c)

	p, ok := c.Provider("deepseek")
	if !ok {
		t.Fatal("deepseek provider missing")
	}
	if !p.HasModel("deepseek-v4-flash") || !p.HasModel("deepseek-v4-pro") || !p.HasModel("glm-5") {
		t.Fatalf("deepseek models = %+v, want official models plus existing model", p.ModelList())
	}
	if c.DefaultModel != "deepseek/deepseek-v4-flash" {
		t.Fatalf("default_model = %q, want retargeted official ref", c.DefaultModel)
	}
	if _, ok := c.ResolveModel(c.DefaultModel); !ok {
		t.Fatalf("retargeted default_model %q should resolve", c.DefaultModel)
	}
}

func TestNormalizeOfficialDeepSeekModelsLeavesExternalEndpointUntouched(t *testing.T) {
	c := &Config{Providers: []ProviderEntry{{
		Name:    "deepseek",
		Kind:    "openai",
		BaseURL: "https://proxy.example.com/v1",
		Model:   "glm-5",
	}}}
	normalizeOfficialDeepSeekModels(c)

	p, ok := c.Provider("deepseek")
	if !ok {
		t.Fatal("deepseek provider missing")
	}
	if p.HasModel("deepseek-v4-flash") || p.HasModel("deepseek-v4-pro") {
		t.Fatalf("external endpoint models = %+v, want untouched custom list", p.ModelList())
	}
}

func TestNormalizeLegacyMimoCustomProvidersEnsuresReferencedMimoAPI(t *testing.T) {
	c := Default()
	c.DefaultModel = "mimo-api/mimo-v2.5-pro"
	c.Desktop.ProviderAccess = []string{"mimo-api"}
	if !normalizeLegacyMimoCustomProviders(c) {
		t.Fatal("legacy MiMo migration did not report a change")
	}
	p, ok := c.Provider("mimo-api")
	if !ok {
		t.Fatal("mimo-api paid provider missing")
	}
	if !p.HasModel("mimo-v2.5") || !p.HasModel("mimo-v2-omni") {
		t.Fatalf("mimo-api models = %v, want vision-capable MiMo models", p.ModelList())
	}
	normalizeDesktopOfficialProviderAccess(c)
	if got := c.Desktop.ProviderAccess; len(got) != 1 || got[0] != "mimo-api" {
		t.Fatalf("provider_access = %+v, want mimo-api", got)
	}
}

func TestNormalizeLegacyMimoCustomProvidersRecognizesBareModelRefs(t *testing.T) {
	c := Default()
	c.DefaultModel = "mimo-v2.5-pro"
	if !normalizeLegacyMimoCustomProviders(c) {
		t.Fatal("legacy bare MiMo model migration did not report a change")
	}
	p, ok := c.Provider("mimo-pro")
	if !ok {
		t.Fatal("mimo-pro provider missing")
	}
	if p.Model != "mimo-v2.5-pro" {
		t.Fatalf("mimo-pro model = %q, want mimo-v2.5-pro", p.Model)
	}
	if e, ok := c.ResolveModel("mimo-v2.5-pro"); !ok || e.Name != "mimo-pro" {
		t.Fatalf("bare MiMo model resolved to %+v/%v, want mimo-pro", e, ok)
	}
}

func TestNormalizeLegacyMimoCustomProvidersScansBotRefs(t *testing.T) {
	c := Default()
	c.Bot.Model = "mimo-pro"
	c.Bot.Connections = []BotConnectionConfig{{Model: "mimo-flash"}}
	if !normalizeLegacyMimoCustomProviders(c) {
		t.Fatal("legacy bot MiMo migration did not report a change")
	}
	if _, ok := c.Provider("mimo-pro"); !ok {
		t.Fatal("mimo-pro provider missing")
	}
	if _, ok := c.Provider("mimo-flash"); !ok {
		t.Fatal("mimo-flash provider missing")
	}
}

func TestNormalizeLegacyDesktopProviderAccessIncludesUnconfiguredMimoRefs(t *testing.T) {
	c := Default()
	c.DefaultModel = "mimo-pro"
	c.Bot.Connections = []BotConnectionConfig{{Model: "mimo-flash"}}
	normalizeLegacyMimoCustomProviders(c)
	NormalizeLegacyDesktopProviderAccess(c)
	access := desktopProviderAccessMap(c.Desktop.ProviderAccess)
	if !access["mimo-pro"] || !access["mimo-flash"] {
		t.Fatalf("provider_access = %+v, want unconfigured migrated MiMo refs visible", c.Desktop.ProviderAccess)
	}
}

func TestBackfillDeepSeekOfficialPrices(t *testing.T) {
	c := &Config{Providers: []ProviderEntry{{
		Name:    "deepseek",
		Kind:    "openai",
		BaseURL: "https://api.deepseek.com",
		Models:  []string{"deepseek-v4-flash", "deepseek-v4-pro"},
		Default: "deepseek-v4-flash",
	}}}
	backfillDeepSeekOfficialPrices(c)
	p, ok := c.Provider("deepseek")
	if !ok {
		t.Fatal("deepseek provider missing")
	}
	if p.Prices["deepseek-v4-flash"].Output != 0.28 || p.Prices["deepseek-v4-flash"].Currency != "$" || p.Prices["deepseek-v4-pro"].Output != 0.87 || p.Prices["deepseek-v4-pro"].Currency != "$" {
		t.Fatalf("deepseek prices = %+v, want default USD flash/pro prices", p.Prices)
	}
}

func TestBackfillDeepSeekOfficialPricesUsesConfiguredLanguage(t *testing.T) {
	c := &Config{Language: "zh", Providers: []ProviderEntry{{
		Name:    "deepseek",
		Kind:    "openai",
		BaseURL: "https://api.deepseek.com",
		Models:  []string{"deepseek-v4-flash", "deepseek-v4-pro"},
		Default: "deepseek-v4-flash",
	}}}
	backfillDeepSeekOfficialPrices(c)
	p, ok := c.Provider("deepseek")
	if !ok {
		t.Fatal("deepseek provider missing")
	}
	if p.Prices["deepseek-v4-flash"].Output != 2 || p.Prices["deepseek-v4-flash"].Currency != "¥" || p.Prices["deepseek-v4-pro"].Output != 6 || p.Prices["deepseek-v4-pro"].Currency != "¥" {
		t.Fatalf("deepseek prices = %+v, want CNY flash/pro prices", p.Prices)
	}
}

func TestBackfillDeepSeekOfficialPricesKeepsProviderWidePrice(t *testing.T) {
	custom := &provider.Pricing{CacheHit: 9, Input: 9, Output: 9, Currency: "$"}
	c := &Config{Providers: []ProviderEntry{{
		Name:    "deepseek",
		Kind:    "openai",
		BaseURL: "https://api.deepseek.com",
		Models:  []string{"deepseek-v4-flash", "deepseek-v4-pro"},
		Default: "deepseek-v4-flash",
		Price:   custom,
		Prices: map[string]*provider.Pricing{
			"deepseek-v4-flash": {CacheHit: 1, Input: 1, Output: 1, Currency: "$"},
		},
	}}}
	backfillDeepSeekOfficialPrices(c)
	p, ok := c.Provider("deepseek")
	if !ok {
		t.Fatal("deepseek provider missing")
	}
	if len(p.Prices) != 1 {
		t.Fatalf("deepseek prices = %+v, want existing per-model prices only", p.Prices)
	}
	pro, ok := c.ResolveModel("deepseek/deepseek-v4-pro")
	if !ok {
		t.Fatal("deepseek pro did not resolve")
	}
	if pro.Price == nil || pro.Price.Output != 9 {
		t.Fatalf("pro price = %+v, want provider-wide custom price", pro.Price)
	}
	flash, ok := c.ResolveModel("deepseek")
	if !ok {
		t.Fatal("deepseek default did not resolve")
	}
	if flash.Price == nil || flash.Price.Output != 1 {
		t.Fatalf("flash price = %+v, want existing per-model custom price", flash.Price)
	}
}

func TestApplyDeepSeekOfficialDefaultPricingUsesConfiguredLanguage(t *testing.T) {
	c := Default()
	c.Language = "zh"
	applyDeepSeekOfficialDefaultPricing(c)
	flash, ok := c.Provider("deepseek-flash")
	if !ok {
		t.Fatal("deepseek-flash provider missing")
	}
	if flash.Price == nil || flash.Price.Output != 2 || flash.Price.Currency != "¥" {
		t.Fatalf("flash price = %+v, want CNY preset", flash.Price)
	}
	pro, ok := c.Provider("deepseek-pro")
	if !ok {
		t.Fatal("deepseek-pro provider missing")
	}
	if pro.Price == nil || pro.Price.Output != 6 || pro.Price.Currency != "¥" {
		t.Fatalf("pro price = %+v, want CNY preset", pro.Price)
	}
}

func TestApplyDeepSeekOfficialDefaultPricingKeepsCustomPrice(t *testing.T) {
	c := &Config{Language: "zh", Providers: []ProviderEntry{{
		Name:    "deepseek-flash",
		Kind:    "openai",
		BaseURL: "https://api.deepseek.com",
		Model:   "deepseek-v4-flash",
		Price:   &provider.Pricing{CacheHit: 9, Input: 9, Output: 9, Currency: "$"},
	}}}
	applyDeepSeekOfficialDefaultPricing(c)
	p, ok := c.Provider("deepseek-flash")
	if !ok {
		t.Fatal("deepseek-flash provider missing")
	}
	if p.Price == nil || p.Price.Output != 9 || p.Price.Currency != "$" {
		t.Fatalf("custom price = %+v, want unchanged", p.Price)
	}
}

func TestLoadForEditAutoCurrencyKeepsPersistedOfficialUSDPrice(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	body := `language = "zh"

[[providers]]
name = "deepseek-flash"
kind = "openai"
base_url = "https://api.deepseek.com"
model = "deepseek-v4-flash"
price = { cache_hit = 0.0028, input = 0.14, output = 0.28, currency = "$" }
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	c := LoadForEdit(path)
	flash, ok := c.Provider("deepseek-flash")
	if !ok {
		t.Fatal("deepseek-flash provider missing")
	}
	if flash.Price == nil || flash.Price.Output != 0.28 || flash.Price.Currency != "$" {
		t.Fatalf("flash price = %+v, want persisted USD preset", flash.Price)
	}

	if err := c.SetDesktopCurrency("CNY"); err != nil {
		t.Fatalf("SetDesktopCurrency CNY: %v", err)
	}
	if flash.Price == nil || flash.Price.Output != 2 || flash.Price.Currency != "¥" {
		t.Fatalf("flash price = %+v, want explicit CNY preset", flash.Price)
	}
}

func TestLoadForRootAutoCurrencyKeepsPersistedOfficialUSDPrice(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("REASONIX_CREDENTIALS_STORE", "file")
	body := `language = "zh"

[[providers]]
name = "deepseek-flash"
kind = "openai"
base_url = "https://api.deepseek.com"
model = "deepseek-v4-flash"
price = { cache_hit = 0.0028, input = 0.14, output = 0.28, currency = "$" }
`
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	c, err := LoadForRoot(t.TempDir())
	if err != nil {
		t.Fatalf("LoadForRoot: %v", err)
	}
	flash, ok := c.Provider("deepseek-flash")
	if !ok {
		t.Fatal("deepseek-flash provider missing")
	}
	if flash.Price == nil || flash.Price.Output != 0.28 || flash.Price.Currency != "$" {
		t.Fatalf("flash price = %+v, want persisted USD preset", flash.Price)
	}
}

func TestRuntimeAutoCurrencyKeepsPersistedOfficialUSDPrice(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	body := `[[providers]]
name = "deepseek-flash"
kind = "openai"
base_url = "https://api.deepseek.com"
model = "deepseek-v4-flash"
price = { cache_hit = 0.0028, input = 0.14, output = 0.28, currency = "$" }
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	c := LoadForEdit(path)
	c.ApplyRuntimeAutoPricingCurrency("CNY")
	flash, ok := c.Provider("deepseek-flash")
	if !ok {
		t.Fatal("deepseek-flash provider missing")
	}
	if flash.Price == nil || flash.Price.Output != 0.28 || flash.Price.Currency != "$" {
		t.Fatalf("flash price = %+v, want persisted USD preset", flash.Price)
	}
}

func TestLoadForRootAutoCurrencyDoesNotMixPartialOfficialPrices(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("REASONIX_CREDENTIALS_STORE", "file")
	body := `language = "zh"

[[providers]]
name = "deepseek"
kind = "openai"
base_url = "https://api.deepseek.com"
models = ["deepseek-v4-flash", "deepseek-v4-pro"]
default = "deepseek-v4-flash"

[providers.prices]
deepseek-v4-flash = { cache_hit = 0.0028, input = 0.14, output = 0.28, currency = "$" }
`
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	c, err := LoadForRoot(t.TempDir())
	if err != nil {
		t.Fatalf("LoadForRoot: %v", err)
	}
	p, ok := c.Provider("deepseek")
	if !ok {
		t.Fatal("deepseek provider missing")
	}
	for _, model := range p.Models {
		price := p.Prices[model]
		if price == nil || price.Currency != "¥" {
			t.Fatalf("price for %q = %+v, want consistent CNY table", model, price)
		}
	}
}

func TestDeepSeekOfficialPricingCurrencyResolution(t *testing.T) {
	for _, tt := range []struct {
		name     string
		language string
		desktop  DesktopConfig
		want     string
	}{
		{name: "old config defaults to USD", want: "USD"},
		{name: "English auto", desktop: DesktopConfig{Language: "en"}, want: "USD"},
		{name: "Chinese auto", desktop: DesktopConfig{Language: "zh"}, want: "CNY"},
		{name: "CLI Chinese fallback", language: "zh", want: "CNY"},
		{name: "explicit USD wins over Chinese", language: "zh", desktop: DesktopConfig{Language: "zh", Currency: "USD"}, want: "USD"},
		{name: "explicit CNY wins over English", language: "en", desktop: DesktopConfig{Language: "en", Currency: "CNY"}, want: "CNY"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := &Config{Language: tt.language, Desktop: tt.desktop}
			if got := c.DeepSeekOfficialPricingCurrency(); got != tt.want {
				t.Fatalf("pricing currency = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLoadForRootKeepsPricingRegionUserGlobal(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("REASONIX_CREDENTIALS_STORE", "file")
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("[desktop]\nlanguage = \"en\"\ncurrency = \"USD\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "reasonix.toml"), []byte("[desktop]\nlanguage = \"zh\"\ncurrency = \"CNY\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadForRoot(project)
	if err != nil {
		t.Fatalf("LoadForRoot: %v", err)
	}
	if got := cfg.DesktopCurrency(); got != "USD" {
		t.Fatalf("project config overrode user pricing currency: got %q, want USD", got)
	}
	if got := cfg.DesktopLanguage(); got != "en" {
		t.Fatalf("project config overrode user desktop language: got %q, want en", got)
	}
}

func TestApplyDeepSeekOfficialDefaultPricingExplicitCurrencyWins(t *testing.T) {
	c := Default()
	c.Desktop.Language = "zh"
	c.Desktop.Currency = "USD"
	applyDeepSeekOfficialDefaultPricing(c)
	flash, _ := c.Provider("deepseek-flash")
	if flash.Price == nil || flash.Price.Output != 0.28 || flash.Price.Currency != "$" {
		t.Fatalf("flash price = %+v, want explicit USD preset", flash.Price)
	}

	c.Desktop.Language = "en"
	c.Desktop.Currency = "CNY"
	applyDeepSeekOfficialDefaultPricing(c)
	if flash.Price == nil || flash.Price.Output != 2 || flash.Price.Currency != "¥" {
		t.Fatalf("flash price = %+v, want explicit CNY preset", flash.Price)
	}
}

func TestResetOfficialProviderPricingOnUpgradeRunsOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reasonix.toml")
	c := &Config{
		ConfigVersion: 2,
		Providers: []ProviderEntry{
			{
				Name:    "deepseek",
				Kind:    "openai",
				BaseURL: "https://api.deepseek.com",
				Models:  []string{"deepseek-v4-flash", "deepseek-v4-pro"},
				Default: "deepseek-v4-flash",
				Price:   &provider.Pricing{CacheHit: 9, Input: 9, Output: 9, Currency: "$"},
				Prices: map[string]*provider.Pricing{
					"deepseek-v4-flash": {CacheHit: 8, Input: 8, Output: 8, Currency: "$"},
				},
			},
			{
				Name:    "mimo-api",
				Kind:    "openai",
				BaseURL: "https://api.xiaomimimo.com/v1",
				Models:  []string{"mimo-v2.5-pro", "mimo-v2.5", "mimo-v2-omni"},
				Default: "mimo-v2.5-pro",
				Price:   &provider.Pricing{CacheHit: 7, Input: 7, Output: 7, Currency: "$"},
			},
		},
	}
	if err := c.SaveTo(path); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}

	changed, err := ResetOfficialProviderPricingOnUpgrade(path)
	if err != nil {
		t.Fatalf("ResetOfficialProviderPricingOnUpgrade: %v", err)
	}
	if !changed {
		t.Fatal("upgrade reset did not run for config_version 2")
	}
	var got Config
	if _, err := toml.DecodeFile(path, &got); err != nil {
		t.Fatalf("decode migrated config: %v", err)
	}
	if got.ConfigVersion != Default().ConfigVersion {
		t.Fatalf("config_version = %d, want %d", got.ConfigVersion, Default().ConfigVersion)
	}
	deepseek, ok := got.Provider("deepseek")
	if !ok {
		t.Fatal("deepseek provider missing")
	}
	if deepseek.Price != nil {
		t.Fatalf("deepseek provider-wide price = %+v, want nil after reset", deepseek.Price)
	}
	if p := deepseek.Prices["deepseek-v4-flash"]; p == nil || p.Currency != "$" || p.Output != 0.28 {
		t.Fatalf("deepseek flash price = %+v, want USD default", p)
	}
	if p := deepseek.Prices["deepseek-v4-pro"]; p == nil || p.Currency != "$" || p.Output != 0.87 {
		t.Fatalf("deepseek pro price = %+v, want USD default", p)
	}
	mimo, ok := got.Provider("mimo-api")
	if !ok {
		t.Fatal("mimo-api provider missing")
	}
	if mimo.Price == nil || mimo.Price.Currency != "$" || mimo.Price.Output != 7 {
		t.Fatalf("mimo provider-wide price = %+v, want custom price preserved", mimo.Price)
	}

	deepseek.Prices["deepseek-v4-flash"] = &provider.Pricing{CacheHit: 4, Input: 4, Output: 4, Currency: "$"}
	if err := got.SaveTo(path); err != nil {
		t.Fatalf("SaveTo after custom edit: %v", err)
	}
	changed, err = ResetOfficialProviderPricingOnUpgrade(path)
	if err != nil {
		t.Fatalf("second ResetOfficialProviderPricingOnUpgrade: %v", err)
	}
	if changed {
		t.Fatal("upgrade reset ran again after config_version was updated")
	}
	got = Config{}
	if _, err := toml.DecodeFile(path, &got); err != nil {
		t.Fatalf("decode custom config: %v", err)
	}
	deepseek, _ = got.Provider("deepseek")
	if p := deepseek.Prices["deepseek-v4-flash"]; p == nil || p.Output != 4 || p.Currency != "$" {
		t.Fatalf("post-upgrade custom flash price = %+v, want preserved", p)
	}
}

func TestApplyUserConfigUpgradesOnStartupVersion3NonWindowsAdvancesToV5(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	setRuntimeGOOS(t, "darwin")

	c := &Config{
		ConfigVersion: 3,
		Providers: []ProviderEntry{{
			Name:    "deepseek",
			Kind:    "openai",
			BaseURL: "https://api.deepseek.com",
			Models:  []string{"deepseek-v4-flash"},
			Prices: map[string]*provider.Pricing{
				"deepseek-v4-flash": {CacheHit: 4, Input: 4, Output: 4, Currency: "$"},
			},
		}},
	}
	if err := c.SaveTo(path); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}

	changed, err := ApplyUserConfigUpgradesOnStartup(path)
	if err != nil {
		t.Fatalf("ApplyUserConfigUpgradesOnStartup: %v", err)
	}
	if !changed {
		t.Fatal("v3 config should advance through the retired Auto Plan migration")
	}
	var got Config
	if _, err := toml.DecodeFile(path, &got); err != nil {
		t.Fatalf("decode migrated config: %v", err)
	}
	if got.ConfigVersion != 5 {
		t.Fatalf("config_version = %d, want 5", got.ConfigVersion)
	}
	deepseek, _ := got.Provider("deepseek")
	if p := deepseek.Prices["deepseek-v4-flash"]; p == nil || p.Output != 4 || p.Currency != "$" {
		t.Fatalf("custom flash price = %+v, want preserved", p)
	}
}

func TestApplyUserConfigUpgradesOnStartupWindowsBashEnforceDefaultsOffOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	setRuntimeGOOS(t, "windows")

	c := Default()
	c.ConfigVersion = 3
	c.Sandbox.Bash = "enforce"
	if err := c.SaveTo(path); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}

	changed, err := ApplyUserConfigUpgradesOnStartup(path)
	if err != nil {
		t.Fatalf("ApplyUserConfigUpgradesOnStartup: %v", err)
	}
	if !changed {
		t.Fatal("upgrade should migrate Windows bash sandbox default")
	}
	got := LoadForEdit(path)
	if got.ConfigVersion != Default().ConfigVersion {
		t.Fatalf("config_version = %d, want %d", got.ConfigVersion, Default().ConfigVersion)
	}
	if got.Sandbox.Bash != "off" || got.BashMode() != "off" {
		t.Fatalf("Windows bash mode after migration = raw %q effective %q, want off/off", got.Sandbox.Bash, got.BashMode())
	}

	got.Sandbox.Bash = "enforce"
	if got.BashMode() != "off" {
		t.Fatalf("manual Windows enforce should still resolve off before save, got %q", got.BashMode())
	}
	if err := got.SaveTo(path); err != nil {
		t.Fatalf("SaveTo manual enforce: %v", err)
	}
	changed, err = ApplyUserConfigUpgradesOnStartup(path)
	if err != nil {
		t.Fatalf("second ApplyUserConfigUpgradesOnStartup: %v", err)
	}
	if changed {
		t.Fatal("v4 config should not be migrated again after user attempts to re-enable enforce")
	}
	got = LoadForEdit(path)
	if got.Sandbox.Bash != "off" || got.BashMode() != "off" {
		t.Fatalf("manual Windows enforce after save = raw %q effective %q, want off/off", got.Sandbox.Bash, got.BashMode())
	}
}

func TestApplyUserConfigUpgradesOnStartupWindowsBashOffOnlyMarksVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	setRuntimeGOOS(t, "windows")

	c := Default()
	c.ConfigVersion = 3
	c.Sandbox.Bash = "off"
	if err := c.SaveTo(path); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}

	changed, err := ApplyUserConfigUpgradesOnStartup(path)
	if err != nil {
		t.Fatalf("ApplyUserConfigUpgradesOnStartup: %v", err)
	}
	if !changed {
		t.Fatal("Windows v3 config should be marked as migrated")
	}
	got := LoadForEdit(path)
	if got.ConfigVersion != Default().ConfigVersion {
		t.Fatalf("config_version = %d, want %d", got.ConfigVersion, Default().ConfigVersion)
	}
	if got.Sandbox.Bash != "off" || got.BashMode() != "off" {
		t.Fatalf("Windows bash mode after marker migration = raw %q effective %q, want off/off", got.Sandbox.Bash, got.BashMode())
	}
}

func TestApplyUserConfigUpgradesOnStartupRetiresAutoPlan(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	setRuntimeGOOS(t, "darwin")
	original := `config_version = 4
default_model = "deepseek-flash"

[agent]
auto_plan = "on"
auto_plan_classifier = "deepseek-flash"
temperature = 0.4
`
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	changed, err := ApplyUserConfigUpgradesOnStartup(path)
	if err != nil {
		t.Fatalf("ApplyUserConfigUpgradesOnStartup: %v", err)
	}
	if !changed {
		t.Fatal("v4 auto-plan config should migrate to the manual-only experience")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "auto_plan") {
		t.Fatalf("retired auto-plan keys remain after migration:\n%s", raw)
	}
	got := LoadForEdit(path)
	if got.ConfigVersion != Default().ConfigVersion || got.Agent.AutoPlan != "off" || got.Agent.AutoPlanClassifier != "" {
		t.Fatalf("migrated config = version:%d auto:%q classifier:%q", got.ConfigVersion, got.Agent.AutoPlan, got.Agent.AutoPlanClassifier)
	}
	if got.Agent.Temperature != 0.4 {
		t.Fatalf("unrelated agent temperature = %v, want 0.4", got.Agent.Temperature)
	}

	again, err := ApplyUserConfigUpgradesOnStartup(path)
	if err != nil {
		t.Fatalf("second ApplyUserConfigUpgradesOnStartup: %v", err)
	}
	if again {
		t.Fatal("v5 config should not migrate again")
	}
}

func TestResolveModelUsesPerModelPricing(t *testing.T) {
	c := &Config{Providers: []ProviderEntry{{
		Name:    "deepseek",
		Kind:    "openai",
		BaseURL: "https://api.deepseek.com",
		Models:  []string{"deepseek-v4-flash", "deepseek-v4-pro"},
		Default: "deepseek-v4-flash",
		Price:   &provider.Pricing{CacheHit: 9, Input: 9, Output: 9, Currency: "$"},
		Prices: map[string]*provider.Pricing{
			"deepseek-v4-flash": &provider.Pricing{CacheHit: 0.02, Input: 1, Output: 2, Currency: "¥"},
			"deepseek-v4-pro":   &provider.Pricing{CacheHit: 0.025, Input: 3, Output: 6, Currency: "¥"},
		},
	}}}
	pro, ok := c.ResolveModel("deepseek/deepseek-v4-pro")
	if !ok {
		t.Fatal("deepseek pro did not resolve")
	}
	if pro.Price == nil || pro.Price.Output != 6 {
		t.Fatalf("pro price = %+v, want model-specific pro price", pro.Price)
	}
	flash, ok := c.ResolveModel("deepseek")
	if !ok {
		t.Fatal("deepseek default did not resolve")
	}
	if flash.Price == nil || flash.Price.Output != 2 {
		t.Fatalf("flash price = %+v, want model-specific flash price", flash.Price)
	}
}

func TestNormalizeLegacyMimoProviderCatalogsBackfillsOfficialMimoAPIStub(t *testing.T) {
	c := &Config{
		DefaultModel: "mimo-api/mimo-v2.5-pro",
		Desktop:      DesktopConfig{ProviderAccess: []string{"mimo-api"}},
		Providers: []ProviderEntry{{
			Name:      "mimo-api",
			Kind:      "openai",
			BaseURL:   "https://api.xiaomimimo.com/v1",
			Model:     "mimo-v2.5-pro",
			APIKeyEnv: "MIMO_API_KEY",
		}},
	}

	normalizeDesktopOfficialProviderAccess(c)

	p, ok := c.Provider("mimo-api")
	if !ok {
		t.Fatal("mimo-api provider missing")
	}
	wantModels := []string{"mimo-v2.5-pro", "mimo-v2.5", "mimo-v2-omni"}
	if !reflect.DeepEqual(p.ModelList(), wantModels) {
		t.Fatalf("mimo-api models = %v, want %v", p.ModelList(), wantModels)
	}
	if p.Default != "mimo-v2.5-pro" {
		t.Fatalf("mimo-api default = %q, want mimo-v2.5-pro", p.Default)
	}
	if !reflect.DeepEqual(p.VisionModels, []string{"mimo-v2.5", "mimo-v2-omni"}) {
		t.Fatalf("mimo-api vision_models = %v, want vision-capable MiMo models", p.VisionModels)
	}
}

func TestNormalizeDesktopOfficialProviderAccessDoesNotBackfillCustomNamedMimoAPI(t *testing.T) {
	c := &Config{
		Desktop: DesktopConfig{ProviderAccess: []string{"mimo-api"}},
		Providers: []ProviderEntry{{
			Name:    "mimo-api",
			Kind:    "openai",
			BaseURL: "https://proxy.example.com/v1",
			Model:   "mimo-v2.5-pro",
		}},
	}

	normalizeDesktopOfficialProviderAccess(c)

	p, ok := c.Provider("mimo-api")
	if !ok {
		t.Fatal("mimo-api provider missing")
	}
	if p.HasModel("mimo-v2.5") || p.HasModel("mimo-v2-omni") {
		t.Fatalf("custom mimo-api models = %v, want original custom list", p.ModelList())
	}
}

func TestNormalizeLegacyMimoProviderCatalogsBackfillsOfficialMimoTokenPlanStub(t *testing.T) {
	c := &Config{
		Desktop: DesktopConfig{ProviderAccess: []string{"mimo-token-plan"}},
		Providers: []ProviderEntry{{
			Name:      "mimo-token-plan",
			Kind:      "openai",
			BaseURL:   "https://token-plan-cn.xiaomimimo.com/v1",
			Model:     "mimo-v2.5-pro",
			APIKeyEnv: "MIMO_API_KEY",
			Price:     &provider.Pricing{CacheHit: 0.025, Input: 3, Output: 6, Currency: "CNY"},
		}},
	}

	normalizeDesktopOfficialProviderAccess(c)

	p, ok := c.Provider("mimo-token-plan")
	if !ok {
		t.Fatal("mimo-token-plan provider missing")
	}
	if !reflect.DeepEqual(p.ModelList(), []string{"mimo-v2.5-pro", "mimo-v2.5"}) {
		t.Fatalf("mimo-token-plan models = %v, want token plan catalog", p.ModelList())
	}
	if p.Price == nil || p.Price.Currency != "CNY" {
		t.Fatalf("mimo-token-plan price = %+v, want preserved custom provider-wide price", p.Price)
	}
	if !reflect.DeepEqual(p.VisionModels, []string{"mimo-v2.5"}) {
		t.Fatalf("mimo-token-plan vision_models = %v, want token plan vision metadata", p.VisionModels)
	}
}

// ── Explicit model list: normalization must not override user selection ───────

func TestBackfillDeepSeekProSkipsWhenExplicitModelList(t *testing.T) {
	// User saved with only flash via Settings → Models = ["deepseek-v4-flash"].
	c := &Config{Providers: []ProviderEntry{
		{Name: "deepseek", Kind: "openai", BaseURL: "https://api.deepseek.com", Models: []string{"deepseek-v4-flash"}, Default: "deepseek-v4-flash", APIKeyEnv: "DEEPSEEK_API_KEY"},
	}}
	backfillDeepSeekPro(c)
	if hasModel(c, "deepseek-v4-pro") != nil {
		t.Fatal("deepseek-v4-pro must not be added when user has an explicit model list")
	}
	if len(c.Providers) != 1 {
		t.Fatalf("providers = %d, want 1 (no new entry should be added)", len(c.Providers))
	}
}

func TestEnsureProviderModelsSkipsWhenExplicitModelList(t *testing.T) {
	// User saved with only flash via Settings → Models = ["deepseek-v4-flash"].
	p := &ProviderEntry{
		Name:    "deepseek",
		BaseURL: "https://api.deepseek.com",
		Models:  []string{"deepseek-v4-flash"},
		Default: "deepseek-v4-flash",
	}
	ensureProviderModels(p, []string{"deepseek-v4-flash", "deepseek-v4-pro"}, "deepseek-v4-flash")
	if p.HasModel("deepseek-v4-pro") {
		t.Fatal("ensureProviderModels must not merge required models when Models is explicitly set")
	}
	if len(p.Models) != 1 || p.Models[0] != "deepseek-v4-flash" {
		t.Fatalf("models = %v, want [deepseek-v4-flash]", p.Models)
	}
}

func TestMergeCuratedModelsIntoProviderSkipsWhenExplicitModelList(t *testing.T) {
	// User saved with only two mimo models via Settings.
	p := &ProviderEntry{
		Name:    "mimo-api",
		BaseURL: "https://api.xiaomimimo.com/v1",
		Models:  []string{"mimo-v2.5-pro", "mimo-v2.5"},
		Default: "mimo-v2.5-pro",
	}
	mergeCuratedModelsIntoProvider(p, []string{"mimo-v2.5-pro", "mimo-v2.5", "mimo-v2-omni"}, "mimo-v2.5-pro")
	if p.HasModel("mimo-v2-omni") {
		t.Fatal("mergeCuratedModelsIntoProvider must not add mimo-v2-omni when Models is explicitly set")
	}
	if len(p.Models) != 2 {
		t.Fatalf("models = %v, want 2 selected models", p.Models)
	}
}

func TestNormalizeOfficialMimoVisionModelsSkipsExplicitModelList(t *testing.T) {
	// User saved with only pro via Settings → Models = ["mimo-v2.5-pro"].
	c := &Config{
		Desktop: DesktopConfig{ProviderAccess: []string{"mimo-api"}},
		Providers: []ProviderEntry{{
			Name:      "mimo-api",
			Kind:      "openai",
			BaseURL:   "https://api.xiaomimimo.com/v1",
			Models:    []string{"mimo-v2.5-pro"},
			Default:   "mimo-v2.5-pro",
			APIKeyEnv: "MIMO_API_KEY",
		}},
	}
	normalizeDesktopOfficialProviderAccess(c)
	p, ok := c.Provider("mimo-api")
	if !ok {
		t.Fatal("mimo-api provider missing")
	}
	if p.HasModel("mimo-v2.5") || p.HasModel("mimo-v2-omni") {
		t.Fatalf("mimo-api models = %v, want only explicitly selected pro model", p.ModelList())
	}
	if len(p.VisionModels) != 0 {
		t.Fatalf("mimo-api vision_models = %v, want empty for pro-only explicit model list", p.VisionModels)
	}
}

func TestNormalizeOfficialMimoVisionModelsPreservesExplicitEmptyList(t *testing.T) {
	c := &Config{
		Desktop: DesktopConfig{ProviderAccess: []string{"mimo-api"}},
		Providers: []ProviderEntry{{
			Name:         "mimo-api",
			Kind:         "openai",
			BaseURL:      "https://api.xiaomimimo.com/v1",
			Models:       []string{"mimo-v2.5-pro", "mimo-v2.5", "mimo-v2-omni"},
			Default:      "mimo-v2.5-pro",
			APIKeyEnv:    "MIMO_API_KEY",
			VisionModels: []string{},
		}},
	}
	normalizeDesktopOfficialProviderAccess(c)
	p, ok := c.Provider("mimo-api")
	if !ok {
		t.Fatal("mimo-api provider missing")
	}
	if p.VisionModels == nil || len(p.VisionModels) != 0 {
		t.Fatalf("mimo-api vision_models = %#v, want explicit empty list", p.VisionModels)
	}
}

func TestNormalizeOfficialDeepSeekModelsSkipsExplicitModelList(t *testing.T) {
	// User saved with only flash via Settings → Models = ["deepseek-v4-flash"].
	c := &Config{Providers: []ProviderEntry{{
		Name:      "deepseek",
		Kind:      "openai",
		BaseURL:   "https://api.deepseek.com",
		Models:    []string{"deepseek-v4-flash"},
		Default:   "deepseek-v4-flash",
		APIKeyEnv: "DEEPSEEK_API_KEY",
	}}}
	normalizeOfficialDeepSeekModels(c)
	p, ok := c.Provider("deepseek")
	if !ok {
		t.Fatal("deepseek provider missing")
	}
	if p.HasModel("deepseek-v4-pro") {
		t.Fatal("normalizeOfficialDeepSeekModels must not add pro when Models is explicitly set")
	}
}
