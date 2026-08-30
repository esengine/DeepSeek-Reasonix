package config

import (
	"testing"

	"reasonix/internal/provider/openai"
)

func TestEffectiveVisionEnablesPinnedOfficialDeepSeekVisionSKU(t *testing.T) {
	sku := &ProviderEntry{
		Name:    "deepseek",
		Kind:    "openai",
		BaseURL: "https://api.deepseek.com",
		Model:   openai.OfficialDeepSeekVisionModel,
	}
	if !CanConfigureVision(sku) {
		t.Fatal("official DeepSeek Settings must expose per-model image-input checkboxes")
	}
	if !EffectiveVision(sku) || !ExplicitModelVision(sku) {
		t.Fatal("selecting the pinned official DeepSeek vision SKU must enable image input")
	}
}

func TestEffectiveVisionHonorsOfficialDeepSeekVisionModels(t *testing.T) {
	sku := &ProviderEntry{
		Name:         "deepseek",
		Kind:         "openai",
		BaseURL:      "https://api.deepseek.com",
		Model:        openai.OfficialDeepSeekVisionModel,
		VisionModels: []string{openai.OfficialDeepSeekVisionModel},
	}
	if !EffectiveVision(sku) || !ExplicitModelVision(sku) {
		t.Fatal("checking image input on the official vision SKU must enable image input")
	}

	sku.VisionModels = []string{}
	if EffectiveVision(sku) || ExplicitModelVision(sku) {
		t.Fatal("unchecking image input must disable image input on the official vision SKU")
	}

	flash := &ProviderEntry{
		Name:         "deepseek",
		Kind:         "openai",
		BaseURL:      "https://api.deepseek.com",
		Model:        "deepseek-v4-flash",
		VisionModels: []string{"deepseek-v4-flash", openai.OfficialDeepSeekVisionModel},
	}
	if EffectiveVision(flash) || ExplicitModelVision(flash) {
		t.Fatal("checking image input on Flash must not enable official DeepSeek image payloads")
	}
}

func TestNormalizeOfficialDeepSeekModelsBackfillsVisionSKUOnStockCatalog(t *testing.T) {
	c := &Config{Providers: []ProviderEntry{{
		Name:      "deepseek",
		Kind:      "anthropic",
		BaseURL:   "https://api.deepseek.com/anthropic",
		Models:    []string{"deepseek-v4-flash", "deepseek-v4-pro"},
		Default:   "deepseek-v4-flash",
		APIKeyEnv: "DEEPSEEK_API_KEY",
		Prices:    DeepSeekV4PricesForCurrency("USD"),
	}}}
	normalizeOfficialDeepSeekModels(c)
	p, ok := c.Provider("deepseek")
	if !ok {
		t.Fatal("deepseek provider missing")
	}
	if !p.HasModel(openai.OfficialDeepSeekVisionModel) {
		t.Fatalf("stock official catalog = %v, want pinned vision SKU", p.ModelList())
	}
	if p.Default != "deepseek-v4-flash" {
		t.Fatalf("default = %q, want flash after vision backfill", p.Default)
	}
	if !p.HasVisionModel(openai.OfficialDeepSeekVisionModel) {
		t.Fatalf("vision_models = %v, want pinned vision SKU", p.VisionModels)
	}
	flash := p.Prices["deepseek-v4-flash"]
	got := p.Prices[openai.OfficialDeepSeekVisionModel]
	if flash == nil || got == nil || got.CacheHit != flash.CacheHit || got.Input != flash.Input || got.Output != flash.Output || got.Currency != flash.Currency {
		t.Fatalf("vision SKU price = %+v, want Flash table %+v", got, flash)
	}
}

func TestNormalizeOfficialDeepSeekModelsBackfillsVisionOnFlashProvider(t *testing.T) {
	c := &Config{Providers: []ProviderEntry{{
		Name:      "deepseek-flash",
		Kind:      "openai",
		BaseURL:   "https://api.deepseek.com",
		Models:    []string{"deepseek-v4-flash"},
		Default:   "deepseek-v4-flash",
		APIKeyEnv: "DEEPSEEK_API_KEY",
	}}}
	normalizeOfficialDeepSeekModels(c)
	p, ok := c.Provider("deepseek-flash")
	if !ok {
		t.Fatal("deepseek-flash provider missing")
	}
	if !p.HasModel("deepseek-v4-flash") || !p.HasModel(openai.OfficialDeepSeekVisionModel) || p.HasModel("deepseek-v4-pro") {
		t.Fatalf("deepseek-flash models = %v, want flash + vision SKU", p.ModelList())
	}
}

