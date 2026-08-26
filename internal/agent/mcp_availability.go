package agent

import (
	"errors"
	"fmt"
	"strings"
)

func (r *MCPCapabilityRuntime) serverRegistered(server string) bool {
	if r == nil {
		return false
	}
	r.mu.RLock()
	_, ok := r.servers[strings.TrimSpace(server)]
	r.mu.RUnlock()
	return ok
}

func mcpServerUnregisteredMessage(server string) string {
	return fmt.Sprintf("MCP server %q is not registered in this session", server)
}

func mcpServerDisabledMessage(server string) string {
	return fmt.Sprintf("MCP server %q is disabled in this session", server)
}

func mcpServerUnregisteredError(server string) error {
	return errors.New(mcpServerUnregisteredMessage(server))
}

func mcpServerDisabledError(server string) error {
	return errors.New(mcpServerDisabledMessage(server))
}

func (t *UseCapabilityTool) serverRegistered(server string) bool {
	if t.runtime != nil {
		return t.runtime.serverRegistered(server)
	}
	// Standalone proxies have no authoritative runtime inventory to miss from.
	return true
}

// serverUnavailableReason distinguishes a disabled server from one this
// session never registered so the refusal points to the correct remedy.
func (t *UseCapabilityTool) serverUnavailableReason(server string) string {
	if !t.serverRegistered(server) {
		return mcpServerUnregisteredMessage(server)
	}
	return mcpServerDisabledMessage(server)
}
