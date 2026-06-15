package config

import (
	"testing"

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
		t.Errorf("pro price not the preset: %+v", pro.Price)
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
	if p.Prices["deepseek-v4-flash"].Output != 0.28 || p.Prices["deepseek-v4-pro"].Output != 0.87 {
		t.Fatalf("deepseek prices = %+v, want current V4 flash/pro prices", p.Prices)
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
			"deepseek-v4-flash": &provider.Pricing{CacheHit: 0.0028, Input: 0.14, Output: 0.28, Currency: "$"},
			"deepseek-v4-pro":   &provider.Pricing{CacheHit: 0.003625, Input: 0.435, Output: 0.87, Currency: "$"},
		},
	}}}
	pro, ok := c.ResolveModel("deepseek/deepseek-v4-pro")
	if !ok {
		t.Fatal("deepseek pro did not resolve")
	}
	if pro.Price == nil || pro.Price.Output != 0.87 {
		t.Fatalf("pro price = %+v, want model-specific pro price", pro.Price)
	}
	flash, ok := c.ResolveModel("deepseek")
	if !ok {
		t.Fatal("deepseek default did not resolve")
	}
	if flash.Price == nil || flash.Price.Output != 0.28 {
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
}