func TestNormalizeOfficialDeepSeekModelsSkipsVisionOnProProvider(t *testing.T) {
	c := &Config{Providers: []ProviderEntry{{
		Name:      "deepseek-pro",
		Kind:      "openai",
		BaseURL:   "https://api.deepseek.com",
		Models:    []string{"deepseek-v4-pro"},
		Default:   "deepseek-v4-pro",
		APIKeyEnv: "DEEPSEEK_API_KEY",
	}}}
	normalizeOfficialDeepSeekModels(c)
	p, ok := c.Provider("deepseek-pro")
	if !ok {
		t.Fatal("deepseek-pro provider missing")
	}
	if p.HasModel(openai.OfficialDeepSeekVisionModel) {
		t.Fatalf("deepseek-pro models = %v, want pro only", p.ModelList())
	}
}

func TestNormalizeOfficialDeepSeekModelsPreservesExplicitEmptyVisionModels(t *testing.T) {
	c := &Config{Providers: []ProviderEntry{{
		Name:         "deepseek",
		Kind:         "openai",
		BaseURL:      "https://api.deepseek.com",
		Models:       []string{"deepseek-v4-flash", "deepseek-v4-pro"},
		Default:      "deepseek-v4-flash",
		VisionModels: []string{},
		APIKeyEnv:    "DEEPSEEK_API_KEY",
	}}}
	normalizeOfficialDeepSeekModels(c)
	p, ok := c.Provider("deepseek")
	if !ok {
		t.Fatal("deepseek provider missing")
	}
	if p.HasModel(openai.OfficialDeepSeekVisionModel) {
		t.Fatal("explicit empty vision_models must not re-add the vision SKU to a Flash/Pro catalog")
	}
	if p.VisionModels == nil || len(p.VisionModels) != 0 {
		t.Fatalf("vision_models = %#v, want preserved explicit empty list", p.VisionModels)
	}
}

func TestDeepSeekV4PricesIncludeVisionSKU(t *testing.T) {
	for _, currency := range []string{"CNY", "USD"} {
		prices := DeepSeekV4PricesForCurrency(currency)
		flash := prices["deepseek-v4-flash"]
		got := prices[openai.OfficialDeepSeekVisionModel]
		if flash == nil || got == nil || got.CacheHit != flash.CacheHit || got.Input != flash.Input || got.Output != flash.Output || got.Currency != flash.Currency {
			t.Fatalf("%s vision SKU price = %+v, want Flash table %+v", currency, got, flash)
		}
	}
}

func TestDeepSeekOfficialPresetsRouteVisionToPinnedSKU(t *testing.T) {
	for _, id := range []string{"deepseek-anthropic", "deepseek-responses"} {
		preset, ok := CuratedProviderPreset(id)
		if !ok || len(preset.Entries) != 1 {
			t.Fatalf("%s preset = %+v found=%v", id, preset, ok)
		}
		entry := preset.Entries[0]
		if !entry.HasModel(openai.OfficialDeepSeekVisionModel) || entry.Default != "deepseek-v4-flash" || entry.Vision {
			t.Fatalf("%s entry = %+v", id, entry)
		}
		var cfg Config
		if err := cfg.UpsertProvider(entry); err != nil {
			t.Fatalf("UpsertProvider(%s): %v", id, err)
		}
		flash, _ := cfg.ResolveModel(entry.Name + "/deepseek-v4-flash")
		pro, _ := cfg.ResolveModel(entry.Name + "/deepseek-v4-pro")
		vision, ok := cfg.ResolveModel(entry.Name + "/" + openai.OfficialDeepSeekVisionModel)
		if flash == nil || pro == nil || !ok {
			t.Fatalf("%s models did not resolve", id)
		}
		if EffectiveVision(flash) || EffectiveVision(pro) || !EffectiveVision(vision) {
			t.Fatalf("%s vision routing = flash:%t pro:%t vision:%t", id, EffectiveVision(flash), EffectiveVision(pro), EffectiveVision(vision))
		}
	}
}
