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
	}
	if !res.KeyToEnv || res.Plugins != 2 {
		t.Errorf("result = %+v, want KeyToEnv=true Plugins=2", res)
	}

	envData, err := os.ReadFile(filepath.Join(home, ".env"))
	if err != nil {
		t.Fatalf("read ~/.env: %v", err)
	}
	if !strings.Contains(string(envData), "DEEPSEEK_API_KEY=sk-legacy-123") {
		t.Errorf("~/.env missing key: %q", envData)
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read dest config: %v", err)
	}
	toml := string(got)
	for _, want := range []string{`language      = "zh"`, `name    = "fs"`, `name    = "stripe"`, `type    = "http"`, `auto_start = false`} {
		if !strings.Contains(toml, want) {
			t.Errorf("dest config missing %q:\n%s", want, toml)
		}
	}

	if _, err := os.Stat(src); err != nil {
		t.Errorf("legacy file must be left untouched: %v", err)
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

func TestMigrateNoLegacyIsNoop(t *testing.T) {
	legacyHome(t)
	res, err := MigrateLegacyIfNeeded()
	if err != nil || res != nil {
		t.Errorf("no legacy install should be a silent no-op, got res=%+v err=%v", res, err)
	}
}

func TestMigrateToleratesUTF8BOM(t *testing.T) {
	src, _, home := legacyHome(t)
	writeLegacy(t, src, "\ufeff"+`{"apiKey":"sk-bom"}`)
	res, err := MigrateLegacyIfNeeded()
	if err != nil {
		t.Fatalf("a BOM-prefixed legacy config must still parse: %v", err)
	}
	if res == nil || !res.KeyToEnv {
		t.Fatalf("BOM-prefixed config did not migrate: %+v", res)
	}
	data, _ := os.ReadFile(filepath.Join(home, ".env"))
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
}
