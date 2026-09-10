package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Windows-edited configurations keep CRLF. Every schema 7-9 file takes the
// lexical migration, so line endings must survive both the version-only fast
// path and the provider-splitting rewrite.
func TestOpenCodeGoV10MigrationPreservesCRLF(t *testing.T) {
	plain := `# intro
config_version = 9 # c
default_model = "custom/text"

[[providers]]
name = "custom"
kind = "openai"
base_url = "https://example.invalid/v1"
api_key_env = "CUSTOM_KEY"
model = "text"
`
	versionLater := `default_model = "custom/text"
config_version = 9

[[providers]]
name = "custom"
kind = "openai"
base_url = "https://example.invalid/v1"
api_key_env = "CUSTOM_KEY"
model = "text"
`
	for name, fixture := range map[string]struct {
		body      string
		providers int
		aliases   bool
	}{
		"version-only":      {plain, 1, false},
		"version-not-first": {versionLater, 1, false},
		"official-deepseek": {strings.Replace(plain, "config_version = 9 # c", "config_version = 8", 1), 1, false},
		"opencode-go-split": {openCodeGoUpgradeFixture, 5, true},
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			crlf := strings.ReplaceAll(fixture.body, "\n", "\r\n")
			if err := os.WriteFile(path, []byte(crlf), 0o600); err != nil {
				t.Fatal(err)
			}
			changed, err := ApplyUserConfigUpgradesOnStartup(path)
			if err != nil || !changed {
				t.Fatalf("changed=%v err=%v", changed, err)
			}
			raw, _ := os.ReadFile(path)
			text := string(raw)
			if strings.Contains(strings.ReplaceAll(text, "\r\n", ""), "\n") || !strings.Contains(text, "\r\n") {
				t.Fatalf("line endings not preserved:\n%q", text)
			}
			var cfg Config
			if _, err := decodeTOMLFile(path, &cfg); err != nil {
				t.Fatalf("migrated config no longer parses: %v\n%q", err, text)
			}
			if cfg.ConfigVersion != openCodeGoUpgradeVersion || len(cfg.Providers) != fixture.providers {
				t.Fatalf("version=%d providers=%d\n%q", cfg.ConfigVersion, len(cfg.Providers), text)
			}
			if strings.Count(text, "config_version") != 1 {
				t.Fatalf("duplicate config_version:\n%q", text)
			}
			if j := readOpenCodeGoJournal(path, raw); fixture.aliases && (j == nil || !j.Committed || len(j.Aliases) == 0) {
				t.Fatalf("journal not activated for CRLF split: %+v", j)
			}
			if _, err := ApplyUserConfigUpgradesOnStartup(path); err != nil {
				t.Fatalf("second startup: %v", err)
			}
		})
	}
}
