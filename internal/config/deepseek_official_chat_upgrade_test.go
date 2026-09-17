package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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
				want := strings.Replace(raw, "config_version = 8", "config_version = 10", 1)
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
					if err := os.WriteFile(path, []byte(strings.Replace(raw, "config_version = 8", "config_version = 10", 1)), 0600); err != nil {
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

func TestCurrentConfigRepairsExactProviderEndpointContract(t *testing.T) {
	raw := fmt.Sprintf(`config_version = %d # preserve current version
# preserve comment
[[providers]]
name = "deepseek-anthropic"
display_name = "Deepseek2"
preset_id = "deepseek-anthropic"
kind = "responses"
base_url = "https://api.deepseek.com"
request_url = "https://api.deepseek.com/anthropic/v1/messages"
chat_url = "https://stale.example/chat/completions"
api_key_env = "MY_KEY"
models = ["deepseek-v4-flash"]
default = "deepseek-v4-flash"
responses_mode = "stateful" # preserve mode comment
responses_stateful = true # preserve legacy comment
future = { value = "keep" }
`, Default().ConfigVersion)
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(raw), 0o640); err != nil {
		t.Fatal(err)
	}
	if changed, err := ApplyUserConfigUpgradesOnStartup(path); err != nil || !changed {
		t.Fatalf("repair: changed=%v err=%v", changed, err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(got)
	for _, want := range []string{
		"# preserve current version",
		"# preserve comment",
		`kind = "anthropic"`,
		`base_url = "https://api.deepseek.com/anthropic"`,
		`request_url = ""`,
		`chat_url = ""`,
		`api_key_env = "MY_KEY"`,
		`future = { value = "keep" }`,
		"# preserve mode comment",
		"# preserve legacy comment",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("repaired config missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "responses_mode") || strings.Contains(text, "responses_stateful") {
		t.Fatalf("responses-only fields survived repair:\n%s", text)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o640 {
		t.Fatalf("config mode = %o, want 640", info.Mode().Perm())
	}
	loaded := LoadForEdit(path)
	p, ok := loaded.Provider("deepseek-anthropic")
	if !ok || p.Kind != "anthropic" || ProviderEffectiveRequestURL(p) != "https://api.deepseek.com/anthropic/v1/messages" ||
		p.APIKeyEnv != "MY_KEY" || p.DefaultModel() != "deepseek-v4-flash" {
		t.Fatalf("reloaded provider = %+v found=%v", p, ok)
	}
	if changed, err := ApplyUserConfigUpgradesOnStartup(path); err != nil || changed {
		t.Fatalf("second startup rewrote config: changed=%v err=%v", changed, err)
	}
}

func TestCurrentConfigRepairAddsMissingOfficialBaseURL(t *testing.T) {
	for _, inline := range []bool{false, true} {
		t.Run(fmt.Sprintf("inline=%v", inline), func(t *testing.T) {
			provider := `[[providers]]
name = "deepseek-anthropic"
preset_id = "deepseek-anthropic"
kind = "responses"
request_url = "https://api.deepseek.com/anthropic/v1/messages"
`
			if inline {
				provider = `providers = [{ name = "deepseek-anthropic", preset_id = "deepseek-anthropic", kind = "responses", request_url = "https://api.deepseek.com/anthropic/v1/messages" }]
`
			}
			raw := fmt.Sprintf("config_version = %d\n%s", Default().ConfigVersion, provider)
			path := filepath.Join(t.TempDir(), "config.toml")
			if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
				t.Fatal(err)
			}
			if changed, err := ApplyUserConfigUpgradesOnStartup(path); err != nil || !changed {
				t.Fatalf("repair: changed=%v err=%v", changed, err)
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(got), `kind = "anthropic"`) ||
				!strings.Contains(string(got), `base_url = "https://api.deepseek.com/anthropic"`) {
				t.Fatalf("missing repaired protocol fields:\n%s", got)
			}
			loaded := LoadForEdit(path)
			entry, ok := loaded.Provider("deepseek-anthropic")
			if !ok || entry.Kind != "anthropic" ||
				ProviderEffectiveRequestURL(entry) != "https://api.deepseek.com/anthropic/v1/messages" {
				t.Fatalf("reloaded provider = %+v found=%v", entry, ok)
			}
		})
	}
}
