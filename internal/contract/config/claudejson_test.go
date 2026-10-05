package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeClaudeJSON(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, claudeJSONFile), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	return filepath.Join(dir, claudeJSONFile)
}

// `claude mcp add` defaults to the per-project map inside ~/.claude.json, so
// reading only .mcp.json found none of what a machine had configured: three
// servers on the reporting user's machine, zero of them in a file Reasonix read.
func TestClaudeConfigSuppliesTheServersItsOwnDefaultScopeHolds(t *testing.T) {
	root := t.TempDir()
	path := writeClaudeJSON(t, `{
	  "mcpServers": {"shared": {"type": "stdio", "command": "user-node"}},
	  "projects": {
	    "`+filepath.ToSlash(root)+`": {
	      "mcpServers": {
	        "shared":  {"type": "stdio", "command": "local-node"},
	        "project": {"type": "stdio", "command": "only-here"}
	      }
	    }
	  }
	}`)

	got := loadClaudeMCP(path, root)
	by := map[string]PluginEntry{}
	for _, e := range got {
		if _, seen := by[e.Name]; !seen {
			by[e.Name] = e
		}
	}
	if len(by) != 2 {
		t.Fatalf("got %d distinct servers, want shared+project: %+v", len(by), got)
	}
	// Claude Code connects the highest scope only; local outranks user.
	if cmd := by["shared"].Command; cmd != "local-node" {
		t.Fatalf("shared resolved to %q, want the project's own entry", cmd)
	}
	if by["shared"].Source != MCPSourceClaudeLocal {
		t.Fatalf("shared source = %q, want %q", by["shared"].Source, MCPSourceClaudeLocal)
	}
	if by["project"].Source != MCPSourceClaudeLocal {
		t.Fatalf("project source = %q", by["project"].Source)
	}
}

// A server the user wrote down for Reasonix is the explicit one; this read is a
// compatibility path and must not take a name away from it.
func TestReasonixConfigurationOutranksTheCompatibilityRead(t *testing.T) {
	cfg := &Config{Plugins: []PluginEntry{
		{Name: "shared", Command: "reasonix-node", Source: MCPSourceUserConfig},
	}}
	cfg.mergeMCPJSON([]PluginEntry{
		{Name: "shared", Command: "claude-node", Source: MCPSourceClaudeLocal},
		{Name: "extra", Command: "claude-node", Source: MCPSourceClaudeUser},
	})
	if len(cfg.Plugins) != 2 {
		t.Fatalf("got %d plugins, want the original plus the new name", len(cfg.Plugins))
	}
	if cfg.Plugins[0].Command != "reasonix-node" {
		t.Fatalf("a Claude entry replaced Reasonix's own: %+v", cfg.Plugins[0])
	}
	if cfg.Plugins[1].Name != "extra" {
		t.Fatalf("a name nothing else claimed was dropped: %+v", cfg.Plugins)
	}
}

// The file belongs to another tool. Reasonix refusing to start over a typo in it
// would be a worse answer than starting without it.
func TestAnUnreadableClaudeConfigIsNotAnError(t *testing.T) {
	if got := loadClaudeMCP(filepath.Join(t.TempDir(), "absent.json"), t.TempDir()); got != nil {
		t.Fatalf("absent file returned %+v", got)
	}
	if got := loadClaudeMCP(writeClaudeJSON(t, `{ not json`), t.TempDir()); got != nil {
		t.Fatalf("malformed file returned %+v", got)
	}
	if got := loadClaudeMCP("", t.TempDir()); got != nil {
		t.Fatalf("empty path returned %+v", got)
	}
}

// Claude Code writes those keys with forward slashes on Windows, where the same
// directory is also spelled with backslashes.
func TestTheProjectKeyIsComparedAsAPathNotAString(t *testing.T) {
	root := t.TempDir()
	path := writeClaudeJSON(t, `{"projects": {"`+filepath.ToSlash(root)+`": {
	  "mcpServers": {"here": {"type": "stdio", "command": "node"}}}}}`)
	if got := loadClaudeMCP(path, filepath.FromSlash(root)); len(got) != 1 {
		t.Fatalf("a path spelled the other way found %d servers", len(got))
	}
	if got := loadClaudeMCP(path, filepath.Join(root, "elsewhere")); len(got) != 0 {
		t.Fatalf("another directory picked up this project's servers: %+v", got)
	}
}

// The effect at the boundary a session actually goes through: a workspace whose
// only MCP configuration is Claude's own default scope still comes up with it.
func TestLoadingAWorkspaceFindsWhatClaudeConfiguredForIt(t *testing.T) {
	root := t.TempDir()
	writeClaudeJSON(t, `{"projects": {"`+filepath.ToSlash(root)+`": {
	  "mcpServers": {"unity-editor-mcp": {"type": "stdio", "command": "node", "args": ["server.js"]}}}}}`)

	cfg, err := LoadForRootReadOnly(root)
	if err != nil {
		t.Fatalf("LoadForRootReadOnly: %v", err)
	}
	var found *PluginEntry
	for i := range cfg.Plugins {
		if cfg.Plugins[i].Name == "unity-editor-mcp" {
			found = &cfg.Plugins[i]
		}
	}
	if found == nil {
		t.Fatalf("a workspace with a Claude-configured server loaded %d plugins and none of them it", len(cfg.Plugins))
	}
	if found.Source != MCPSourceClaudeLocal {
		t.Fatalf("source = %q, want %q", found.Source, MCPSourceClaudeLocal)
	}
	// It carries the user's own authority: they wrote the file it came from.
	if !found.Source.UserAuthorized() || !found.ShouldAutoStart() {
		t.Fatalf("authorized=%v autostart=%v; a server from the user's own file needs neither gate",
			found.Source.UserAuthorized(), found.ShouldAutoStart())
	}
}
