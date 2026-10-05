// Package mcpsetup turns whatever shape a user has an MCP server in — the
// mcpServers JSON its docs print, a command line, a bare URL, or CLI argv — into
// config.PluginEntry values, and reports what is risky about the result.
//
// It exists because the same three input shapes reach three frontends: the CLI
// parses argv, the desktop and the web UI take a pasted block. Naming rules and
// secret detection have to agree across all of them, so they live here rather
// than in whichever frontend needed them first.
//
// Nothing here writes config or connects anything: a Draft is a proposal the
// caller shows the user before the controller installs it.
package mcpsetup
