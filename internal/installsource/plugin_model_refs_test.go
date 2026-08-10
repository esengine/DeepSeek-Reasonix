package installsource

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/pluginpkg"
)

const testPluginConfig = `default_model = "plugin/commandcode/commandcode/deepseek-v4-flash"

[[providers]]
name = "deepseek-flash"
kind = "openai"
base_url = "https://api.deepseek.com/v1"
model = "deepseek-v4-flash"
api_key_env = "REASONIX_TEST_KEY"
`

// removePluginPackageFixture installs one plugin package into an isolated
// REASONIX_HOME and writes cfg as the user config. It returns the tool under
// test and the config path.
func removePluginPackageFixture(t *testing.T, cfg string) (*installSourceTool, string) {
	t.Helper()

	home := t.TempDir()
	reasonixHome := filepath.Join(home, ".reasonix")
	if err := os.MkdirAll(filepath.Join(reasonixHome, "plugins", "commandcode"), 0o755); err != nil {
		t.Fatalf("mkdir plugin root: %v", err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("REASONIX_HOME", reasonixHome)
	// Provider entries resolve keys only from Reasonix's global .env, so the
	// fallback provider counts as configured only when the key lives there.
	if err := os.WriteFile(filepath.Join(reasonixHome, ".env"), []byte("REASONIX_TEST_KEY=sk-test\n"), 0o600); err != nil {
		t.Fatalf("write credentials: %v", err)
	}
	oldUserHomeDir := userHomeDir
	userHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { userHomeDir = oldUserHomeDir })

	if err := pluginpkg.Upsert(reasonixHome, pluginpkg.InstalledPlugin{
		Name:    "commandcode",
		Root:    filepath.Join("plugins", "commandcode"),
		Version: "0.1.0",
		Enabled: true,
	}); err != nil {
		t.Fatalf("upsert plugin: %v", err)
	}

	path := filepath.Join(reasonixHome, "config.toml")
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	tl := NewTool(Options{ProjectRoot: t.TempDir()}).(*installSourceTool)
	return tl, path
}

func TestRemovePluginPackageRepairsDefaultModel(t *testing.T) {
	tl, path := removePluginPackageFixture(t, testPluginConfig)

	act := &action{Kind: "plugin", Action: "remove_plugin_package", Name: "commandcode"}
	if err := tl.applyRemovePluginPackage(request{}, act); err != nil {
		t.Fatalf("applyRemovePluginPackage: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if strings.Contains(string(got), "plugin/commandcode/") {
		t.Fatalf("config still references the removed plugin:\n%s", got)
	}
	if !strings.Contains(string(got), `default_model = "deepseek-flash"`) {
		t.Fatalf("default_model was not repaired:\n%s", got)
	}
}

func TestRemovePluginPackageLeavesUnrelatedConfigByteIdentical(t *testing.T) {
	cfg := strings.Replace(testPluginConfig,
		`default_model = "plugin/commandcode/commandcode/deepseek-v4-flash"`,
		`default_model = "deepseek-flash"`, 1)
	tl, path := removePluginPackageFixture(t, cfg)

	act := &action{Kind: "plugin", Action: "remove_plugin_package", Name: "commandcode"}
	if err := tl.applyRemovePluginPackage(request{}, act); err != nil {
		t.Fatalf("applyRemovePluginPackage: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if string(got) != cfg {
		t.Fatalf("config was rewritten although no ref named the plugin:\ngot:\n%s\nwant:\n%s", got, cfg)
	}
}

func TestRemovePluginPackageKeepsMalformedConfig(t *testing.T) {
	const broken = "default_model = \"plugin/commandcode/commandcode/deepseek-v4-flash\"\n[[providers\n"
	tl, path := removePluginPackageFixture(t, broken)

	act := &action{Kind: "plugin", Action: "remove_plugin_package", Name: "commandcode"}
	if err := tl.applyRemovePluginPackage(request{}, act); err != nil {
		t.Fatalf("applyRemovePluginPackage: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if string(got) != broken {
		t.Fatalf("a malformed config must be left for the user to recover:\n%s", got)
	}
	if _, ok, _ := pluginpkg.FindInstalled(tl.reasonixHome, "commandcode"); ok {
		t.Fatal("plugin should still be removed when the config cannot be repaired")
	}
}
