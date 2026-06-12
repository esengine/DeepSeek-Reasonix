// Package builtinmcp defines MCP servers that ship with Reasonix without
// requiring user configuration.
package builtinmcp

import "reasonix/internal/config"

const (
	TimeName     = "time"
	Context7Name = "context7"
)

// Entries returns the built-in MCP servers that are always available. They use
// the lazy tier so startup never blocks on package installation or network.
func Entries() []config.PluginEntry {
	return []config.PluginEntry{
		{
			Name:    TimeName,
			Type:    "stdio",
			Command: "uvx",
			Args:    []string{"mcp-server-time"},
			Tier:    "lazy",
		},
		{
			Name:    Context7Name,
			Type:    "stdio",
			Command: "npx",
			Args:    []string{"-y", "@upstash/context7-mcp"},
			Tier:    "lazy",
		},
	}
}

// Entry returns one built-in MCP entry by name.
func Entry(name string) (config.PluginEntry, bool) {
	for _, e := range Entries() {
		if e.Name == name {
			return e, true
		}
	}
	return config.PluginEntry{}, false
}

// IsBuiltIn reports whether name is a Reasonix-shipped MCP server.
func IsBuiltIn(name string) bool {
	_, ok := Entry(name)
	return ok
}

// AppendMissing appends built-in MCP entries unless a configured or
// session-scoped entry with the same name exists. Explicit user and host config
// wins, including auto_start=false.
func AppendMissing(out []config.PluginEntry, configured []config.PluginEntry, reservedNames ...string) []config.PluginEntry {
	seen := make(map[string]bool, len(configured))
	for _, e := range configured {
		seen[e.Name] = true
	}
	for _, name := range reservedNames {
		seen[name] = true
	}
	for _, e := range Entries() {
		if !seen[e.Name] {
			out = append(out, e)
		}
	}
	return out
}
