package config

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// renderPluginPolicy writes the per-server policy fields both render scopes
// share. They live together so a policy added to one scope cannot be forgotten
// in the other, which would silently drop a user's setting on the next save.
func renderPluginPolicy(b *strings.Builder, pl PluginEntry) {
	if len(pl.DisabledTools) > 0 {
		fmt.Fprintf(b, "disabled_tools = %s\n", renderStringArray(pl.DisabledTools))
	}
	if c := strings.TrimSpace(pl.Concurrency); c != "" {
		fmt.Fprintf(b, "concurrency = %q\n", c)
	}
	if pl.AutoStart != nil {
		fmt.Fprintf(b, "auto_start = %v\n", *pl.AutoStart)
	}
	if pl.DeclaredMCPLoad() == MCPLoadAlways {
		fmt.Fprintf(b, "load = %q\n", MCPLoadAlways)
	}
}

// renderMCPLoadSection writes [tools.mcp_load] when the user has chosen a mode
// for any server.
func renderMCPLoadSection(b *strings.Builder, modes map[string]string) {
	if len(modes) == 0 {
		return
	}
	b.WriteString("[tools.mcp_load]\n")
	for _, name := range slices.Sorted(maps.Keys(modes)) {
		mode, _ := normalizeMCPLoad(modes[name])
		fmt.Fprintf(b, "%s = %q\n", renderTOMLKeyPart(name), mode)
	}
	b.WriteString("\n")
}

// renderPluginOverrides writes a server's own timeouts and OAuth allowance,
// each only when set, so an untouched entry renders as it always has.
func renderPluginOverrides(b *strings.Builder, pl PluginEntry) {
	if pl.StartupTimeoutSeconds > 0 {
		b.WriteString("# Per-server MCP initialize + tools/list timeout; 0 keeps the global/default cap.\n")
		fmt.Fprintf(b, "startup_timeout_seconds = %d\n", pl.StartupTimeoutSeconds)
	}
	if pl.CallTimeoutSeconds > 0 {
		b.WriteString("# Per-server MCP call timeout; 0 keeps the global/default cap.\n")
		fmt.Fprintf(b, "call_timeout_seconds = %d\n", pl.CallTimeoutSeconds)
	}
	if hasPositiveIntMap(pl.ToolTimeoutSeconds) {
		b.WriteString("# Raw MCP tool names with per-tool call timeouts.\n")
		fmt.Fprintf(b, "tool_timeout_seconds = %s\n", renderIntMap(pl.ToolTimeoutSeconds))
	}
	if pl.OAuthAllowMissingPKCEMetadata {
		b.WriteString("# Proceed with an OAuth server that does not advertise PKCE methods (user config only).\n")
		b.WriteString("oauth_allow_missing_pkce_metadata = true\n")
	}
}
