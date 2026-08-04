package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// customProviderTOML declares a single self-hosted OpenAI-compatible provider
// and none of the vendor-specific fields an official DeepSeek entry carries.
const customProviderTOML = `config_version = 1
default_model = "gateway/my-model"

[[providers]]
name        = "gateway"
kind        = "openai"
base_url    = "http://localhost:8021/v1"
models      = ["my-model"]
api_key_env = "GATEWAY_API_KEY"
`

func assertNoOfficialDeepSeekFields(t *testing.T, tag string, p *ProviderEntry) {
	t.Helper()
	if p.BalanceURL != "" {
		t.Errorf("%s: custom provider gained balance_url %q; a self-hosted endpoint must not be pointed at another vendor's wallet API", tag, p.BalanceURL)
	}
	if p.ContextWindow != 0 {
		t.Errorf("%s: custom provider gained context_window %d; an undeclared window must stay unset so compaction is not sized against a foreign default", tag, p.ContextWindow)
	}
	if p.Price != nil {
		t.Errorf("%s: custom provider gained price %+v; another vendor's price table must not be applied to it", tag, *p.Price)
	}
	if p.Model != "" {
		t.Errorf("%s: custom provider gained model %q from a built-in default", tag, p.Model)
	}
}

// TestLoadForEditKeepsCustomProviderFreeOfOfficialDefaults covers #7357/#7358.
// Config loads seed from Default(), which ships two official DeepSeek entries.
// TOML array-of-tables decoding is positional, so the first [[providers]] in a
// user file used to be unified onto the DeepSeek entry already occupying index
// 0 and silently inherit every field the user had not set.
func TestLoadForEditKeepsCustomProviderFreeOfOfficialDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(customProviderTOML), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := LoadForEditWithoutCredentials(path)
	if len(cfg.Providers) != 1 {
		t.Fatalf("providers = %d, want 1", len(cfg.Providers))
	}
	p := &cfg.Providers[0]
	if p.Name != "gateway" || p.BaseURL != "http://localhost:8021/v1" {
		t.Fatalf("unexpected provider identity: name=%q base_url=%q", p.Name, p.BaseURL)
	}
	assertNoOfficialDeepSeekFields(t, "LoadForEdit", p)
}

// TestSaveAfterLoadForEditDoesNotWriteForeignProviderFields is the on-disk half
// of #7357: the leaked fields were persisted into the user's own file on the
// next rewrite, so they outlived the process that invented them.
func TestSaveAfterLoadForEditDoesNotWriteForeignProviderFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(customProviderTOML), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := LoadForEditWithoutCredentials(path)
	if err := cfg.SaveTo(path); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, unwanted := range []string{"balance_url", "context_window", "price"} {
		if strings.Contains(string(raw), unwanted) {
			t.Errorf("rewritten config contains %q for a custom provider:\n%s", unwanted, raw)
		}
	}
}

// TestLoadForRootKeepsCustomProviderFreeOfOfficialDefaults exercises the same
// leak through the runtime loader, which is what the agent and compaction read.
func TestLoadForRootKeepsCustomProviderFreeOfOfficialDefaults(t *testing.T) {
	home := t.TempDir()
	ws := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(customProviderTOML), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadForRootReadOnly(ws)
	if err != nil {
		t.Fatal(err)
	}
	p, ok := cfg.Provider("gateway")
	if !ok {
		t.Fatal("gateway provider missing after load")
	}
	assertNoOfficialDeepSeekFields(t, "LoadForRoot", p)
}

// TestLastKnownGoodRecoveryKeepsCustomProviderFreeOfOfficialDefaults covers the
// recovery path, which decodes a snapshot onto a freshly seeded Config and so
// leaked through the same positional overlay.
func TestLastKnownGoodRecoveryKeepsCustomProviderFreeOfOfficialDefaults(t *testing.T) {
	home := t.TempDir()
	ws := t.TempDir()
	t.Setenv("REASONIX_HOME", home)

	// Malformed live config forces the last-known-good branch.
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("config_version = ["), 0o600); err != nil {
		t.Fatal(err)
	}
	lkg := LastKnownGoodConfigPath()
	if lkg == "" {
		t.Skip("last-known-good path unavailable in this environment")
	}
	if err := os.MkdirAll(filepath.Dir(lkg), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lkg, []byte(customProviderTOML), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadForRootReadOnly(ws)
	if err != nil {
		t.Fatal(err)
	}
	p, ok := cfg.Provider("gateway")
	if !ok {
		t.Fatal("gateway provider missing after last-known-good recovery")
	}
	assertNoOfficialDeepSeekFields(t, "last-known-good", p)
}

// TestSecondCustomProviderKeepsNoOfficialDefaults guards the index-1 overlay:
// Default() ships two DeepSeek entries, so the second declared provider used to
// inherit the Pro SKU's price table and model.
func TestSecondCustomProviderKeepsNoOfficialDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	raw := customProviderTOML + `
[[providers]]
name        = "second"
kind        = "openai"
base_url    = "http://localhost:9000/v1"
models      = ["m2"]
api_key_env = "SECOND_KEY"
`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := LoadForEditWithoutCredentials(path)
	if len(cfg.Providers) != 2 {
		t.Fatalf("providers = %d, want 2", len(cfg.Providers))
	}
	for i := range cfg.Providers {
		assertNoOfficialDeepSeekFields(t, cfg.Providers[i].Name, &cfg.Providers[i])
	}
}

// TestOfficialDeepSeekProviderStillGetsItsDefaults pins the other half of the
// contract: removing the positional overlay must not stop a genuinely official
// endpoint from being priced, because that backfill is keyed on the endpoint.
func TestOfficialDeepSeekProviderStillGetsItsDefaults(t *testing.T) {
	home := t.TempDir()
	ws := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	official := `config_version = 1
default_model = "deepseek-flash/deepseek-v4-flash"

[[providers]]
name        = "deepseek-flash"
kind        = "openai"
base_url    = "https://api.deepseek.com"
model       = "deepseek-v4-flash"
api_key_env = "DEEPSEEK_API_KEY"
`
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(official), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadForRootReadOnly(ws)
	if err != nil {
		t.Fatal(err)
	}
	p, ok := cfg.Provider("deepseek-flash")
	if !ok {
		t.Fatal("official deepseek-flash provider missing after load")
	}
	if p.Prices["deepseek-v4-flash"] == nil {
		t.Errorf("official DeepSeek provider lost its per-model price backfill: prices=%v", p.Prices)
	}
}
