package tool

import (
	"fmt"
	"slices"
	"strings"
)

// CodeMCPToolDisabled identifies a call refused because the user or project
// configuration explicitly disabled that MCP tool.
const CodeMCPToolDisabled = "mcp.tool_disabled_by_config"

// ReplaceDisabledMCP replaces one server's disabled-tool policy so a removed,
// renamed, or reconnected server cannot leave stale aliases that blame
// configuration for a tool that no longer exists.
func (r *Registry) ReplaceDisabledMCP(server string, bindings []MCPBinding) {
	if r == nil {
		return
	}
	server = strings.TrimSpace(server)
	if server == "" {
		return
	}

	names := map[string]bool{}
	for _, binding := range bindings {
		for _, name := range disabledMCPNames(binding) {
			names[name] = true
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.disabledMCPByServer == nil {
		r.disabledMCPByServer = map[string]map[string]bool{}
	}
	if len(names) == 0 {
		delete(r.disabledMCPByServer, server)
	} else {
		r.disabledMCPByServer[server] = names
	}
	r.rebuildDisabledMCPLocked()
}

// MarkDisabledMCP adds one binding to its server's disabled-tool policy. Prefer
// ReplaceDisabledMCP when applying a complete configuration snapshot.
func (r *Registry) MarkDisabledMCP(binding MCPBinding) {
	if r == nil {
		return
	}
	server := strings.TrimSpace(binding.Server)
	if server == "" {
		server = strings.TrimSpace(binding.CallableName)
	}
	if server == "" {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.disabledMCPByServer == nil {
		r.disabledMCPByServer = map[string]map[string]bool{}
	}
	if r.disabledMCP == nil {
		r.disabledMCP = map[string]bool{}
	}
	names := r.disabledMCPByServer[server]
	if names == nil {
		names = map[string]bool{}
		r.disabledMCPByServer[server] = names
	}
	for _, name := range disabledMCPNames(binding) {
		names[name] = true
		r.disabledMCP[name] = true
	}
}

// ClearDisabledMCP removes every disabled-tool alias recorded for one server.
func (r *Registry) ClearDisabledMCP(server string) {
	if r == nil {
		return
	}
	server = strings.TrimSpace(server)
	if server == "" {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.disabledMCPByServer[server]; !ok {
		return
	}
	delete(r.disabledMCPByServer, server)
	r.rebuildDisabledMCPLocked()
}

// CopyDisabledMCPFrom copies the disabled-tool and held-tool policy into a derived
// registry. Child agents may not copy the tools themselves, but they still need
// the same attribution when a stale call names a tool hidden by either.
func (r *Registry) CopyDisabledMCPFrom(parent *Registry) {
	if r == nil || parent == nil || r == parent {
		return
	}
	parent.mu.RLock()
	byServer := make(map[string]map[string]bool, len(parent.disabledMCPByServer))
	for server, names := range parent.disabledMCPByServer {
		byServer[server] = make(map[string]bool, len(names))
		for name := range names {
			byServer[server][name] = true
		}
	}
	parent.mu.RUnlock()

	r.mu.Lock()
	r.disabledMCPByServer = byServer
	r.rebuildDisabledMCPLocked()
	r.mu.Unlock()
	r.CopyHeldMCPFrom(parent)
}

// DisabledMCP reports whether name identifies a tool disabled by configuration.
func (r *Registry) DisabledMCP(name string) bool {
	_, ok := r.DisabledMCPRefusal(name)
	return ok
}

// DisabledMCPRefusal returns the typed refusal for a configuration-disabled MCP
// tool. The second result is false for unknown names.
func (r *Registry) DisabledMCPRefusal(name string) (Refusal, bool) {
	if r == nil {
		return Refusal{}, false
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return Refusal{}, false
	}
	r.mu.RLock()
	disabled := r.disabledMCP[name]
	r.mu.RUnlock()
	if !disabled {
		return Refusal{}, false
	}
	return Refusal{
		Code:    CodeMCPToolDisabled,
		Message: fmt.Sprintf("blocked: tool %q is disabled by configuration", name),
	}, true
}

func (r *Registry) rebuildDisabledMCPLocked() {
	r.disabledMCP = map[string]bool{}
	for _, names := range r.disabledMCPByServer {
		for name := range names {
			r.disabledMCP[name] = true
		}
	}
}

// disabledMCPNames returns the server-qualified aliases for one binding. Bare
// raw and visible names are deliberately excluded: they are ambiguous across
// servers and may also name an ordinary built-in or a hallucinated tool.
func disabledMCPNames(binding MCPBinding) []string {
	names := append([]string{binding.CallableName}, mcpBindingAliases(binding)...)
	slices.Sort(names)
	names = slices.Compact(names)
	out := names[:0]
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" || name == strings.TrimSpace(binding.RawName) || name == strings.TrimSpace(binding.VisibleName) {
			continue
		}
		out = append(out, name)
	}
	return out
}
