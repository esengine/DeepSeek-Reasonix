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
	// Concurrency is "parallel" (default) or "serial"; see SPEC 3.16.
	Concurrency string `toml:"concurrency"`
	// AutoStart controls whether the server connects during session startup.
	// Nil preserves historical behavior: configured servers start automatically.
	AutoStart *bool `toml:"auto_start"`
	// Tier is a legacy compatibility field. New config rendering omits it; enabled
	// MCP servers connect automatically in the background unless auto_start=false.
	// Historical values are accepted for old files:
	//   "eager"      — blocks startup until the handshake completes; required for
	//                  servers whose tools the system prompt depends on.
	//   "lazy"       — legacy alias for background.
	//   "background" — placeholder + spawn fired at boot but not waited on;
	//                  swap happens once the spawn finishes.
	// Empty defaults to "background" so enabled MCPs connect automatically
	// without blocking chat. Unknown non-empty values fall back to "background".
	Tier         string          `toml:"tier"`
	Source       MCPConfigSource `toml:"-" json:"-"`
	expansionEnv map[string]string
}
