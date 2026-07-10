package config

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/pluginpkg"
)

func TestLoadMergesInstalledPluginSkillRootsAndMCP(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	root := filepath.Join(home, "plugins", "superpowers")
	writeConfigTestFile(t, filepath.Join(root, pluginpkg.NativeManifest), `{
  "name": "superpowers",
  "version": "1.0.0",
  "skills": "skills",
  "mcpServers": {
    "helper": { "command": "bin/helper" }
  }
}`)
	if err := pluginpkg.Upsert(home, pluginpkg.InstalledPlugin{
		Name:         "superpowers",
		Root:         "plugins/superpowers",
		Version:      "1.0.0",
		ManifestKind: "reasonix",
		Enabled:      true,
	}); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadForRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Skills.Paths) == 0 || cfg.Skills.Paths[len(cfg.Skills.Paths)-1] != filepath.Join(root, "skills") {
		t.Fatalf("skills paths = %#v", cfg.Skills.Paths)
	}
	var found bool
	for _, p := range cfg.Plugins {
		if p.Name == "helper" {
			found = true
			if p.Command != filepath.Join(root, "bin", "helper") {
				t.Fatalf("plugin command = %q", p.Command)
			}
			if p.Env["REASONIX_PLUGIN_NAME"] != "superpowers" {
				t.Fatalf("plugin env = %#v", p.Env)
			}
		}
	}
	if !found {
		t.Fatalf("plugin MCP server missing: %#v", cfg.Plugins)
	}
	if owner, ok := cfg.PluginPackageOwner("helper"); !ok || owner != "superpowers" {
		t.Fatalf("plugin MCP owner = %q, %v; want superpowers, true", owner, ok)
	}
}

func writeConfigTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
