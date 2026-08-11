package config

import (
	"os"
	"path/filepath"
	"testing"
)

// Regression for #8337: a per-provider effort selection written by /effort or
// the desktop must not make the DeepSeek family non-canonicalizable on the
// next load. Before the fix, the canonical member carrying effort="max" next
// to legacy family members with empty effort failed the family-equality check,
// so canonicalization was bypassed and the stored level was unreachable
// (the model came back as Auto).
func TestDeepSeekEffortDoesNotBreakFamilyCanonicalization(t *testing.T) {
	body := `config_version = 5
default_model = "deepseek/deepseek-v4-flash"

[[providers]]
name = "deepseek"
kind = "anthropic"
base_url = "https://api.deepseek.com/anthropic"
models = ["deepseek-v4-flash", "deepseek-v4-pro"]
default = "deepseek-v4-flash"
api_key_env = "DEEPSEEK_API_KEY"
effort = "max"

[[providers]]
name = "deepseek-flash"
kind = "anthropic"
base_url = "https://api.deepseek.com/anthropic"
api_key_env = "DEEPSEEK_API_KEY"
`
	path := filepath.Join(t.TempDir(), "reasonix.toml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	c := LoadForEdit(path)
	if !canCanonicalizeLegacyDeepSeekProviders(c) {
		t.Fatal("a per-provider effort selection on the canonical member must not make the DeepSeek family non-canonicalizable")
	}
	p, ok := c.Provider("deepseek")
	if !ok {
		t.Fatal("canonical deepseek provider missing")
	}
	if got := p.Effort; got != "max" {
		t.Fatalf("stored effort = %q, want max", got)
	}
}
