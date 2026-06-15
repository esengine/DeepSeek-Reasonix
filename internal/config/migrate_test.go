package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// legacyHome points HOME / config-dir / .env resolution at a fresh temp tree and
// returns the legacy config.json path and the v1+ dest config path.
func legacyHome(t *testing.T) (src, dest, home string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)                               // os.UserHomeDir on Windows
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config")) // os.UserConfigDir on Linux
	t.Setenv("AppData", filepath.Join(home, "AppData"))         // os.UserConfigDir on Windows
	return filepath.Join(home, ".reasonix", "config.json"), userConfigPath(), home
}

func writeLegacy(t *testing.T, src, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(src), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestMigrateImportsKeyPluginsAndLang(t *testing.T) {
	src, dest, home := legacyHome(t)
	writeLegacy(t, src, `{
		"apiKey": "sk-legacy-123",
		"model": "deepseek-v4-pro",
		"lang": "zh",
		"mcpServers": {
			"fs": {"command": "npx", "args": ["-y", "server-fs"], "type": "stdio"},
			"stripe": {"type": "http", "url": "https://mcp.stripe.com", "disabled": true}
		},
		"mcpEnv": {"fs": {"ROOT": "/tmp"}}
	}`)

	res, err := MigrateLegacyIfNeeded()
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if res == nil {
		t.Fatal("expected a migration result")
	} else if !res.KeyToEnv || res.Plugins != 2 {
		t.Errorf("result = %+v, want KeyToEnv=true Plugins=2", res)
	}

	envData, err := os.ReadFile(UserCredentialsPath())
	if err != nil {
		t.Fatalf("read credentials: %v", err)
	}
	if !strings.Contains(string(envData), "DEEPSEEK_API_KEY=sk-legacy-123") {
		t.Errorf("credentials missing key: %q", envData)
	}
	if _, err := os.Stat(filepath.Join(home, ".env")); !os.IsNotExist(err) {
		t.Errorf("migration must not write the user's ~/.env, stat err=%v", err)
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read dest config: %v", err)
	}
	toml := string(got)
	for _, want := range []string{`language      = "zh"`, `[desktop]`, `language = "zh"`, `name    = "fs"`, `name    = "stripe"`, `type    = "http"`, `auto_start = false`} {
		if !strings.Contains(toml, want) {
			t.Errorf("dest config missing %q:\n%s", want, toml)
		}
	}
	if !strings.Contains(toml, `default_model = "deepseek-pro/deepseek-v4-pro"`) {
		t.Errorf("dest config missing imported model:\n%s", toml)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.DefaultModel != "deepseek-pro/deepseek-v4-pro" {
		t.Errorf("DefaultModel = %q, want deepseek-pro/deepseek-v4-pro", loaded.DefaultModel)
	}

	if _, err := os.Stat(src); err != nil {
		t.Errorf("legacy file must be left untouched: %v", err)
	}
}

func TestMigrateImportsLegacyQQConfig(t *testing.T) {
	src, dest, _ := legacyHome(t)
	writeLegacy(t, src, `{
		"qq": {
			"enabled": true,
			"appId": "qq-app-id",
			"appSecret": "qq-secret",
			"sandbox": true,
			"ownerOpenId": " owner-openid ",
			"allowlist": ["owner-openid", " member-openid ", "member-openid", ""]
		}
	}`)

	res, err := MigrateLegacyIfNeeded()
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if res == nil {
		t.Fatal("expected migration result")
	}
	envData, err := os.ReadFile(UserCredentialsPath())
	if err != nil {
		t.Fatalf("read credentials: %v", err)
	}
	if !strings.Contains(string(envData), "QQ_BOT_APP_SECRET=qq-secret") {
		t.Fatalf("credentials missing QQ secret: %q", envData)
	}
	tomlData, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	toml := string(tomlData)
	for _, want := range []string{
		`[bot.qq]`,
		`enabled = true`,
		`app_id = "qq-app-id"`,
		`app_secret_env = "QQ_BOT_APP_SECRET"`,
		`sandbox = true`,
		`qq_users = ["owner-openid", "member-openid"]`,
	} {
		if !strings.Contains(toml, want) {
			t.Fatalf("migrated config missing %q:\n%s", want, toml)
		}
	}
	if strings.Contains(toml, "qq-secret") {
		t.Fatalf("migrated TOML must not contain QQ secret:\n%s", toml)
	}
}

// TestMigrateImportsLegacyMCPStringList covers the pre-mcpServers `mcp` format
// (#3949): `--mcp`-style strings, with mcpEnv/mcpDisabled keyed by name and
// mcpServers winning a name collision.
func TestMigrateImportsLegacyMCPStringList(t *testing.T) {
	src, _, _ := legacyHome(t)
	writeLegacy(t, src, `{
		"mcp": [
			"memory=npx -y @modelcontextprotocol/server-memory",
			"search=https://mcp.example.com/sse",
			"stream=streamable+https://mcp.example.com/http",
			"fs=node old-fs.js",
			"off=npx -y server-off"
		],
		"mcpServers": {"fs": {"command": "npx", "args": ["-y", "server-fs"]}},
		"mcpEnv": {"memory": {"MEMORY_PATH": "/tmp/mem"}},
		"mcpDisabled": ["off"]
	}`)

	if _, err := MigrateLegacyIfNeeded(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	byName := map[string]PluginEntry{}
	for _, p := range cfg.Plugins {
		byName[p.Name] = p
	}
	mem := byName["memory"]
	if mem.Command != "npx" || len(mem.Args) != 2 || mem.Args[1] != "@modelcontextprotocol/server-memory" {
		t.Errorf("memory spec not parsed: %+v", mem)
	}
	if mem.Env["MEMORY_PATH"] != "/tmp/mem" {
		t.Errorf("mcpEnv not applied to memory: %+v", mem.Env)
	}
	if s := byName["search"]; s.Type != "sse" || s.URL != "https://mcp.example.com/sse" {
		t.Errorf("plain URL should migrate as SSE: %+v", s)
	}
	if s := byName["stream"]; s.Type != "http" || s.URL != "https://mcp.example.com/http" {
		t.Errorf("streamable+ URL should migrate as http: %+v", s)
	}
	if fs := byName["fs"]; len(fs.Args) != 2 || fs.Args[1] != "server-fs" {
		t.Errorf("mcpServers should win the fs name collision: %+v", fs)
	}
	if off := byName["off"]; off.AutoStart == nil || *off.AutoStart {
		t.Errorf("mcpDisabled entry should migrate with auto_start=false: %+v", off)
	}
	if len(cfg.Plugins) != 5 {
		t.Errorf("got %d plugins, want 5: %+v", len(cfg.Plugins), cfg.Plugins)
	}
}

func TestMigrateRoundTripsThroughLoad(t *testing.T) {
	src, _, _ := legacyHome(t)
	writeLegacy(t, src, `{"apiKey":"sk-x","mcpServers":{"fs":{"command":"npx","env":{"A":"1"}}}}`)

	if _, err := MigrateLegacyIfNeeded(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(cfg.Plugins) != 1 || cfg.Plugins[0].Name != "fs" || cfg.Plugins[0].Command != "npx" {
		t.Errorf("plugins did not round-trip through Load: %+v", cfg.Plugins)
	}
	if cfg.Plugins[0].Env["A"] != "1" {
		t.Errorf("plugin env lost: %+v", cfg.Plugins[0].Env)
	}
}

func TestMigrateSkipsWhenDestExists(t *testing.T) {
	src, dest, _ := legacyHome(t)
	writeLegacy(t, src, `{"apiKey":"sk-x"}`)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte("default_model = \"deepseek-flash\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := MigrateLegacyIfNeeded()
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if res != nil {
		t.Errorf("must not migrate over an existing v1+ config, got %+v", res)
	}
}

func TestMigrateImportsLegacyV1TOMLBeforeJSON(t *testing.T) {
	srcJSON, dest, _ := legacyHome(t)
	legacyTOML := filepath.Join(filepath.Dir(dest), "reasonix.toml")
	if err := os.MkdirAll(filepath.Dir(legacyTOML), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacyTOML, []byte(`
default_model = "deepseek-flash"
language = "en"

[ui]
theme = "light"
theme_style = "glacier"
close_behavior = "quit"

[[plugins]]
name = "legacy-v1"
command = "legacy-bin"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	writeLegacy(t, srcJSON, `{"apiKey":"sk-json-should-not-win"}`)

	res, err := MigrateLegacyIfNeeded()
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if res == nil || res.From != legacyTOML {
		t.Fatalf("expected v1 TOML migration, got %+v", res)
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read migrated config: %v", err)
	}
	text := string(got)
	for _, want := range []string{`config_version = 2`, `[desktop]`, `close_behavior = "quit"`, `name    = "legacy-v1"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("migrated TOML missing %q:\n%s", want, text)
		}
	}
	if _, err := os.Stat(UserCredentialsPath()); !os.IsNotExist(err) {
		t.Fatalf("v1 TOML migration should not import lower-priority JSON key, credentials stat err=%v", err)
	}
}

func TestMigrateImportsLegacyV1HomeTOMLBeforeJSON(t *testing.T) {
	srcJSON, dest, home := legacyHome(t)
	legacyTOML := filepath.Join(home, ".reasonix", "reasonix.toml")
	if err := os.MkdirAll(filepath.Dir(legacyTOML), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacyTOML, []byte(`
default_model = "deepseek-flash"

[[plugins]]
name = "legacy-home-v1"
command = "legacy-home-bin"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	writeLegacy(t, srcJSON, `{"apiKey":"sk-json-should-not-win","mcpServers":{"json":{"command":"json-bin"}}}`)

	res, err := MigrateLegacyIfNeeded()
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if res == nil || res.From != legacyTOML {
		t.Fatalf("expected home v1 TOML migration, got %+v", res)
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read migrated config: %v", err)
	}
	text := string(got)
	if !strings.Contains(text, `name    = "legacy-home-v1"`) {
		t.Fatalf("home v1 plugin was not migrated:\n%s", text)
	}
	if strings.Contains(text, `name    = "json"`) {
		t.Fatalf("lower-priority v0.5 JSON should not be merged when v1 TOML exists:\n%s", text)
	}
}

func TestMigrateNoLegacyIsNoop(t *testing.T) {
	legacyHome(t)
	res, err := MigrateLegacyIfNeeded()
	if err != nil || res != nil {
		t.Errorf("no legacy install should be a silent no-op, got res=%+v err=%v", res, err)
	}
}

func TestMigrateToleratesUTF8BOM(t *testing.T) {
	src, _, _ := legacyHome(t)
	writeLegacy(t, src, "\ufeff"+`{"apiKey":"sk-bom"}`)
	res, err := MigrateLegacyIfNeeded()
	if err != nil {
		t.Fatalf("a BOM-prefixed legacy config must still parse: %v", err)
	}
	if res == nil || !res.KeyToEnv {
		t.Fatalf("BOM-prefixed config did not migrate: %+v", res)
	}
	data, _ := os.ReadFile(UserCredentialsPath())
	if !strings.Contains(string(data), "DEEPSEEK_API_KEY=sk-bom") {
		t.Errorf("key not migrated from BOM-prefixed config: %q", data)
	}
}

func TestMigrateCustomBaseURLWarns(t *testing.T) {
	src, _, _ := legacyHome(t)
	writeLegacy(t, src, `{"apiKey":"sk-x","baseUrl":"https://my-proxy.example/v1"}`)
	res, err := MigrateLegacyIfNeeded()
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if len(res.Warnings) == 0 {
		t.Error("a non-DeepSeek base_url should produce a warning")
	}
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load migrated config: %v", err)
	}
	for _, name := range []string{"deepseek-flash", "deepseek-pro"} {
		p, ok := cfg.Provider(name)
		if !ok || p.BaseURL != "https://my-proxy.example/v1" {
			t.Fatalf("%s base_url was not migrated: %+v", name, p)
		}
	}
}
