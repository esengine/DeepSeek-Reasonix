package config

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/BurntSushi/toml"

	"reasonix/internal/base/testenv"
)

const botTableFixture = `default_model = "deepseek-flash"

[bot]
enabled = true
model = "deepseek-pro"
max_steps = 21

[bot.allowlist]
enabled = true
qq_users = ["owner-openid", "member-openid"]

[bot.qq]
enabled = true
app_id = "qq-app-id"
app_secret_env = "QQ_BOT_APP_SECRET"
access = { enabled = true, users = ["u1"] }

[[bot.connections]]
id = "feishu-lark"
provider = "feishu"
credential = { app_id = "cli_lark", app_secret_env = "LARK_BOT_APP_SECRET" }
session_mappings = [{ remote_id = "ou_123", session_id = "topic:1" }]

[[bot.routes]]
chat_id = "oc_group"
workspace_root = "/tmp/reasonix-route"
`

func TestBotTableSurvivesAFullRewrite(t *testing.T) {
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	path := UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(botTableFixture), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadForEditReadOnlyStrict(path)
	if err != nil {
		t.Fatalf("a config carrying [bot] must still load: %v", err)
	}
	if _, err := LoadForRoot(testenv.TempDir(t)); err != nil {
		t.Fatalf("runtime load with [bot]: %v", err)
	}
	cfg.Agent.Temperature = 0.3
	if err := cfg.SaveTo(path); err != nil {
		t.Fatal(err)
	}

	var want, got struct {
		Bot map[string]any `toml:"bot"`
	}
	if _, err := toml.Decode(botTableFixture, &want); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := toml.Decode(string(raw), &got); err != nil {
		t.Fatalf("rewritten config does not parse: %v\n%s", err, raw)
	}
	if !reflect.DeepEqual(tomlValue(got.Bot), tomlValue(want.Bot)) {
		t.Fatalf("[bot] changed across a rewrite:\n got %#v\nwant %#v\n%s", got.Bot, want.Bot, raw)
	}
}

// tomlValue folds the two spellings of an array of tables — [[t]] and
// t = [{...}] decode to different Go slice types — into one.
func tomlValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = tomlValue(e)
		}
		return out
	case []map[string]any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = tomlValue(e)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = tomlValue(e)
		}
		return out
	default:
		return v
	}
}

func TestBotCredentialVariablesStayCredentials(t *testing.T) {
	var cfg Config
	if _, err := toml.Decode(botTableFixture, &cfg); err != nil {
		t.Fatal(err)
	}
	names := credentialEnvNamesFromConfig(&cfg)
	for _, want := range []string{"QQ_BOT_APP_SECRET", "LARK_BOT_APP_SECRET"} {
		if !slices.Contains(names, want) {
			t.Fatalf("credential names %v lost %s named under [bot]", names, want)
		}
	}
}

func TestNoBotTableIsNotInvented(t *testing.T) {
	out := RenderTOMLForScope(Default(), RenderScopeUser)
	var doc map[string]toml.Primitive
	if _, err := toml.Decode(out, &doc); err != nil {
		t.Fatal(err)
	}
	if _, ok := doc["bot"]; ok {
		t.Fatalf("a config without [bot] rendered one:\n%s", out)
	}
}
