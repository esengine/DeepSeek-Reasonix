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
	owners := cfg.PluginPackageSkillOwners()[CanonicalSkillPath(filepath.Join(root, "skills"))]
	if len(owners) != 1 || owners[0] != "superpowers" {
		t.Fatalf("plugin skill owners = %#v, want superpowers", owners)
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

func TestClaudePackageMCPExpandsRootAndDoesNotAutoStart(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	root := filepath.Join(home, "plugins", "claude-mcp")
	writeConfigTestFile(t, filepath.Join(root, pluginpkg.ClaudeManifest), `{"name":"claude-mcp"}`)
	writeConfigTestFile(t, filepath.Join(root, ".mcp.json"), `{
  "mcpServers": {
    "Local Search": {
      "command": "${CLAUDE_PLUGIN_ROOT}/bin/server",
      "args": ["--root", "${CLAUDE_PLUGIN_ROOT}/data", "--workspace", "${CLAUDE_PROJECT_DIR}"],
      "env": {"DATA_DIR": "${CLAUDE_PLUGIN_ROOT}/data"}
    }
  }
}`)
	if err := pluginpkg.Upsert(home, pluginpkg.InstalledPlugin{Name: "claude-mcp", Root: "plugins/claude-mcp", ManifestKind: "claude", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	cfg, err := LoadForRoot(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Plugins) != 1 {
		t.Fatalf("plugins = %#v", cfg.Plugins)
	}
	got := cfg.Plugins[0]
	if got.Command != filepath.Join(root, "bin", "server") || got.Args[1] != filepath.Join(root, "data") || got.Env["DATA_DIR"] != filepath.Join(root, "data") {
		t.Fatalf("Claude root was not expanded: %#v", got)
	}
	if got.Env["CLAUDE_PLUGIN_ROOT"] != root {
		t.Fatalf("CLAUDE_PLUGIN_ROOT = %q", got.Env["CLAUDE_PLUGIN_ROOT"])
	}
	if got.Args[3] != workspace || got.Env["CLAUDE_PROJECT_DIR"] != workspace {
		t.Fatalf("workspace expansion = %#v", got)
	}
	if got.ShouldAutoStart() {
		t.Fatal("imported Claude MCP must require an explicit connection")
	}
	if len(cfg.AutoStartPlugins()) != 0 {
		t.Fatalf("auto-start plugins = %#v", cfg.AutoStartPlugins())
	}
}

func TestClaudePackageMCPDeduplicatesSameConnectionAcrossPackages(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	for i, name := range []string{"legal-one", "legal-two"} {
		root := filepath.Join(home, "plugins", name)
		writeConfigTestFile(t, filepath.Join(root, pluginpkg.ClaudeManifest), `{"name":"`+name+`"}`)
		writeConfigTestFile(t, filepath.Join(root, ".mcp.json"), `{
  "mcpServers":{"飞书":{"type":"http","url":"https://open.feishu.cn/mcp","description":"package `+string(rune('A'+i))+` description"}}
}`)
		if err := pluginpkg.Upsert(home, pluginpkg.InstalledPlugin{Name: name, Root: "plugins/" + name, ManifestKind: "claude", Enabled: true}); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := LoadForRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Plugins) != 1 || cfg.Plugins[0].URL != "https://open.feishu.cn/mcp" {
		t.Fatalf("deduplicated plugins = %#v", cfg.Plugins)
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

// TestCommandDirsIncludePluginPackageCommands pins the plugin-commands wiring:
// an enabled plugin package's command roots join command discovery at the
// before user/project entries, retaining package ownership, while a disabled
// package contributes nothing.
func TestCommandDirsIncludePluginPackageCommands(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	root := filepath.Join(home, "plugins", "pwf")
	writeConfigTestFile(t, filepath.Join(root, pluginpkg.ClaudeManifest), `{"name": "pwf"}`)
	writeConfigTestFile(t, filepath.Join(root, "skills", "planner", "SKILL.md"), "---\ndescription: p\n---\nbody")
	writeConfigTestFile(t, filepath.Join(root, "commands", "plan.md"), "---\ndescription: plan\n---\nPlan: $ARGUMENTS")
	if err := pluginpkg.Upsert(home, pluginpkg.InstalledPlugin{
		Name:         "pwf",
		Root:         "plugins/pwf",
		ManifestKind: "claude",
		Enabled:      true,
	}); err != nil {
		t.Fatal(err)
	}

	dirs := CommandDirsForRoot(t.TempDir())
	want := filepath.Join(root, "commands")
	if len(dirs) == 0 || dirs[0] != want {
		t.Fatalf("CommandDirsForRoot = %#v, want plugin commands dir first (lowest priority): %s", dirs, want)
	}
	roots := CommandRootsForRoot(t.TempDir())
	if len(roots) == 0 || roots[0].Path != want || roots[0].Plugin != "pwf" {
		t.Fatalf("CommandRootsForRoot = %#v, want plugin ownership on first root", roots)
	}

	if err := pluginpkg.SetEnabled(home, "pwf", false); err != nil {
		t.Fatal(err)
	}
	for _, dir := range CommandDirsForRoot(t.TempDir()) {
		if dir == want {
			t.Fatalf("disabled plugin's commands dir must not join discovery: %#v", dir)
		}
	}
}

// TestCommandDirsWithoutPluginState keeps the no-plugin fast path intact.
func TestCommandDirsWithoutPluginState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	if dirs := CommandDirsForRoot(t.TempDir()); len(dirs) == 0 {
		t.Fatal("CommandDirsForRoot must still return the conventional dirs")
	}
}
