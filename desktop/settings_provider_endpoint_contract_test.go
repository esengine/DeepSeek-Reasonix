package main

import (
	"strings"
	"testing"

	"reasonix/internal/config"
)

func TestProviderViewCarriesHiddenCatalogIdentity(t *testing.T) {
	view := providerViewFromEntry(config.ProviderEntry{
		Name: "deepseek-anthropic", Kind: "anthropic", BaseURL: "https://api.deepseek.com/anthropic", Model: "deepseek-v4-flash",
	}, false, true)
	if view.PresetID != "deepseek-anthropic" || view.Catalog == nil {
		t.Fatalf("catalog identity missing: %+v", view)
	}
	if view.Catalog.BrandID != "deepseek" || view.Catalog.Region != "global" || view.Catalog.Product != "api" {
		t.Fatalf("catalog identity = %+v", view.Catalog)
	}
	if got := view.Catalog.Protocols["anthropic"].BaseURL; got != "https://api.deepseek.com/anthropic" {
		t.Fatalf("Anthropic route = %q", got)
	}
}

func TestSaveProviderRejectsProtocolEndpointMismatch(t *testing.T) {
	cfg := config.Default()
	cfg.Providers = []config.ProviderEntry{{
		Name: "deepseek-anthropic", PresetID: "deepseek-anthropic", Kind: "anthropic", BaseURL: "https://api.deepseek.com/anthropic", Model: "deepseek-v4-flash",
	}}
	view := providerViewFromEntry(cfg.Providers[0], false, true)
	view.Kind = "openai"
	view.RequestURL = "https://api.deepseek.com/anthropic/v1/chat/completions"
	view.ChatURL = view.RequestURL
	if err := saveProviderConfig(cfg, view); err == nil || !strings.Contains(err.Error(), "https://api.deepseek.com/v1/chat/completions") {
		t.Fatalf("save mismatch error = %v", err)
	}
	if got := cfg.Providers[0].Kind; got != "anthropic" {
		t.Fatalf("rejected save mutated provider kind to %q", got)
	}
}

func TestProtocolSwitchKeepsStableProviderAndModelReferences(t *testing.T) {
	cfg := config.Default()
	cfg.DefaultModel = "deepseek-anthropic/deepseek-v4-flash"
	cfg.Providers = []config.ProviderEntry{{
		Name: "deepseek-anthropic", DisplayName: "Deepseek2", PresetID: "deepseek-anthropic", Kind: "anthropic",
		BaseURL: "https://api.deepseek.com/anthropic", RequestURL: "https://api.deepseek.com/anthropic/v1/messages",
		Model: "deepseek-v4-flash", Models: []string{"deepseek-v4-flash"},
	}}
	view := providerViewFromEntry(cfg.Providers[0], false, true)
	view.Kind = "openai"
	view.BaseURL = "https://api.deepseek.com/v1"
	view.RequestURL = "https://api.deepseek.com/v1/chat/completions"
	view.ChatURL = view.RequestURL
	if err := saveProviderConfig(cfg, view); err != nil {
		t.Fatalf("save protocol switch: %v", err)
	}
	got := cfg.Providers[0]
	if got.Name != "deepseek-anthropic" || got.PresetID != "deepseek-anthropic" || cfg.DefaultModel != "deepseek-anthropic/deepseek-v4-flash" {
		t.Fatalf("stable references changed: provider=%+v default=%q", got, cfg.DefaultModel)
	}
	if got.DisplayName != "Deepseek2" || got.Kind != "openai" || got.RequestURL != "https://api.deepseek.com/v1/chat/completions" {
		t.Fatalf("switched connection = %+v", got)
	}
}
