package plugin

import "reasonix/internal/tool"

// ToolInfo is the human-facing metadata returned by MCP tools/list for one tool.
type ToolInfo struct {
	Name            string
	Description     string
	ReadOnlyHint    bool
	DestructiveHint bool
	SchemaError     string
}

// ServerStatus summarises one connected server for the /mcp command.
type ServerStatus struct {
	Name      string
	Transport string
	// ConfigSource is the config plane that registered this server
	// (user_config, project_config, workspace, built-in, …). Empty when unknown.
	// Surfaced in /mcp status so operators can tell where a tool came from (#6578).
	ConfigSource      string
	Tools             int
	Prompts           int
	Resources         int
	HasTools          bool
	ToolList          []ToolInfo
	ToolBindings      []tool.MCPBinding `json:"-"`
	ProtocolVersion   string
	SessionState      SessionState
	SessionIDPresent  bool
	ReconnectAttempts int
	LastErrorKind     SessionErrorKind
	LastError         string
	// HostProfile is the client-capability profile this host declares
	// ("core-v1", "interactive-v1", "desktop-apps-2026-01-26-v1").
	HostProfile string
	// ElicitationNegotiated reports that the client declared elicitation and
	// the session runs a protocol revision where the server can use it.
	ElicitationNegotiated bool
	// AppsNegotiated reports two-way MCP Apps agreement: the client declared
	// io.modelcontextprotocol/ui and the server answered with the extension.
	AppsNegotiated bool
}
