package plugin

import (
	"maps"
	"slices"
	"strings"
)

// Prompts returns every MCP prompt discovered across connected servers.
func (h *Host) Prompts() []Prompt {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return append([]Prompt(nil), h.prompts...)
}

// Resources returns every MCP resource discovered across connected servers.
func (h *Host) Resources() []Resource {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return append([]Resource(nil), h.resources...)
}

// ServerNames returns the connected servers' names, in connection order.
func (h *Host) ServerNames() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	names := make([]string, len(h.clients))
	for i, c := range h.clients {
		names[i] = c.name
	}
	return names
}

// Failures returns configured MCP servers that failed to connect.
func (h *Host) Failures() []Failure {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]Failure, len(h.failures))
	copy(out, h.failures)
	return out
}

// ConnectingServers returns server names whose startup handshake is currently in
// flight. It is intentionally status-only: connected clients and failures remain
// the source of truth for ready/issue states.
func (h *Host) ConnectingServers() []string {
	h.spawningMu.Lock()
	defer h.spawningMu.Unlock()
	names := make(map[string]struct{}, len(h.spawning))
	for key, attempt := range h.spawning {
		name := key
		if attempt != nil && strings.TrimSpace(attempt.server) != "" {
			name = attempt.server
		}
		names[name] = struct{}{}
	}
	out := slices.Sorted(maps.Keys(names))
	return out
}

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
	ConfigSource string
	// Description is the server's own account of itself, from the optional
	// instructions field of the initialize result. Only the server can answer
	// what it is for, so a surface either shows this or shows nothing.
	Description string
	Tools       int
	Prompts     int
	Resources   int
	HasTools    bool
	ToolList    []ToolInfo
}

// Servers returns a status summary per connected server, in connection order.
func (h *Host) Servers() []ServerStatus {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]ServerStatus, 0, len(h.clients))
	for _, c := range h.clients {
		s := ServerStatus{
			Name:         c.name,
			Transport:    c.transport,
			ConfigSource: strings.TrimSpace(c.spec.ConfigSource),
			Description:  c.instructions,
			Tools:        c.toolCount,
			HasTools:     c.hasTools,
		}
		c.toolsMu.Lock()
		s.ToolList = append([]ToolInfo(nil), c.tools...)
		c.toolsMu.Unlock()
		for _, p := range h.prompts {
			if p.Server == c.name {
				s.Prompts++
			}
		}
		for _, r := range h.resources {
			if r.Server == c.name {
				s.Resources++
			}
		}
		out = append(out, s)
	}
	return out
}

// serverInstructions returns what a connected server said about itself, so a
// cache write started away from the connect path persists the same text the
// live status shows.
func (h *Host) serverInstructions(name string) string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, c := range h.clients {
		if c.name == name {
			return c.instructions
		}
	}
	return ""
}
