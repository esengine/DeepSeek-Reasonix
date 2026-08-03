package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestMergeProvidersWithDefaultsPreservesCustomProviders(t *testing.T) {
	custom := []ProviderEntry{
		{Name: "alpha", Kind: "openai", BaseURL: "https://alpha.example/v1", Model: "alpha-1"},
		{Name: "beta", Kind: "openai", BaseURL: "https://beta.example/v1", Model: "beta-1"},
	}
	merged := mergeProvidersWithDefaults(custom)
	// Built-ins first, then custom in order.
	builtinCount := len(Default().Providers)
	if len(merged) != builtinCount+2 {
		t.Fatalf("merged = %d providers, want %d", len(merged), builtinCount+2)
	}
	for i, p := range custom {
		got := merged[builtinCount+i]
		if got.Name != p.Name || got.BaseURL != p.BaseURL || got.Model != p.Model {
			t.Errorf("custom provider %d = %+v, want %+v", i, got, p)
		}
	}
}

func TestMergeProvidersWithDefaultsBuiltinOverrideOnce(t *testing.T) {
	// An explicit entry with a built-in name replaces the default exactly once.
	override := ProviderEntry{Name: "deepseek-flash", Kind: "openai", BaseURL: "https://override.example/v1", Model: "m"}
	merged := mergeProvidersWithDefaults([]ProviderEntry{override, {Name: "custom", BaseURL: "x"}})
	count := 0
	for _, p := range merged {
		if p.Name == "deepseek-flash" {
			count++
			if p.BaseURL != "https://override.example/v1" {
				t.Errorf("built-in was not replaced: %+v", p)
			}
		}
	}
	if count != 1 {
		t.Fatalf("deepseek-flash appears %d times, want exactly 1", count)
	}
}

func TestNormalizePersistedConfigKeepsCustomProviders(t *testing.T) {
	cfg := Default()
	cfg.Providers = []ProviderEntry{
		{Name: "alpha", Kind: "openai", BaseURL: "https://alpha.example/v1", Model: "alpha-1"},
		{Name: "beta", Kind: "openai", BaseURL: "https://beta.example/v1", Model: "beta-1"},
	}
	normalized := normalizePersistedConfig(cfg)
	seen := map[string]bool{}
	for _, p := range normalized.Providers {
		seen[p.Name] = true
		if p.Name == "alpha" && p.BaseURL != "https://alpha.example/v1" {
			t.Errorf("alpha lost its base_url: %+v", p)
		}
		if p.Name == "beta" && p.Model != "beta-1" {
			t.Errorf("beta lost its model: %+v", p)
		}
	}
	if !seen["alpha"] || !seen["beta"] {
		t.Fatalf("custom providers missing after normalization: %v", seen)
	}
}

func TestValidateAndWriteRejectsDroppedCustomProvider(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "config.toml")
	want := Default()
	want.Providers = []ProviderEntry{
		{Name: "alpha", Kind: "openai", BaseURL: "https://alpha.example/v1", Model: "alpha-1"},
	}
	body, err := renderTOMLForScopeErr(want, RenderScopeUser)
	if err != nil {
		t.Fatal(err)
	}
	// A renderer that drops the custom provider's base_url must be caught by
	// the persisted-semantics comparison (not only by round-trip drift).
	lines := strings.Split(body, "\n")
	kept := lines[:0]
	for _, line := range lines {
		if strings.Contains(line, `base_url    = "https://alpha.example/v1"`) {
			continue
		}
		kept = append(kept, line)
	}
	opts := writeConfigOptions{scope: RenderScopeUser, want: want}
	_, err = validateAndWriteConfigResolved(path, strings.Join(kept, "\n"), 0o600, opts, "")
	if err == nil || !strings.Contains(err.Error(), "providers") || !strings.Contains(err.Error(), "base_url") {
		t.Fatalf("dropped custom provider field accepted: %v", err)
	}
}
