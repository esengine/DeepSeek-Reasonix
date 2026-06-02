package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMCPJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, mcpJSONFile)
	doc := `{
  "mcpServers": {
    "stripe": {
      "type": "http",
      "url": "https://mcp.stripe.com",
      "headers": { "Authorization": "Bearer ${STRIPE_KEY}" }
    },
    "filesystem": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"],
      "env": { "FOO": "bar" }
    }
  }
}`
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := loadMCPJSON(path)
	if err != nil {
		t.Fatal(err)
	}
	// Sorted by name: filesystem before stripe.
	if len(got) != 2 || got[0].Name != "filesystem" || got[1].Name != "stripe" {
		t.Fatalf("entries = %+v, want [filesystem stripe] sorted", got)
	}
	fs := got[0]
	if fs.Command != "npx" || len(fs.Args) != 3 || fs.Env["FOO"] != "bar" {
		t.Errorf("filesystem decoded wrong: %+v", fs)
	}
	st := got[1]
	if st.Type != "http" || st.URL != "https://mcp.stripe.com" ||
		st.Headers["Authorization"] != "Bearer ${STRIPE_KEY}" {
		t.Errorf("stripe decoded wrong: %+v", st)
	}
}

func TestLoadMCPJSONAbsentAndMalformed(t *testing.T) {
	dir := t.TempDir()

	// Absent file: not an error, no entries.
	got, err := loadMCPJSON(filepath.Join(dir, "missing.json"))
	if err != nil || got != nil {
		t.Errorf("absent file: got (%v, %v), want (nil, nil)", got, err)
	}

	// Malformed file: an error so a typo surfaces instead of dropping servers.
	bad := filepath.Join(dir, mcpJSONFile)
	if err := os.WriteFile(bad, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadMCPJSON(bad); err == nil {
		t.Error("malformed .mcp.json: want error, got nil")
	}
}

func TestLoadMergesMCPJSON(t *testing.T) {
	// Point the user-config and home dirs at an empty temp dir so Load picks up
	// no global config, then chdir into a project dir holding both files.
	empty := t.TempDir()
	t.Setenv("HOME", empty)
	t.Setenv("XDG_CONFIG_HOME", empty)
	t.Chdir(t.TempDir())

	toml := `[[plugins]]
name = "shared"
command = "local-bin"
`
	if err := os.WriteFile("reasonix.toml", []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	mcp := `{ "mcpServers": {
  "shared": { "type": "http", "url": "https://override.example" },
  "extra":  { "command": "extra-bin", "auto_start": false }
} }`
	if err := os.WriteFile(mcpJSONFile, []byte(mcp), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]PluginEntry{}
	for _, p := range cfg.Plugins {
		byName[p.Name] = p
	}
	if len(byName) != 2 {
		t.Fatalf("plugins = %+v, want shared + extra", cfg.Plugins)
	}
	if byName["shared"].Command != "local-bin" || byName["shared"].URL != "" {
		t.Errorf("reasonix.toml should win the collision, got %+v", byName["shared"])
	}
	if byName["extra"].Command != "extra-bin" {
		t.Errorf("extra not merged from .mcp.json, got %+v", byName["extra"])
	}
	if byName["extra"].AutoStart == nil || *byName["extra"].AutoStart {
		t.Errorf("extra auto_start=false not preserved, got %+v", byName["extra"].AutoStart)
	}
}

func TestLoadMergesPluginsAcrossTOMLSources(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "xdg"))
	t.Setenv("AppData", filepath.Join(root, "AppData")) // os.UserConfigDir reads AppData on Windows
	t.Chdir(t.TempDir())

	gpath := UserConfigPath()
	if gpath == "" {
		t.Fatal("UserConfigPath empty under isolated env")
	}
	if err := os.MkdirAll(filepath.Dir(gpath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(gpath, []byte("[[plugins]]\nname = \"globalmcp\"\ncommand = \"global-bin\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("reasonix.toml", []byte("[[plugins]]\nname = \"projectmcp\"\ncommand = \"project-bin\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, p := range cfg.Plugins {
		names[p.Name] = true
	}
	if !names["globalmcp"] || !names["projectmcp"] {
		t.Fatalf("a project reasonix.toml [[plugins]] dropped the global config's server; got %+v", cfg.Plugins)
	}
}

func TestMergeMCPJSONPrecedence(t *testing.T) {
	// reasonix.toml already declares "shared" (stdio); .mcp.json offers a colliding
	// "shared" (http) plus a fresh "extra". reasonix.toml must win on the collision;
	// "extra" gets appended.
	cfg := &Config{Plugins: []PluginEntry{
		{Name: "shared", Command: "local-bin"},
	}}
	cfg.mergeMCPJSON([]PluginEntry{
		{Name: "shared", Type: "http", URL: "https://override.example"},
		{Name: "extra", Command: "extra-bin"},
	})

	if len(cfg.Plugins) != 2 {
		t.Fatalf("plugins = %+v, want 2 (shared kept, extra added)", cfg.Plugins)
	}
	if cfg.Plugins[0].Name != "shared" || cfg.Plugins[0].Command != "local-bin" || cfg.Plugins[0].URL != "" {
		t.Errorf("collision not won by reasonix.toml: %+v", cfg.Plugins[0])
	}
	if cfg.Plugins[1].Name != "extra" || cfg.Plugins[1].Command != "extra-bin" {
		t.Errorf("non-colliding entry not appended: %+v", cfg.Plugins[1])
	}
}
