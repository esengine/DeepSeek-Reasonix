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

func TestNormalizeDesktopOfficialProviderAccessCanonicalizesLegacyIDs(t *testing.T) {
	c := Default()
	c.DefaultModel = "deepseek-flash/deepseek-v4-pro"
	c.Desktop.ProviderAccess = []string{"deepseek-flash", "mimo-pro"}
	normalizeDesktopOfficialProviderAccess(c)
	if len(c.Desktop.ProviderAccess) != 2 || c.Desktop.ProviderAccess[0] != "deepseek" || c.Desktop.ProviderAccess[1] != "mimo-token-plan" {
		t.Fatalf("provider_access = %+v, want canonical official ids", c.Desktop.ProviderAccess)
	}
	if c.DefaultModel != "deepseek/deepseek-v4-pro" {
		t.Fatalf("default_model = %q, want deepseek/deepseek-v4-pro", c.DefaultModel)
	}
	if _, ok := c.Provider("deepseek"); !ok {
		t.Fatal("canonical deepseek provider missing")
	}
	if _, ok := c.Provider("mimo-token-plan"); !ok {
		t.Fatal("canonical mimo-token-plan provider missing")
	}
	if p, _ := c.Provider("mimo-token-plan"); p.Price != nil {
		t.Fatalf("mimo-token-plan mixed-model price = %+v, want nil", p.Price)
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

func TestNormalizeDesktopOfficialProviderAccessEnsuresMimoAPI(t *testing.T) {
	c := Default()
	c.DefaultModel = "mimo-api/mimo-v2.5-pro"
	c.Desktop.ProviderAccess = []string{"mimo-api"}
	normalizeDesktopOfficialProviderAccess(c)
	p, ok := c.Provider("mimo-api")
	if !ok {
		t.Fatal("mimo-api paid provider missing")
	}
	if !p.HasModel("mimo-v2.5") || !p.HasModel("mimo-v2-omni") {
		t.Fatalf("mimo-api models = %v, want vision-capable MiMo models", p.ModelList())
	}
	if got := c.Desktop.ProviderAccess; len(got) != 1 || got[0] != "mimo-api" {
		t.Fatalf("provider_access = %+v, want mimo-api", got)
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
	if mimo.Price != nil {
		t.Fatalf("mimo provider-wide price = %+v, want nil after reset", mimo.Price)
	}
	if p := mimo.Prices["mimo-v2.5-pro"]; p == nil || p.Currency != "¥" || p.Output != 6 {
		t.Fatalf("mimo pro price = %+v, want RMB default", p)
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

func TestNormalizeDesktopOfficialProviderAccessBackfillsExistingMimoAPIModels(t *testing.T) {
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
	if !p.HasModel("mimo-v2.5-pro") || !p.HasModel("mimo-v2.5") || !p.HasModel("mimo-v2-omni") {
		t.Fatalf("mimo-api models = %v, want curated MiMo API models", p.ModelList())
	}
	if p.Default != "mimo-v2.5-pro" {
		t.Fatalf("mimo-api default = %q, want mimo-v2.5-pro", p.Default)
	}
	if want := []string{"mimo-v2.5", "mimo-v2-omni"}; !reflect.DeepEqual(p.VisionModels, want) {
		t.Fatalf("mimo-api vision_models = %v, want %v", p.VisionModels, want)
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

func TestNormalizeDesktopOfficialProviderAccessBackfillsExistingMimoTokenPlanAndClearsPrice(t *testing.T) {
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
	if !p.HasModel("mimo-v2.5-pro") || !p.HasModel("mimo-v2.5") {
		t.Fatalf("mimo-token-plan models = %v, want pro and flash models", p.ModelList())
	}
	if p.Price != nil {
		t.Fatalf("mimo-token-plan mixed-model price = %+v, want nil", p.Price)
	}
	if p.Prices["mimo-v2.5-pro"] == nil || p.Prices["mimo-v2.5-pro"].Currency != "¥" || p.Prices["mimo-v2.5-pro"].Output != 6 {
		t.Fatalf("mimo-v2.5-pro price = %+v, want RMB domestic pricing", p.Prices["mimo-v2.5-pro"])
	}
	if p.Prices["mimo-v2.5"] == nil || p.Prices["mimo-v2.5"].Currency != "¥" || p.Prices["mimo-v2.5"].Output != 2 {
		t.Fatalf("mimo-v2.5 price = %+v, want RMB domestic pricing", p.Prices["mimo-v2.5"])
	}
	if want := []string{"mimo-v2.5"}; !reflect.DeepEqual(p.VisionModels, want) {
		t.Fatalf("mimo-token-plan vision_models = %v, want %v", p.VisionModels, want)
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
