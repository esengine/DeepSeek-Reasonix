package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// legacyHome points HOME / config-dir / .env resolution at a fresh temp tree and
// returns the legacy config.json path and the v1+ dest config path.
func legacyHome(t *testing.T) (src, dest, home string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("REASONIX_CREDENTIALS_STORE", "file")
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

func TestMigrateMCPToUserConfigOnUpgradeCollectsKnownSources(t *testing.T) {
	srcJSON, dest, _ := legacyHome(t)
	writeLegacy(t, srcJSON, `{
		"mcpServers": {
			"legacy-json": {"command": "legacy-json-bin"},
			"disabled-json": {"command": "disabled-json-bin", "disabled": true},
			"project-json": {"command": "legacy-json-should-not-win"},
			"global": {"command": "legacy-should-not-win"}
		}
	}`)
	writeLegacy(t, filepath.Join(filepath.Dir(dest), "reasonix.toml"), `
[[plugins]]
name = "legacy-toml"
command = "legacy-toml-bin"
`)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte(`
[[plugins]]
name = "global"
command = "global-bin"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	projectTOML := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectTOML, "reasonix.toml"), []byte(`
[[plugins]]
name = "project-toml"
command = "project-toml-bin"

[[plugins]]
name = "global"
command = "project-should-not-win"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	projectJSON := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectJSON, ".mcp.json"), []byte(`{
		"mcpServers": {
			"project-json": {"command": "project-json-bin"}
		}
	}`), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := MigrateMCPToUserConfigOnUpgrade([]string{projectTOML, projectJSON, projectTOML})
	if err != nil {
		t.Fatalf("MigrateMCPToUserConfigOnUpgrade: %v", err)
	}
	if res == nil || res.Added != 5 {
		t.Fatalf("migration result = %+v, want 5 added", res)
	}
	cfg := LoadForEdit(dest)
	byName := map[string]PluginEntry{}
	for _, p := range cfg.Plugins {
		byName[p.Name] = p
	}
	for name, command := range map[string]string{
		"global":        "global-bin",
		"disabled-json": "disabled-json-bin",
		"legacy-toml":   "legacy-toml-bin",
		"legacy-json":   "legacy-json-bin",
		"project-toml":  "project-toml-bin",
		"project-json":  "project-json-bin",
	} {
		if byName[name].Command != command {
			t.Fatalf("%s command = %q, want %q; plugins=%+v", name, byName[name].Command, command, cfg.Plugins)
		}
	}
	if p := byName["disabled-json"]; p.AutoStart == nil || *p.AutoStart {
		t.Fatalf("disabled legacy MCP should migrate with auto_start=false: %+v", p)
	}
	if _, err := os.Stat(mcpGlobalMigrationMarkerPath()); err != nil {
		t.Fatalf("migration marker missing: %v", err)
	}

	lateProject := t.TempDir()
	if err := os.WriteFile(filepath.Join(lateProject, "reasonix.toml"), []byte(`
[[plugins]]
name = "late"
command = "late-bin"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err = MigrateMCPToUserConfigOnUpgrade([]string{lateProject})
	if err != nil {
		t.Fatalf("second migration: %v", err)
	}
	if res != nil {
		t.Fatalf("second migration result = %+v, want nil due marker", res)
	}
	if got := LoadForEdit(dest); len(got.Plugins) != len(cfg.Plugins) {
		t.Fatalf("second migration changed plugins: %+v", got.Plugins)
	}
}

func TestMigrateMCPToUserConfigOnUpgradeDoesNotMarkEmptyScan(t *testing.T) {
	_, _, _ = legacyHome(t)
	res, err := MigrateMCPToUserConfigOnUpgrade(nil)
	if err != nil {
		t.Fatalf("MigrateMCPToUserConfigOnUpgrade: %v", err)
	}
	if res != nil {
		t.Fatalf("result = %+v, want nil", res)
	}
	if _, err := os.Stat(mcpGlobalMigrationMarkerPath()); !os.IsNotExist(err) {
		t.Fatalf("empty scan should not write marker, stat err=%v", err)
	}
}

func TestMigrateMCPToUserConfigOnUpgradeRefusesMalformedGlobalConfig(t *testing.T) {
	srcJSON, dest, _ := legacyHome(t)
	writeLegacy(t, srcJSON, `{
		"mcpServers": {
			"legacy-json": {"command": "legacy-json-bin"}
		}
	}`)
	const malformed = "[[plugins]\nname = \"broken\"\n"
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte(malformed), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := MigrateMCPToUserConfigOnUpgrade(nil)
	if err == nil {
		t.Fatal("expected malformed global config to abort MCP migration")
	}
	if res != nil {
		t.Fatalf("result = %+v, want nil", res)
	}
	if got, readErr := os.ReadFile(dest); readErr != nil {
		t.Fatalf("read dest: %v", readErr)
	} else if string(got) != malformed {
		t.Fatalf("malformed config was overwritten:\n%s", got)
	}
	if _, statErr := os.Stat(mcpGlobalMigrationMarkerPath()); !os.IsNotExist(statErr) {
		t.Fatalf("failed migration must not write marker, stat err=%v", statErr)
	}
}

func TestMigrateMCPToUserConfigOnUpgradePreservesConfigVersion(t *testing.T) {
	_, dest, _ := legacyHome(t)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte("config_version = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "reasonix.toml"), []byte(`
[[plugins]]
name = "project"
command = "project-bin"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := MigrateMCPToUserConfigOnUpgrade([]string{project})
	if err != nil {
		t.Fatalf("MigrateMCPToUserConfigOnUpgrade: %v", err)
	}
	if res == nil || res.Added != 1 {
		t.Fatalf("result = %+v, want 1 added", res)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if !strings.Contains(string(got), "config_version = 1") {
		t.Fatalf("MCP migration should not advance config_version:\n%s", got)
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
	for _, want := range []string{`config_version = 3`, `[desktop]`, `close_behavior = "quit"`, `name    = "legacy-v1"`} {
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

func TestLoadFallsBackToLegacyOSConfigWhenPrimaryMissing(t *testing.T) {
	_, dest, _ := legacyHome(t)
	legacy := legacyUserConfigPath()
	if legacy == "" {
		t.Skip("legacy OS config path matches primary path on this platform")
	}
	if err := os.MkdirAll(filepath.Dir(legacy), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte(`default_model = "legacy-provider/legacy-model"`), 0o644); err != nil {
		t.Fatal(err)
	}

	if source := SourcePath(); source != legacy {
		t.Fatalf("SourcePath() = %q, want legacy path %q", source, legacy)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DefaultModel != "legacy-provider/legacy-model" {
		t.Fatalf("DefaultModel = %q, want legacy value", cfg.DefaultModel)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatalf("Load fallback should not create primary config, stat err=%v", err)
	}
}

func TestLoadPrefersPrimaryConfigOverLegacyOSConfig(t *testing.T) {
	_, dest, _ := legacyHome(t)
	legacy := legacyUserConfigPath()
	if legacy == "" {
		t.Skip("legacy OS config path matches primary path on this platform")
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte(`default_model = "primary-provider/primary-model"`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(legacy), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte(`default_model = "legacy-provider/legacy-model"`), 0o644); err != nil {
		t.Fatal(err)
	}

	if source := SourcePath(); source != dest {
		t.Fatalf("SourcePath() = %q, want primary path %q", source, dest)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DefaultModel != "primary-provider/primary-model" {
		t.Fatalf("DefaultModel = %q, want primary value", cfg.DefaultModel)
	}
}

func TestMigrateImportsLegacyOSConfigToPrimaryConfig(t *testing.T) {
	_, dest, _ := legacyHome(t)
	legacy := legacyUserConfigPath()
	if legacy == "" {
		t.Skip("legacy OS config path matches primary path on this platform")
	}
	if err := os.MkdirAll(filepath.Dir(legacy), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte(`
default_model = "legacy-provider/legacy-model"

[[plugins]]
name = "legacy-os"
command = "legacy-os-bin"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := MigrateLegacyIfNeeded()
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if res == nil || res.From != legacy || res.To != dest {
		t.Fatalf("migration result = %+v, want %s -> %s", res, legacy, dest)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read migrated config: %v", err)
	}
	if !strings.Contains(string(got), `name    = "legacy-os"`) {
		t.Fatalf("dest missing legacy plugin:\n%s", got)
	}
	if _, err := os.Stat(legacy); err != nil {
		t.Fatalf("legacy file must remain untouched: %v", err)
	}
}

func TestMigrateImportsLegacyXDGConfigToPrimaryConfig(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("legacy XDG paths are Unix-only")
	}
	_, dest, home := legacyHome(t)
	legacy := filepath.Join(home, ".config", "reasonix", "config.toml")
	if samePath(legacy, dest) {
		t.Skip("legacy XDG config path matches primary path on this platform")
	}
	if err := os.MkdirAll(filepath.Dir(legacy), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte(`
default_model = "legacy-xdg-provider/legacy-xdg-model"

[[plugins]]
name = "legacy-xdg"
command = "legacy-xdg-bin"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := MigrateLegacyIfNeeded()
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if res == nil || res.From != legacy || res.To != dest {
		t.Fatalf("migration result = %+v, want %s -> %s", res, legacy, dest)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read migrated config: %v", err)
	}
	if !strings.Contains(string(got), `name    = "legacy-xdg"`) {
		t.Fatalf("dest missing legacy XDG plugin:\n%s", got)
	}
}

func TestMigrateImportsLegacyCredentialsEvenWhenPrimaryConfigExists(t *testing.T) {
	_, dest, _ := legacyHome(t)
	legacy := legacyUserConfigPath()
	if legacy == "" {
		t.Skip("legacy OS config path matches primary path on this platform")
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte(`default_model = "deepseek-flash/deepseek-chat"`), 0o644); err != nil {
		t.Fatal(err)
	}
	legacyCred := filepath.Join(filepath.Dir(legacy), "credentials")
	if err := os.MkdirAll(filepath.Dir(legacyCred), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacyCred, []byte("DEEPSEEK_API_KEY=sk-old-creds\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	res, err := MigrateLegacyIfNeeded()
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if res != nil {
		t.Fatalf("primary config exists, config migration should be skipped, got %+v", res)
	}
	data, err := os.ReadFile(UserCredentialsPath())
	if err != nil {
		t.Fatalf("read migrated credentials: %v", err)
	}
	if string(data) != "DEEPSEEK_API_KEY=sk-old-creds\n" {
		t.Fatalf("migrated credentials = %q", data)
	}
}

func TestMigrateImportsLegacyKeyringCredentials(t *testing.T) {
	legacyHome(t)
	old := legacyKeyringCredentialValueLookup
	legacyKeyringCredentialValueLookup = func(key string) (string, bool) {
		if key == "DEEPSEEK_API_KEY" {
			return "sk-old-keyring", true
		}
		return "", false
	}
	t.Cleanup(func() { legacyKeyringCredentialValueLookup = old })

	res, err := MigrateLegacyIfNeeded()
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if res != nil {
		t.Fatalf("no config migration should be needed, got %+v", res)
	}
	data, err := os.ReadFile(UserCredentialsPath())
	if err != nil {
		t.Fatalf("read migrated credentials: %v", err)
	}
	if string(data) != "DEEPSEEK_API_KEY=sk-old-keyring\n" {
		t.Fatalf("migrated credentials = %q", data)
	}
}

func TestMigrateSkipsLegacyCredentialsAlreadyInCurrentAutoStore(t *testing.T) {
	_, dest, _ := legacyHome(t)
	t.Setenv("REASONIX_CREDENTIALS_STORE", "")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte(`default_model = "deepseek-flash/deepseek-chat"`), 0o644); err != nil {
		t.Fatal(err)
	}
	currentCred := UserCredentialsPath()
	if err := os.MkdirAll(filepath.Dir(currentCred), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(currentCred, []byte("DEEPSEEK_API_KEY=sk-current\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	legacyPaths := legacyCredentialsPaths()
	if len(legacyPaths) == 0 {
		t.Skip("no legacy credentials path on this platform")
	}
	legacyCred := legacyPaths[0]
	if err := os.MkdirAll(filepath.Dir(legacyCred), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacyCred, []byte("DEEPSEEK_API_KEY=sk-stale\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	res, err := MigrateLegacyIfNeeded()
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if res != nil {
		t.Fatalf("primary config exists, config migration should be skipped, got %+v", res)
	}
	data, err := os.ReadFile(currentCred)
	if err != nil {
		t.Fatalf("read current credentials: %v", err)
	}
	if string(data) != "DEEPSEEK_API_KEY=sk-current\n" {
		t.Fatalf("current credentials were overwritten: %q", data)
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

func TestMigrateSupportData(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping since legacyOSSupportDir equals current reasonixHomeDir on Windows")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("REASONIX_CREDENTIALS_STORE", "file")
	t.Setenv("USERPROFILE", home)
	t.Setenv("AppData", filepath.Join(home, "AppData"))

	legacyConf := legacyUserConfigPath()
	if legacyConf == "" {
		t.Skip("skipping because legacy config path is empty")
	}
	legacyDir := filepath.Dir(legacyConf)

	// Write data to the legacy support directory
	filesToWrite := map[string]string{
		"config.toml":                  "language = \"zh\"",
		"hooks.json":                   `{"hook":"test"}`,
		"sessions/s1.json":             `{"id":"s1"}`,
		"projects/p1/sessions/s2.json": `{"id":"s2"}`,
		"skills/custom.md":             `custom skill`,
		"archive/a1.json":              `{"compacted": true}`,
	}
	for rel, content := range filesToWrite {
		path := filepath.Join(legacyDir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(filepath.Join(legacyDir, "sessions"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(filepath.Join(legacyDir, "sessions", "s1.json"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(filepath.Join(legacyDir, "hooks.json"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	res, err := MigrateLegacyIfNeeded()
	if err != nil {
		t.Fatalf("MigrateLegacyIfNeeded failed: %v", err)
	}
	if res == nil {
		t.Fatal("expected migration result, got nil")
	}

	newDir := filepath.Dir(userConfigPath())
	for rel, expectedContent := range filesToWrite {
		if rel == "config.toml" {
			continue
		}
		newPath := filepath.Join(newDir, rel)
		data, err := os.ReadFile(newPath)
		if err != nil {
			t.Errorf("expected file %s to be migrated, but got error: %v", rel, err)
			continue
		}
		if string(data) != expectedContent {
			t.Errorf("file %s content mismatch: got %q, want %q", rel, string(data), expectedContent)
		}
	}
	if runtime.GOOS != "windows" {
		for _, check := range []struct {
			rel  string
			perm os.FileMode
		}{
			{rel: "sessions", perm: 0o700},
			{rel: "sessions/s1.json", perm: 0o600},
			{rel: "hooks.json", perm: 0o600},
		} {
			info, err := os.Stat(filepath.Join(newDir, check.rel))
			if err != nil {
				t.Fatalf("stat migrated %s: %v", check.rel, err)
			}
			if got := info.Mode().Perm(); got != check.perm {
				t.Fatalf("migrated %s mode = %o, want %o", check.rel, got, check.perm)
			}
		}
	}
}
