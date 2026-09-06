package config

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestDeepSeekChatDefaultStartupUpgradePreservesData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	raw := `# user comment
config_version = 7 # schema
future_field = "keep me"
default_model = "deepseek/deepseek-v4-pro"
[[providers]]
name = "deepseek"
kind = "anthropic" # transport
base_url = "https://api.deepseek.com/anthropic"
models = ["deepseek-v4-flash", "deepseek-v4-pro"]
default = "deepseek-v4-pro"
api_key_env = "DEEPSEEK_API_KEY"
web_search = false
default_effort = "max"
future_provider_field = { value = "untouched" }
[providers.prices.deepseek-v4-pro]
input = 123
[desktop]
future_desktop_field = true
`
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	changed, err := ApplyUserConfigUpgradesOnStartup(path)
	if err != nil || !changed {
		t.Fatalf("upgrade=%v err=%v", changed, err)
	}
	got, _ := os.ReadFile(path)
	want := strings.ReplaceAll(raw, `config_version = 7`, `config_version = 8`)
	want = strings.ReplaceAll(want, `kind = "anthropic"`, `kind = "openai"`)
	want = strings.ReplaceAll(want, `base_url = "https://api.deepseek.com/anthropic"`, `base_url = "https://api.deepseek.com"`)
	if string(got) != want {
		t.Fatalf("unexpected rewrite:\n%s", got)
	}
	// After the one-time upgrade, an explicit Messages choice must survive.
	manual := strings.ReplaceAll(want, `kind = "openai"`, `kind = "anthropic"`)
	manual = strings.ReplaceAll(manual, `base_url = "https://api.deepseek.com"`, `base_url = "https://api.deepseek.com/anthropic"`)
	if err := os.WriteFile(path, []byte(manual), 0600); err != nil {
		t.Fatal(err)
	}
	if changed, err := ApplyUserConfigUpgradesOnStartup(path); err != nil || changed {
		t.Fatalf("overrode post-upgrade choice: %v %v", changed, err)
	}
	got, _ = os.ReadFile(path)
	if string(got) != manual {
		t.Fatal("post-upgrade config changed")
	}
}

func TestDeepSeekChatDefaultMigrationScope(t *testing.T) {
	base := ProviderEntry{Name: "deepseek-flash", Kind: "anthropic", BaseURL: deepSeekAnthropicBaseURL, Model: "deepseek-v4-flash", APIKeyEnv: "DEEPSEEK_API_KEY"}
	for _, tc := range []struct {
		name string
		edit func(*ProviderEntry)
		want bool
	}{
		{"stock", func(*ProviderEntry) {}, true},
		{"key and search preference", func(p *ProviderEntry) { p.APIKeyEnv = "MY_KEY"; p.WebSearch = boolPointer(false) }, true},
		{"explicit preset", func(p *ProviderEntry) { p.PresetID = "deepseek-anthropic" }, false},
		{"separate name", func(p *ProviderEntry) { p.Name = "deepseek-anthropic" }, false},
		{"responses choice", func(p *ProviderEntry) { p.Kind = "responses"; p.BaseURL = "https://api.deepseek.com" }, false},
		{"proxy", func(p *ProviderEntry) { p.BaseURL = "https://relay.example/anthropic" }, false},
		{"request override", func(p *ProviderEntry) { p.RequestURL = "https://api.deepseek.com/anthropic/v1/messages" }, false},
		{"headers", func(p *ProviderEntry) { p.Headers = map[string]string{"X-Route": "custom"} }, false},
		{"body override", func(p *ProviderEntry) { p.ExtraBody = map[string]any{"thinking": false} }, false},
		{"unknown model", func(p *ProviderEntry) { p.Model = "future-model" }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := base
			tc.edit(&p)
			if got := isLegacyDeepSeekMessagesDefault(&p); got != tc.want {
				t.Fatalf("eligible=%v want %v", got, tc.want)
			}
		})
	}
}

func TestDeepSeekChatDefaultUpgradeInlineAndConcurrent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	raw := `config_version = 7
providers = [{name="deepseek-pro",kind="anthropic",base_url="https://api.deepseek.com/anthropic",model="deepseek-v4-pro",api_key_env="DEEPSEEK_API_KEY",web_search=false,future="kept"}]
`
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			if _, err := ApplyUserConfigUpgradesOnStartup(path); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	got, _ := os.ReadFile(path)
	var parsed Config
	if _, err := toml.Decode(string(got), &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.ConfigVersion != 8 || len(parsed.Providers) != 1 || parsed.Providers[0].Kind != "openai" || *parsed.Providers[0].WebSearch {
		t.Fatalf("bad migration: %s", got)
	}
	if !strings.Contains(string(got), `future="kept"`) {
		t.Fatal("lost unknown field")
	}
}

func TestDeepSeekChatDefaultUpgradeFutureConfigUntouched(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	raw := `config_version = 999
providers = [{name="deepseek-flash",kind="anthropic",base_url="https://api.deepseek.com/anthropic",model="deepseek-v4-flash",api_key_env="DEEPSEEK_API_KEY"}]
`
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	if changed, err := ApplyUserConfigUpgradesOnStartup(path); err != nil || changed {
		t.Fatalf("future changed: %v %v", changed, err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != raw {
		t.Fatal("future config rewritten")
	}
}

func TestOldAutomaticProtocolMigrationDoesNotUndoChatDefault(t *testing.T) {
	raw := `config_version = 8
[[providers]]
name = "deepseek-flash"
kind = "openai"
base_url = "https://api.deepseek.com"
model = "deepseek-v4-flash"
api_key_env = "DEEPSEEK_API_KEY"
`
	next, changed, err := rewriteLegacyDeepSeekProtocol(raw, "", true)
	if err != nil || changed || next != raw {
		t.Fatalf("old migration undid v8 default: changed=%v err=%v", changed, err)
	}
}
