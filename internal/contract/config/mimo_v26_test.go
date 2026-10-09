package config

import (
	"reflect"
	"testing"

	"reasonix/internal/contract/provider"
)

var mimoPresetIDs = []string{
	"mimo-api", "mimo-anthropic",
	"mimo-token-plan-cn", "mimo-token-plan-cn-anthropic",
	"mimo-token-plan-sgp", "mimo-token-plan-sgp-anthropic",
	"mimo-token-plan-ams", "mimo-token-plan-ams-anthropic",
}

func mimoPresetEntry(t *testing.T, id string) ProviderEntry {
	t.Helper()
	preset, ok := CuratedProviderPreset(id)
	if !ok || len(preset.Entries) != 1 {
		t.Fatalf("preset %s = %+v, %v", id, preset, ok)
	}
	return preset.Entries[0]
}

func legacyMimoShape(t *testing.T, id string) ProviderEntry {
	t.Helper()
	e := mimoPresetEntry(t, id)
	e.Models = []string{"mimo-v2.5-pro", "mimo-v2.5"}
	e.VisionModels = []string{"mimo-v2.5"}
	e.Default = "mimo-v2.5-pro"
	e.ContextWindow = legacyMimoWindow
	e.Prices = mimoDomesticPrices(e.Models)
	return e
}

func TestMimoPresetsLeadWithV26AndDeclareOneWindow(t *testing.T) {
	for _, id := range mimoPresetIDs {
		e := mimoPresetEntry(t, id)
		if e.Default != "mimo-v2.6-pro" || e.Models[0] != "mimo-v2.6-pro" {
			t.Fatalf("%s default/first = %q/%q, want mimo-v2.6-pro", id, e.Default, e.Models[0])
		}
		for _, m := range []string{"mimo-v2.6-flash", "mimo-v2.5-pro", "mimo-v2.5"} {
			if !e.HasModel(m) {
				t.Fatalf("%s models = %v, missing %s", id, e.Models, m)
			}
		}
		if e.ContextWindow != 1_000_000 {
			t.Fatalf("%s window = %d, want 1000000", id, e.ContextWindow)
		}
		for _, m := range []string{"mimo-v2.6-pro", "mimo-v2.6-flash", "mimo-v2.6-pro-ultraspeed", "mimo-v2.5-pro"} {
			if e.HasVisionModel(m) {
				t.Fatalf("%s marks %s image-taking; the docs do not say so", id, m)
			}
		}
		for _, m := range e.Models {
			if e.Prices[m] == nil {
				t.Fatalf("%s has no price for %s", id, m)
			}
		}
		if got := e.HasModel("mimo-v2.6-pro-ultraspeed"); got != (id == "mimo-api" || id == "mimo-anthropic") {
			t.Fatalf("%s lists ultraspeed = %v; only the pay-as-you-go endpoints serve it", id, got)
		}
	}
}

func TestMimoV26PricesAreTheVendorsPage(t *testing.T) {
	cases := map[string][3]float64{
		"mimo-v2.6-pro":            {0.025, 3, 6},
		"mimo-v2.6-flash":          {0.02, 1, 2},
		"mimo-v2.6-pro-ultraspeed": {0.25, 30, 60},
	}
	e := mimoPresetEntry(t, "mimo-api")
	for model, want := range cases {
		p := e.PriceForModel(model)
		if p == nil || p.CacheHit != want[0] || p.Input != want[1] || p.Output != want[2] {
			t.Fatalf("%s price = %+v, want %v", model, p, want)
		}
	}
}

