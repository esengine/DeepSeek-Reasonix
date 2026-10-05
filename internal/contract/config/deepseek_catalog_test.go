package config

import (
	"slices"
	"testing"

	"reasonix/internal/contract/provider"
)

// An installed user's model list is frozen in their config file. The vendor
// retired the names in it into one model that reads images, and nothing reaches
// that user unless the load puts it there — while a relay serving the same
// names is a different endpoint making its own promises.
func TestRetiredDeepSeekFlashModelsMigrate(t *testing.T) {
	cases := []struct {
		name   string
		entry  ProviderEntry
		want   bool
		models []string
		vision []string
	}{
		{
			name: "the list we shipped points at the model that serves it",
			entry: ProviderEntry{
				Name: "deepseek", Kind: "openai", BaseURL: "https://api.deepseek.com",
				Models: []string{"deepseek-v4-flash", "deepseek-v4-pro"},
			},
			want:   true,
			models: []string{DeepSeekFlashModel, deepSeekProModel},
		},
		{
			name: "the experimental vision name folds into the same model",
			entry: ProviderEntry{
				Name: "deepseek-vision", Kind: "anthropic", BaseURL: "https://api.deepseek.com",
				Models: []string{"deepseek-v4-flash-vision-exp"},
			},
			want:   true,
			models: []string{DeepSeekFlashModel},
		},
		{
			name: "an entry carrying both retired names lists the survivor once",
			entry: ProviderEntry{
				Name: "deepseek", Kind: "openai", BaseURL: "https://api.deepseek.com",
				Models: []string{"deepseek-v4-flash", "deepseek-v4-flash-vision-exp", "deepseek-v4-pro"},
			},
			want:   true,
			models: []string{DeepSeekFlashModel, deepSeekProModel},
		},
		{
			name: "a curated list keeps its curation, under the new name",
			entry: ProviderEntry{
				Name: "deepseek", Kind: "openai", BaseURL: "https://api.deepseek.com",
				Models: []string{"deepseek-v4-flash"},
			},
			want:   true,
			models: []string{DeepSeekFlashModel},
		},
		{
			name: "a relay serving the same names is not this endpoint",
			entry: ProviderEntry{
				Name: "relay", Kind: "openai", BaseURL: "https://relay.example.com/v1",
				Models: []string{"deepseek-v4-flash", "deepseek-v4-pro"},
			},
			models: []string{"deepseek-v4-flash", "deepseek-v4-pro"},
			vision: []string{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			entry := tc.entry
			if got := migrateRetiredDeepSeekFlashModels(&entry); got != tc.want {
				t.Fatalf("changed = %v, want %v", got, tc.want)
			}
			if !slices.Equal(entry.ModelList(), tc.models) {
				t.Fatalf("models = %v, want %v", entry.ModelList(), tc.models)
			}
			tickDeepSeekFlashVision(&entry)
			if tc.vision != nil && !slices.Equal(entry.VisionModels, tc.vision) {
				t.Fatalf("vision_models = %v, want %v", entry.VisionModels, tc.vision)
			}
		})
	}
}

