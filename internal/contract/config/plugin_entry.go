package config

// PluginEntry declares an external MCP server. Type selects the transport:
// "stdio" (default) launches Command/Args/Env as a subprocess; "http"
// (a.k.a. streamable-http) and "sse" connect to a remote URL with optional
// static Headers. String fields support ${VAR} / ${VAR:-default} expansion so
// secrets (bearer tokens, keys) come from the environment, not the file. The
// fields mirror Claude Code's mcpServers spec, so entries can come from either
// reasonix.toml's [[plugins]] or a project-root .mcp.json (see loadMCPJSON).
type PluginEntry struct {
	Name    string            `toml:"name"`
	Type    string            `toml:"type"` // "stdio" (default) | "http" | "sse"
	Command string            `toml:"command"`
	Args    []string          `toml:"args"`
	Env     map[string]string `toml:"env"`
	URL     string            `toml:"url"`
	Headers map[string]string `toml:"headers"`
	// DisabledTools excludes exact raw names returned by this server in tools/list.
	DisabledTools []string `toml:"disabled_tools"`
	// StartupTimeoutSeconds overrides [tools].mcp_startup_timeout_seconds for
	// initialize + tools/list. Zero keeps the global/default cap.
	StartupTimeoutSeconds int `toml:"startup_timeout_seconds"`
	// CallTimeoutSeconds overrides the default per-call deadline for this MCP
	// server. Zero falls back to [tools].mcp_call_timeout_seconds.
	CallTimeoutSeconds int `toml:"call_timeout_seconds"`
	// ToolTimeoutSeconds overrides the per-call deadline for raw MCP tool names
	// from this server. Keys are server-local tool names, not model-visible
	// mcp__server__tool names.
	ToolTimeoutSeconds map[string]int `toml:"tool_timeout_seconds"`
	// OAuthAllowMissingPKCEMetadata lets OAuth proceed with an authorization
	// server that does not advertise code_challenge_methods_supported. PKCE
	// S256 is still sent; it is honoured only from a user-authorized source.
	OAuthAllowMissingPKCEMetadata bool `toml:"oauth_allow_missing_pkce_metadata"`
	// Concurrency is "parallel" (default) or "serial"; see SPEC 3.16.
	Concurrency string `toml:"concurrency"`
	// AutoStart controls whether the server connects during session startup.
	// Nil preserves historical behavior: configured servers start automatically.
	AutoStart *bool `toml:"auto_start"`
	// Load is "always" to put this server's tools in the provider schema from
	// session start, or empty/"deferred" to reach them through use_capability.
	// A [tools.mcp_load] entry for the server overrides it; see MCPAlwaysLoad.
	Load string `toml:"load"`
	// Tier is retired: loading strips it from every config file it reads.
	Tier         string          `toml:"tier"`
	Source       MCPConfigSource `toml:"-" json:"-"`
	expansionEnv map[string]string
}