func TestShippedMimoPresetUpgradeMovesOnlyUntouchedShape(t *testing.T) {
	for _, id := range mimoPresetIDs {
		canonical := mimoPresetEntry(t, id)
		held := legacyMimoShape(t, id)
		held.PresetID = id
		held.Prices = map[string]*provider.Pricing{"mimo-v2.5-pro": {CacheHit: 9, Input: 9, Output: 9, Currency: "¥"}}
		c := &Config{Providers: []ProviderEntry{held}}
		if !upgradeShippedPresets(c) {
			t.Fatalf("%s: legacy shape was not upgraded", id)
		}
		got := c.Providers[0]
		if !reflect.DeepEqual(got.Models, canonical.Models) || got.Default != "mimo-v2.6-pro" {
			t.Fatalf("%s: models/default = %v/%q, want %v/mimo-v2.6-pro", id, got.Models, got.Default, canonical.Models)
		}
		if !reflect.DeepEqual(got.VisionModels, []string{"mimo-v2.5"}) || got.ContextWindow != 1_000_000 {
			t.Fatalf("%s: vision/window = %v/%d, want [mimo-v2.5]/1000000", id, got.VisionModels, got.ContextWindow)
		}
		if got.Prices["mimo-v2.5-pro"].Input != 9 {
			t.Fatalf("%s: a price the user wrote was overwritten", id)
		}
		if p := got.Prices["mimo-v2.6-pro"]; p == nil || p.Input != 3 {
			t.Fatalf("%s: added model was not priced: %+v", id, p)
		}

		curated := map[string]func(*ProviderEntry){
			"window":       func(e *ProviderEntry) { e.ContextWindow = 256_000 },
			"default":      func(e *ProviderEntry) { e.Default = "mimo-v2.5" },
			"extra model":  func(e *ProviderEntry) { e.Models = append(e.Models, "mimo-custom") },
			"fewer models": func(e *ProviderEntry) { e.Models = []string{"mimo-v2.5-pro"} },
		}
		for name, mutate := range curated {
			e := legacyMimoShape(t, id)
			e.PresetID = id
			mutate(&e)
			before := e
			before.Models = append([]string(nil), e.Models...)
			c := &Config{Providers: []ProviderEntry{e}}
			upgradeShippedPresets(c)
			after := c.Providers[0]
			if !reflect.DeepEqual(after.Models, before.Models) || after.Default != before.Default ||
				after.ContextWindow != before.ContextWindow || !reflect.DeepEqual(after.VisionModels, before.VisionModels) {
				t.Fatalf("%s/%s: user-curated entry was changed: %+v", id, name, after)
			}
		}
	}
}

func TestShippedMimoPresetUpgradeKeepsExplicitEmptyVisionList(t *testing.T) {
	e := legacyMimoShape(t, "mimo-api")
	e.PresetID = "mimo-api"
	e.VisionModels = []string{}
	c := &Config{Providers: []ProviderEntry{e}}
	upgradeShippedPresets(c)
	if c.Providers[0].VisionModels == nil || len(c.Providers[0].VisionModels) != 0 {
		t.Fatalf("vision_models = %v, want the explicit empty list untouched", c.Providers[0].VisionModels)
	}
}

func TestLegacyMimoCatalogsDefaultToV26ButKeepAStatedModel(t *testing.T) {
	c := &Config{Providers: []ProviderEntry{
		{Name: "mimo-api", Kind: "openai", BaseURL: "https://api.xiaomimimo.com/v1"},
		{Name: "mimo-token-plan", Kind: "openai", BaseURL: "https://token-plan-cn.xiaomimimo.com/v1", Model: "mimo-v2.5-pro"},
	}}
	if !normalizeLegacyMimoProviderCatalogs(c) {
		t.Fatal("no change reported")
	}
	if p := c.Providers[0]; p.Default != "mimo-v2.6-pro" || p.Model != "mimo-v2.6-pro" {
		t.Fatalf("empty legacy entry = %q/%q, want mimo-v2.6-pro", p.Default, p.Model)
	}
	if p := c.Providers[1]; p.Default != "mimo-v2.5-pro" || p.Model != "mimo-v2.5-pro" || !p.HasModel("mimo-v2.6-flash") {
		t.Fatalf("legacy entry naming v2.5-pro = %q/%q %v, want its choice kept and v2.6 added", p.Default, p.Model, p.Models)
	}
}
