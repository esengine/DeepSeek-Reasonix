package config

import (
	"encoding/json"
	"os"
	"path/filepath"

	fileencoding "reasonix/internal/base/fileutil/encoding"
)

// Claude Code writes MCP servers to three places, and .mcp.json — the only one
// Reasonix read — is the one `claude mcp add` does not use by default. Its
// default is the per-project map inside the user's own ~/.claude.json, so a
// machine can hold every server it has there and none where Reasonix looked.
const claudeJSONFile = ".claude.json"

const (
	MCPSourceClaudeLocal MCPConfigSource = "claude_local" // ~/.claude.json, this project
	MCPSourceClaudeUser  MCPConfigSource = "claude_user"  // ~/.claude.json, every project
)

// claudeConfigPath is where Claude Code keeps that file. CLAUDE_CONFIG_DIR
// moves it; a pinned Reasonix home means a test or a sandbox that must not
// reach the real one.
func (r Roots) claudeConfigPath() string {
	if r.pinnedHomeDir() != "" {
		return ""
	}
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, claudeJSONFile)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, claudeJSONFile)
}

// loadClaudeMCP returns the servers Claude Code holds for this workspace: the
// per-project map, then the user-wide one. Both live in a file only the user
// writes, unlike a repo's .mcp.json, which arrives with a clone. An unreadable
// or malformed file yields nothing rather than failing the load — it belongs to
// another tool, and refusing to start over a typo in it is the worse answer.
func loadClaudeMCP(path, root string) []PluginEntry {
	if path == "" {
		return nil
	}
	resolved, err := resolveConfigAccessPath(path, false)
	if err != nil {
		return nil
	}
	b, err := fileencoding.ReadFileUTF8(resolved)
	if err != nil {
		return nil
	}
	var doc struct {
		MCPServers map[string]mcpServerSpec `json:"mcpServers"`
		Projects   map[string]claudeProject `json:"projects"`
	}
	if json.Unmarshal(b, &doc) != nil {
		return nil
	}
	// Claude Code's own order: the project's own servers outrank the user-wide
	// ones under the same name.
	out := specsToEntries(claudeProjectServers(doc.Projects, root), MCPSourceClaudeLocal)
	return append(out, specsToEntries(doc.MCPServers, MCPSourceClaudeUser)...)
}

// claudeProject is one entry of Claude Code's per-project map. Only its servers
// matter here; the rest of what it holds is that tool's business.
type claudeProject struct {
	MCPServers map[string]mcpServerSpec `json:"mcpServers"`
}

// claudeProjectServers finds the entry for root. Claude Code writes these keys
// as absolute paths in its own spelling — forward slashes on Windows, where the
// same directory is also spelled with backslashes — so they are compared as
// paths rather than as strings.
func claudeProjectServers(projects map[string]claudeProject, root string) map[string]mcpServerSpec {
	want, err := filepath.Abs(root)
	if err != nil {
		return nil
	}
	for key, project := range projects {
		if samePath(key, want) {
			return project.MCPServers
		}
	}
	return nil
}
