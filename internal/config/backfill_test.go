package config

import (
	"path/filepath"
	"reflect"
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
	} else if pro.Price == nil || pro.Price.Output != 6 || pro.Price.Currency != "¥" {
		t.Errorf("pro price not the preset: %+v", pro.Price)
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

func TestLoadForEditPersistsLegacyStepFunBaseURLMigration(t *testing.T) {
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
		t.Fatalf("decode persisted config: %v", err)
	}
	if got, _ := disk.Provider("stepfun"); got == nil || got.BaseURL != officialStepFunOpenAIBaseURL {
		t.Fatalf("persisted stepfun = %+v, want official base URL", got)
	}
	if got, _ := disk.Provider("stepfun-anthropic"); got == nil || got.BaseURL != officialStepFunAnthropicBaseURL {
		t.Fatalf("persisted stepfun-anthropic = %+v, want official base URL", got)
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
	if p.Prices["deepseek-v4-flash"].Output != 2 || p.Prices["deepseek-v4-pro"].Output != 6 {
		t.Fatalf("deepseek prices = %+v, want current V4 flash/pro prices", p.Prices)
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
	if p := deepseek.Prices["deepseek-v4-flash"]; p == nil || p.Currency != "¥" || p.Output != 2 {
		t.Fatalf("deepseek flash price = %+v, want RMB default", p)
	}
	if p := deepseek.Prices["deepseek-v4-pro"]; p == nil || p.Currency != "¥" || p.Output != 6 {
		t.Fatalf("deepseek pro price = %+v, want RMB default", p)
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

func TestApplyUserConfigUpgradesOnStartupVersion3NonWindowsNoop(t *testing.T) {
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
	if changed {
		t.Fatal("v3 non-Windows config should not be rewritten by the Windows bash sandbox migration")
	}
	var got Config
	if _, err := toml.DecodeFile(path, &got); err != nil {
		t.Fatalf("decode migrated config: %v", err)
	}
	if got.ConfigVersion != 3 {
		t.Fatalf("config_version = %d, want 3", got.ConfigVersion)
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
