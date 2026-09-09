package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestOfficialDeepSeekV9MigrationAndManualChoice(t *testing.T) {
	for _, kind := range []string{"anthropic", "responses"} {
		for _, inline := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/inline=%v", kind, inline), func(t *testing.T) {
				base, endpoint := "https://api.deepseek.com", "https://api.deepseek.com/responses"
				if kind == "anthropic" {
					base, endpoint = deepSeekAnthropicBaseURL, deepSeekAnthropicBaseURL+"/v1/messages"
				}
				fields := []string{`name="Deepseek2"`, `preset_id="deepseek-anthropic"`, `kind="` + kind + `"`, `base_url="` + base + `"`, `request_url="` + endpoint + `"`, `api_key_env="MY_KEY"`, `models=["DeepSeek-V4.1-Flash-Expires-On-0910","custom-ID"]`, `default="custom-ID"`, `headers={X-Test="keep"}`, `future={value="keep"}`}
				raw := "config_version = 8 # preserve\n# comment\n[[providers]]\n" + strings.Join(fields, "\n") + "\n"
				if inline {
					raw = "config_version = 8 # preserve\n# comment\nproviders=[{" + strings.Join(fields, ",") + "}]\n"
				}
				path := filepath.Join(t.TempDir(), "config.toml")
				if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
					t.Fatal(err)
				}
				if changed, err := ApplyUserConfigUpgradesOnStartup(path); err != nil || !changed {
					t.Fatalf("migration: %v %v", changed, err)
				}
				got, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				want := strings.Replace(raw, "config_version = 8", "config_version = 9", 1)
				want = strings.Replace(want, `kind="`+kind+`"`, `kind="openai"`, 1)
				want = strings.Replace(want, `base_url="`+base+`"`, `base_url="https://api.deepseek.com"`, 1)
				want = strings.Replace(want, `request_url="`+endpoint+`"`, `request_url=""`, 1)
				if string(got) != want {
					t.Fatalf("unexpected edit:\n%s\nwant:\n%s", got, want)
				}
				var c Config
				if _, err := toml.Decode(string(got), &c); err != nil {
					t.Fatal(err)
				}
				if c.Providers[0].Default != "custom-ID" || c.Providers[0].Models[0] != "DeepSeek-V4.1-Flash-Expires-On-0910" {
					t.Fatal("model identity changed")
				}
				loaded := LoadForEdit(path)
				p, ok := loaded.Provider("Deepseek2")
				if !ok || p.Kind != "openai" || p.RequestURL != "" {
					t.Fatal("preset identity restored the old protocol on load")
				}
				// Clearing the standard override is what keeps the account visible
				// to IsOfficialDeepSeekSearchEndpoint.
				if !EffectiveIndependentWebSearch(p) {
					t.Fatal("migration disabled independent web search")
				}
				// Persist through the ordinary writer, then restart twice.
				c.Providers[0].Kind, c.Providers[0].BaseURL, c.Providers[0].RequestURL = kind, base, endpoint
				if inline {
					// Keep this fixture inline, as a user editing TOML would.
					if err := os.WriteFile(path, []byte(strings.Replace(raw, "config_version = 8", "config_version = 9", 1)), 0600); err != nil {
						t.Fatal(err)
					}
				} else if err := c.SaveTo(path); err != nil {
					t.Fatal(err)
				}
				before, _ := os.ReadFile(path)
				for range 2 {
					if changed, err := ApplyUserConfigUpgradesOnStartup(path); err != nil || changed {
						t.Fatalf("manual choice reset: %v %v", changed, err)
					}
				}
				after, _ := os.ReadFile(path)
				if string(after) != string(before) {
					t.Fatal("changed after restart")
				}
			})
		}
	}
}

func TestOfficialDeepSeekV9EndpointBoundary(t *testing.T) {
	for _, endpoint := range []string{"https://relay.example/anthropic", "https://api.deepseek.com/custom/messages", "https://api.deepseek.com/anthropic/v1/messages?route=custom", "https://api.deepseek.com.evil.test/anthropic", "http://api.deepseek.com/anthropic"} {
		p := ProviderEntry{Kind: "anthropic", BaseURL: deepSeekAnthropicBaseURL, RequestURL: endpoint}
		if isOfficialDeepSeekChatUpgrade(&p) {
			t.Errorf("accepted %s", endpoint)
		}
	}
}
