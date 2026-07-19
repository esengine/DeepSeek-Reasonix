// Package capdiag collects read-only capability diagnostics for Skills,
// Commands, Hooks, plugin packages, MCP servers, and instruction docs.
// CLI and desktop share Collect; only CLI --live starts MCP processes.
package capdiag

import (
	"time"

	"reasonix/internal/plugin"
)

// SchemaVersion is the JSON report version. Bump only on breaking shape changes.
const SchemaVersion = 1

// Options configure Collect.
type Options struct {
	// Root is the workspace root (default: current working directory).
	Root string
	// Live starts automatic MCP servers in an isolated Host (CLI only).
	Live bool
	// LiveTimeout is the per-server probe timeout when Live is true.
	LiveTimeout time.Duration
	// RuntimeHost, when set, merges connected/failed/deferred status from an
	// existing Host (desktop active session). Collect never starts MCP when
	// RuntimeHost is set unless Live is also true (desktop passes Live=false).
	RuntimeHost *plugin.Host
	// HomeDir and ReasonixHomeDir override discovery roots (tests).
	HomeDir         string
	ReasonixHomeDir string
}

// Report is the stable capability diagnostics payload.
type Report struct {
	SchemaVersion int                 `json:"schema_version"`
	Root          string              `json:"root"`
	Live          bool                `json:"live"`
	Summary       Summary             `json:"summary"`
	Instructions  InstructionsReport  `json:"instructions"`
	Skills        AssetReport         `json:"skills"`
	Commands      AssetReport         `json:"commands"`
	Hooks         HookReport          `json:"hooks"`
	Plugins       PluginPackageReport `json:"plugins"`
	MCP           MCPReport           `json:"mcp"`
	Issues        []Issue             `json:"issues"`
}

// Summary counts issues and resources.
type Summary struct {
	Errors   int `json:"errors"`
	Warnings int `json:"warnings"`
	Infos    int `json:"infos"`

	Instructions int `json:"instructions"`
	Skills       int `json:"skills"`
	Commands     int `json:"commands"`
	Hooks        int `json:"hooks"`
	Plugins      int `json:"plugins"`
	MCPServers   int `json:"mcp_servers"`
}

// Issue is one diagnostic finding with a stable code.
type Issue struct {
	Severity    string `json:"severity"` // error | warning | info
	Code        string `json:"code"`
	Subsystem   string `json:"subsystem"`
	Name        string `json:"name,omitempty"`
	Source      string `json:"source,omitempty"`
	Message     string `json:"message"`
	Remediation string `json:"remediation,omitempty"`
	SettingsTab string `json:"settings_tab,omitempty"`
}

// InstructionsReport lists loaded instruction/memory docs in load order.
type InstructionsReport struct {
	Docs []InstructionDoc `json:"docs"`
}

// InstructionDoc is one REASONIX.md / AGENTS.md / CLAUDE.md source.
type InstructionDoc struct {
	Path  string `json:"path"`
	Scope string `json:"scope"`
	Order int    `json:"order"`
}

// AssetReport covers skills or commands.
type AssetReport struct {
	Roots       []RootInfo   `json:"roots"`
	Entries     []AssetEntry `json:"entries"`
	Winners     int          `json:"winners"`
	Shadowed    int          `json:"shadowed"`
	Disabled    int          `json:"disabled,omitempty"`
	ParseErrors int          `json:"parse_errors,omitempty"`
}

// RootInfo is one discovery directory.
type RootInfo struct {
	Path   string `json:"path"`
	Scope  string `json:"scope,omitempty"`
	Status string `json:"status"`
}

// AssetEntry is one skill or command candidate.
type AssetEntry struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Scope       string `json:"scope,omitempty"`
	Path        string `json:"path"`
	Status      string `json:"status"` // winner | shadowed | disabled | error
	WinnerPath  string `json:"winner_path,omitempty"`
	Error       string `json:"error,omitempty"`
	RunAs       string `json:"run_as,omitempty"`
}

// HookReport covers hook configuration.
type HookReport struct {
	TrustedProject bool         `json:"trusted_project"`
	ProjectDefines bool         `json:"project_defines_hooks"`
	Sources        []HookSource `json:"sources"`
	Entries        []HookEntry  `json:"entries"`
}

// HookSource is one settings/manifest source.
type HookSource struct {
	Scope      string `json:"scope"`
	Path       string `json:"path"`
	Status     string `json:"status"`
	HookCount  int    `json:"hook_count"`
	ParseError string `json:"parse_error,omitempty"`
}

// HookEntry is one configured hook.
type HookEntry struct {
	Event       string `json:"event"`
	Match       string `json:"match,omitempty"`
	Command     string `json:"command,omitempty"`
	ContextFile string `json:"context_file,omitempty"`
	Description string `json:"description,omitempty"`
	TimeoutMS   int    `json:"timeout_ms,omitempty"`
	Scope       string `json:"scope"`
	Source      string `json:"source"`
	Blocking    bool   `json:"blocking"`
}

// PluginPackageReport covers installed plugin packages.
type PluginPackageReport struct {
	StatePath string              `json:"state_path,omitempty"`
	Packages  []PluginPackageInfo `json:"packages"`
}

// PluginPackageInfo is one installed package.
type PluginPackageInfo struct {
	Name         string   `json:"name"`
	Enabled      bool     `json:"enabled"`
	Version      string   `json:"version,omitempty"`
	Root         string   `json:"root"`
	ManifestKind string   `json:"manifest_kind,omitempty"`
	Skills       int      `json:"skills"`
	Commands     int      `json:"commands"`
	Hooks        int      `json:"hooks"`
	MCPServers   int      `json:"mcp_servers"`
	Warnings     []string `json:"warnings,omitempty"`
	Status       string   `json:"status"` // ok | missing_root | invalid_manifest | disabled
}

// MCPReport covers merged MCP server configuration and optional live/runtime state.
type MCPReport struct {
	Servers []MCPServerInfo `json:"servers"`
}

// MCPServerInfo is one merged MCP server.
type MCPServerInfo struct {
	Name          string        `json:"name"`
	Source        string        `json:"source,omitempty"` // toml | mcp_json | plugin_package
	PackageOwner  string        `json:"package_owner,omitempty"`
	Transport     string        `json:"transport"`
	StartIntent   string        `json:"start_intent"`      // automatic | off
	Command       string        `json:"command,omitempty"` // redacted path form
	URLHost       string        `json:"url_host,omitempty"`
	EnvKeys       []string      `json:"env_keys,omitempty"`
	HeaderKeys    []string      `json:"header_keys,omitempty"`
	RuntimeStatus string        `json:"runtime_status,omitempty"` // connected | failed | deferred | disabled | skipped | probed
	ToolCount     int           `json:"tool_count,omitempty"`
	Tools         []MCPToolInfo `json:"tools,omitempty"`
	Error         string        `json:"error,omitempty"`
}

// MCPToolInfo is one tool discovered during live/runtime probe.
type MCPToolInfo struct {
	Name            string `json:"name"`
	ReadOnlyHint    bool   `json:"read_only_hint,omitempty"`
	DestructiveHint bool   `json:"destructive_hint,omitempty"`
}
