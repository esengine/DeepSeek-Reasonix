package config

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/provider"
)

// The vision connection existed because reading an image needed a model of its
// own. It no longer does, so the entry is a second name for what the flash entry
// already reaches — and two rows for one model is what a panel shows as two
// accounts.
func TestRetiredVisionProviderFoldsIntoFlash(t *testing.T) {
	home := testenv.TempDir(t)
	ws := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	seed := `config_version = 1
default_model = "deepseek-vision/deepseek-v4-flash-vision-exp"

[[providers]]
name        = "deepseek-flash"
kind        = "anthropic"
base_url    = "https://api.deepseek.com/anthropic"
model       = "deepseek-v4-flash"
api_key_env = "DEEPSEEK_API_KEY"

[[providers]]
name        = "deepseek-vision"
kind        = "anthropic"
base_url    = "https://api.deepseek.com/anthropic"
model       = "deepseek-v4-flash-vision-exp"
api_key_env = "DEEPSEEK_API_KEY"
`
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(seed), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadForRootReadOnly(ws)
	if err != nil {
		t.Fatal(err)
	}
	if _, still := cfg.Provider(retiredDeepSeekVisionProvider); still {
		t.Fatal("the vision connection survived the fold; one model now shows as two sources")
	}
	entry, ok := cfg.ResolveModel(cfg.DefaultModel)
	if !ok {
		t.Fatalf("default_model %q no longer resolves after the fold", cfg.DefaultModel)
	}
	if !entry.HasVisionModel(DeepSeekFlashModel) {
		t.Fatalf("the surviving entry does not read images: %+v", entry.VisionModels)
	}
}

// Folding an entry the user configured would answer their edit by deleting
// where it lived, so anything of their own on it keeps the entry.
func TestConfiguredVisionProviderIsNotFolded(t *testing.T) {
	c := &Config{Providers: []ProviderEntry{
		{
			Name: "deepseek-flash", Kind: "anthropic", BaseURL: deepSeekAnthropicBaseURL,
			Model: DeepSeekFlashModel, APIKeyEnv: "DEEPSEEK_API_KEY",
		},
		{
			Name: retiredDeepSeekVisionProvider, Kind: "anthropic", BaseURL: deepSeekAnthropicBaseURL,
			Model: DeepSeekFlashModel, APIKeyEnv: "DEEPSEEK_API_KEY",
			Prices: map[string]*provider.Pricing{DeepSeekFlashModel: {Input: 9, Output: 9, Currency: "$"}},
		},
	}}
	if foldRetiredDeepSeekVisionProvider(c) {
		t.Fatal("an entry carrying the user's own price was folded away")
	}
	if _, ok := c.Provider(retiredDeepSeekVisionProvider); !ok {
		t.Fatal("the entry is gone")
	}
}

// A fold needs somewhere for its callers to land. Without a peer on the same
// key the entry is the only route to the model, and dropping it removes the
// capability rather than deduplicating it.
func TestVisionProviderWithoutAPeerIsKept(t *testing.T) {
	c := &Config{Providers: []ProviderEntry{{
		Name: retiredDeepSeekVisionProvider, Kind: "anthropic", BaseURL: deepSeekAnthropicBaseURL,
		Model: DeepSeekFlashModel, APIKeyEnv: "DEEPSEEK_API_KEY",
	}}}
	if foldRetiredDeepSeekVisionProvider(c) {
		t.Fatal("folded the only entry reaching the model")
	}
}
