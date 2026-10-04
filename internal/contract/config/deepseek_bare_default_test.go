package config

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
)

// The retired flash name is migrated out of the official entry's model list,
// so a bare default_model naming it has to move with it or stop resolving.
func TestRetiredBareDeepSeekDefaultFollowsTheModelList(t *testing.T) {
	for _, tc := range []struct{ name, def string }{
		{"bare retired name", "deepseek-v4-flash"},
		{"qualified retired name", "deepseek/deepseek-v4-flash"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := testenv.TempDir(t)
			ws := testenv.TempDir(t)
			t.Setenv("REASONIX_HOME", home)
			seed := `config_version = 1
default_model = "` + tc.def + `"

[[providers]]
name        = "deepseek"
kind        = "anthropic"
base_url    = "https://api.deepseek.com/anthropic"
models      = ["deepseek-v4-flash", "deepseek-v4-pro"]
api_key_env = "DEEPSEEK_API_KEY"
`
			path := filepath.Join(home, "config.toml")
			if err := os.WriteFile(path, []byte(seed), 0o600); err != nil {
				t.Fatal(err)
			}
			cfg, err := LoadForRootReadOnly(ws)
			if err != nil {
				t.Fatal(err)
			}
			entry, ok := cfg.ResolveModel(cfg.DefaultModel)
			if !ok {
				t.Fatalf("default_model %q no longer resolves; providers: %+v", cfg.DefaultModel, cfg.Providers)
			}
			if entry.Name != "deepseek" || entry.Model != DeepSeekFlashModel {
				t.Fatalf("default_model resolves to %s/%s, want deepseek/%s", entry.Name, entry.Model, DeepSeekFlashModel)
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(after, []byte(seed)) {
				t.Fatalf("a read-only load rewrote config.toml:\n%s", after)
			}
		})
	}
}
