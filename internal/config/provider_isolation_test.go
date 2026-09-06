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

// officialDeepSeekTOML declares the official endpoint under name without
// context_window, balance_url or price, which a documented config may omit.
func officialDeepSeekTOML(name string) string {
	return `config_version = 1
default_model = "` + name + `/deepseek-v4-flash"

[[providers]]
name        = "` + name + `"
kind        = "openai"
base_url    = "https://api.deepseek.com"
model       = "deepseek-v4-flash"
api_key_env = "DEEPSEEK_API_KEY"
`
}

func assertOfficialDeepSeekDefaults(t *testing.T, tag string, p *ProviderEntry) {
	t.Helper()
	if p.ContextWindow != 1_000_000 {
		t.Errorf("%s: official DeepSeek provider has context_window %d, want 1000000; a zero window disables compaction", tag, p.ContextWindow)
	}
	if p.BalanceURL != "https://api.deepseek.com/user/balance" {
		t.Errorf("%s: official DeepSeek provider has balance_url %q, want the vendor wallet endpoint; empty removes the balance readout", tag, p.BalanceURL)
	}
	if p.Prices["deepseek-v4-flash"] == nil {
		t.Errorf("%s: official DeepSeek provider lost its per-model price backfill: prices=%v", tag, p.Prices)
	}
}

// TestOfficialDeepSeekProviderStillGetsItsDefaults pins the other half of the
// contract. Isolating the decoded provider list must not strip the defaults a
// genuinely official endpoint may omit, so they are reapplied by an
// endpoint-keyed backfill that a custom provider can never match.
//
// Both loaders are checked because they normalize through different entry
// points: the runtime loader feeds the agent and compaction, while the edit
// loader is what desktop Settings reads and writes back.
func TestOfficialDeepSeekProviderStillGetsItsDefaults(t *testing.T) {
	for _, name := range []string{"deepseek", "deepseek-flash"} {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			ws := t.TempDir()
			t.Setenv("REASONIX_HOME", home)
			path := filepath.Join(home, "config.toml")
			if err := os.WriteFile(path, []byte(officialDeepSeekTOML(name)), 0o600); err != nil {
				t.Fatal(err)
			}

			cfg, err := LoadForRootReadOnly(ws)
			if err != nil {
				t.Fatal(err)
			}
			p, ok := cfg.Provider(name)
			if !ok {
				t.Fatalf("official %q provider missing after runtime load", name)
			}
			assertOfficialDeepSeekDefaults(t, "LoadForRoot/"+name, p)

			edit := LoadForEditWithoutCredentials(path)
			ep, ok := edit.Provider(name)
			if !ok {
				t.Fatalf("official %q provider missing after edit load", name)
			}
			assertOfficialDeepSeekDefaults(t, "LoadForEdit/"+name, ep)
		})
	}
}

func TestMissingCanonicalDeepSeekUsesChatDefault(t *testing.T) {
	cfg := Default()
	ensureDeepSeekOfficialProvider(cfg)
	p, ok := cfg.Provider("deepseek")
	if !ok {
		t.Fatal("canonical DeepSeek provider missing after load")
	}
	if p.Kind != "openai" || p.BaseURL != "https://api.deepseek.com" || !EffectiveIndependentWebSearch(p) {
		t.Fatalf("canonical DeepSeek provider = kind:%q base_url:%q web_search:%t, want Chat Completions with independent search", p.Kind, p.BaseURL, EffectiveIndependentWebSearch(p))
	}
}

func TestExplicitOpenAIDeepSeekProviderIsNotMigrated(t *testing.T) {
	home := t.TempDir()
	ws := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	path := filepath.Join(home, "config.toml")
	raw := `config_version = 1
default_model = "deepseek-flash/deepseek-v4-flash"

[[providers]]
name        = "deepseek-flash"
kind        = "openai"
base_url    = "https://api.deepseek.com"
model       = "deepseek-v4-flash"
api_key_env = "DEEPSEEK_API_KEY"
`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadForRootReadOnly(ws)
	if err != nil {
		t.Fatal(err)
	}
	p, ok := cfg.Provider("deepseek-flash")
	if !ok {
		t.Fatal("explicit DeepSeek provider missing after load")
	}
	if p.Kind != "openai" || p.BaseURL != "https://api.deepseek.com" {
		t.Fatalf("explicit DeepSeek provider migrated to kind:%q base_url:%q", p.Kind, p.BaseURL)
	}
}

// TestOfficialDeepSeekBackfillRespectsDeclaredValues keeps the backfill from
// overriding a user who deliberately narrowed the window or disabled the
// balance readout for the official endpoint.
func TestOfficialDeepSeekBackfillRespectsDeclaredValues(t *testing.T) {
	home := t.TempDir()
	ws := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	declared := `config_version = 1
default_model = "deepseek-flash/deepseek-v4-flash"

[[providers]]
name           = "deepseek-flash"
kind           = "openai"
base_url       = "https://api.deepseek.com"
model          = "deepseek-v4-flash"
api_key_env    = "DEEPSEEK_API_KEY"
context_window = 65536
`
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(declared), 0o600); err != nil {
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
	if p.ContextWindow != 65536 {
		t.Errorf("declared context_window was overwritten: got %d, want 65536", p.ContextWindow)
	}
}
