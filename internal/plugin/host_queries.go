package plugin

import (
	"sort"
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
	out := make([]string, 0, len(names))
	for name := range names {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
