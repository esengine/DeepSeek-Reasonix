package config

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
)

// mcpJSONFile is the project-root file Claude Code calls .mcp.json. Reasonix reads
// it so an MCP server already configured for Claude works here unchanged — the
// server specs map field-for-field onto PluginEntry.
const mcpJSONFile = ".mcp.json"

// mcpServerSpec mirrors one entry of Claude Code's "mcpServers" map. The field
// names and semantics match PluginEntry (and Claude): command/args/env describe
// a local stdio server; type/url/headers describe a remote one.
type mcpServerSpec struct {
	Type      string            `json:"type"`
	Command   string            `json:"command"`
	Args      []string          `json:"args"`
	Env       map[string]string `json:"env"`
	URL       string            `json:"url"`
	Headers   map[string]string `json:"headers"`
	AutoStart *bool             `json:"auto_start"`
}

// loadMCPJSON reads path (Claude Code's .mcp.json) and returns its servers as
// PluginEntry values, sorted by name for a stable connection order. An absent
// file is not an error (returns nil, nil). A present-but-malformed file is an
// error so a typo surfaces loudly instead of silently dropping every server.
func loadMCPJSON(path string) ([]PluginEntry, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("mcp config %s: %w", path, err)
	}
	var doc struct {
		MCPServers map[string]mcpServerSpec `json:"mcpServers"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("mcp config %s: %w", path, err)
	}
	names := make([]string, 0, len(doc.MCPServers))
	for name := range doc.MCPServers {
		names = append(names, name)
	}
	sort.Strings(names)
	entries := make([]PluginEntry, 0, len(names))
	for _, name := range names {
		s := doc.MCPServers[name]
		entries = append(entries, PluginEntry{
			Name:      name,
			Type:      s.Type,
			Command:   s.Command,
			Args:      s.Args,
			Env:       s.Env,
			URL:       s.URL,
			Headers:   s.Headers,
			AutoStart: s.AutoStart,
		})
	}
	return entries, nil
}

// mergeMCPJSON appends servers from .mcp.json that the TOML config did not
// already declare. reasonix.toml's [[plugins]] win on a name collision: it is the
// Reasonix-specific, more explicit of the two, so it overrides the shared,
// checked-in .mcp.json rather than the other way round.
func (c *Config) mergeMCPJSON(entries []PluginEntry) {
	have := make(map[string]bool, len(c.Plugins))
	for _, p := range c.Plugins {
		have[p.Name] = true
	}
	for _, e := range entries {
		if have[e.Name] {
			continue
		}
		have[e.Name] = true
		c.Plugins = append(c.Plugins, e)
	}
}