// A price or an effort list is keyed by model. Left under the retired key it
// stops answering for the model that replaced it, which reads to the user as
// the rate they typed reverting to the default on its own.
func TestRetiredDeepSeekFlashModelsCarryTheirPerModelSettings(t *testing.T) {
	entry := ProviderEntry{
		Name: "deepseek", Kind: "anthropic", BaseURL: deepSeekAnthropicBaseURL,
		Models: []string{"deepseek-v4-flash", "deepseek-v4-pro"},
		Prices: map[string]*provider.Pricing{
			"deepseek-v4-flash": {CacheHit: 9, Input: 9, Output: 9, Currency: "$"},
		},
		ModelOverrides: map[string]ProviderModelOverride{
			"deepseek-v4-flash": {SupportedEfforts: []string{"disabled", "max"}, DefaultEffort: "max"},
		},
		VisionModels: []string{"deepseek-v4-flash"},
		Default:      "deepseek-v4-flash",
	}
	if !migrateRetiredDeepSeekFlashModels(&entry) {
		t.Fatal("nothing migrated")
	}
	if p := entry.Prices[DeepSeekFlashModel]; p == nil || p.Input != 9 {
		t.Fatalf("price did not follow the rename: %+v", entry.Prices)
	}
	if _, stale := entry.Prices["deepseek-v4-flash"]; stale {
		t.Fatal("the retired key still holds a price; the model now has two")
	}
	if ov := entry.ModelOverrides[DeepSeekFlashModel]; ov.DefaultEffort != "max" {
		t.Fatalf("override did not follow the rename: %+v", entry.ModelOverrides)
	}
	if entry.Default != DeepSeekFlashModel {
		t.Fatalf("default = %q", entry.Default)
	}
	if !slices.Equal(entry.VisionModels, []string{DeepSeekFlashModel}) {
		t.Fatalf("vision_models = %v", entry.VisionModels)
	}
}

// Running twice must not change anything the second time: a plain load does not
// persist, so the next one starts from the same file.
func TestDeepSeekCatalogMigrationIsIdempotent(t *testing.T) {
	c := &Config{Providers: []ProviderEntry{{
		Name: "deepseek", Kind: "anthropic", BaseURL: deepSeekAnthropicBaseURL,
		Models: []string{"deepseek-v4-flash", "deepseek-v4-pro"},
	}}}
	normalizeOfficialDeepSeekModels(c)
	if normalizeOfficialDeepSeekModels(c) {
		t.Fatal("a second pass reported a change; the file would be rewritten every load")
	}
	if got := c.Providers[0].ModelList(); !slices.Equal(got, []string{DeepSeekFlashModel, deepSeekProModel}) {
		t.Fatalf("models = %v", got)
	}
}

// The protocol upgrade allow-lists the models it shipped. Migrating them must
// not read as curation, or it would strand exactly the users it just reached.
func TestMigrationKeepsTheProtocolUpgradeAvailable(t *testing.T) {
	entry := ProviderEntry{
		Name: "deepseek", Kind: "openai", BaseURL: "https://api.deepseek.com",
		APIKeyEnv: "DEEPSEEK_API_KEY", Models: []string{"deepseek-v4-flash", "deepseek-v4-pro"},
	}
	if !CanUpgradeDeepSeekProviderProtocol(&entry) {
		t.Fatal("fixture does not start upgradable")
	}
	migrateRetiredDeepSeekFlashModels(&entry)
	if !CanUpgradeDeepSeekProviderProtocol(&entry) {
		t.Fatal("the migration froze this entry out of the protocol upgrade")
	}
}

// The notice a user gets when an attachment cannot be read has to know whether
// anything could read it; saying "handed to a delegate" with nothing configured
// is how a dropped picture reads as a delivered one.
func TestFirstVisionModelRefFindsAReaderOrSaysNone(t *testing.T) {
	none := &Config{Providers: []ProviderEntry{{
		Name: "deepseek-pro", Kind: "anthropic", BaseURL: deepSeekAnthropicBaseURL,
		Models: []string{deepSeekProModel},
	}}}
	if got := none.FirstVisionModelRef(); got != "" {
		t.Fatalf("text-only config offered %q", got)
	}

	withVision := &Config{Providers: []ProviderEntry{{
		Name: "deepseek", Kind: "anthropic", BaseURL: deepSeekAnthropicBaseURL,
		Models:       []string{DeepSeekFlashModel, deepSeekProModel},
		VisionModels: []string{DeepSeekFlashModel},
	}}}
	if got := withVision.FirstVisionModelRef(); got != "deepseek/"+DeepSeekFlashModel {
		t.Fatalf("offered %q, want the image-taking model", got)
	}
}
