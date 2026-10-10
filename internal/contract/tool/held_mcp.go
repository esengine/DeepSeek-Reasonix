package tool

import (
	"fmt"
	"maps"
	"strings"
)

// CodeMCPToolHeld identifies a call refused because the tool's definition is
// new or differs from the one the user approved for its server.
const CodeMCPToolHeld = "mcp.tool_definition_held"

// HeldMCPClass says how a held tool differs from the approved definitions.
type HeldMCPClass string

const (
	HeldMCPAdded   HeldMCPClass = "added"
	HeldMCPChanged HeldMCPClass = "changed"
	// HeldMCPUnverifiable means the approval record could not be read, so
	// nothing about the tool can be compared.
	HeldMCPUnverifiable HeldMCPClass = "unverifiable"
)

// HeldMCP is one tool withheld from the model, with the binding that names it.
type HeldMCP struct {
	Binding MCPBinding
	Class   HeldMCPClass
}

// ReplaceHeldMCP replaces one server's held set, so a reconnect that no longer
// holds a tool stops refusing it.
func (r *Registry) ReplaceHeldMCP(server string, held []HeldMCP) {
	if r == nil {
		return
	}
	server = strings.TrimSpace(server)
	if server == "" {
		return
	}
	names := map[string]HeldMCP{}
	for _, h := range held {
		for _, name := range disabledMCPNames(h.Binding) {
			names[name] = h
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(names) == 0 {
		delete(r.heldMCP, server)
		return
	}
	if r.heldMCP == nil {
		r.heldMCP = map[string]map[string]HeldMCP{}
	}
	r.heldMCP[server] = names
}

// CopyHeldMCPFrom copies the held set into a derived registry so a child
// agent's stale call gets the same attribution.
func (r *Registry) CopyHeldMCPFrom(parent *Registry) {
	if r == nil || parent == nil || r == parent {
		return
	}
	parent.mu.RLock()
	byServer := make(map[string]map[string]HeldMCP, len(parent.heldMCP))
	for server, names := range parent.heldMCP {
		byServer[server] = maps.Clone(names)
	}
	parent.mu.RUnlock()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.heldMCP = byServer
}

// HeldMCPRefusal returns the typed refusal for a tool held pending
// re-approval. The second result is false for any other name.
func (r *Registry) HeldMCPRefusal(name string) (Refusal, bool) {
	if r == nil {
		return Refusal{}, false
	}
	name = strings.TrimSpace(name)
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, names := range r.heldMCP {
		if h, ok := names[name]; ok {
			return HeldMCPRefusalFor(h), true
		}
	}
	return Refusal{}, false
}

// HeldMCPRefusalFor renders the refusal for one held tool.
func HeldMCPRefusalFor(h HeldMCP) Refusal {
	return Refusal{
		Code: CodeMCPToolHeld,
		Message: fmt.Sprintf("blocked: tool %q of MCP server %q is held (%s): its definition was not approved by the user",
			h.Binding.CallableName, h.Binding.Server, h.Class),
	}
}

// MCPRefusal answers for an MCP tool name the registry does not serve: the
// configuration policy first, then a definition held for re-approval.
func (r *Registry) MCPRefusal(name string) (Refusal, bool) {
	if refusal, ok := r.DisabledMCPRefusal(name); ok {
		return refusal, true
	}
	return r.HeldMCPRefusal(name)
}
