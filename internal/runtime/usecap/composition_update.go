package usecap

import (
	"reasonix/internal/contract/config"
	"reasonix/internal/ext/plugin"
	"strings"
)

// ServerIdentity returns the retained declaration identity without refreshing
// configuration or exposing the server's launch parameters.
func (r *MCPCapabilityRuntime) ServerIdentity(name string) (config.PluginEntry, bool) {
	if r == nil {
		return config.PluginEntry{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	server, found := r.servers[strings.TrimSpace(name)]
	if !found {
		return config.PluginEntry{}, false
	}
	return config.PluginEntry{Name: server.entry.Name, Source: server.entry.Source}, true
}

// UpsertServer makes a hot-added or updated MCP spec authoritative for every
// frontend on this controller. Dynamic state stays host-local and never changes
// the provider-visible use_capability schema.
func (r *MCPCapabilityRuntime) UpsertServer(entry config.PluginEntry, raw plugin.Spec, enabled bool) {
	if r == nil {
		return
	}
	r.dispatchMu.Lock()
	defer r.dispatchMu.Unlock()
	if r.compositionFrozen {
		return
	}
	spec := cloneMCPSpec(raw)
	if !r.compositionAllows(spec) {
		r.mu.Lock()
		delete(r.servers, spec.Name)
		r.mu.Unlock()
		r.state.clearServer(spec.Name)
		return
	}
	name := strings.TrimSpace(spec.Name)
	if name == "" {
		return
	}
	entry = runtimePluginEntry(entry)
	if strings.TrimSpace(entry.Name) == "" {
		entry.Name = name
	}
	cached, keyOK := cachedToolsForSpec(spec)
	r.mu.Lock()
	r.servers[name] = mcpRuntimeServer{
		entry:      entry,
		spec:       spec,
		enabled:    enabled,
		cached:     cached,
		cacheKeyOK: keyOK,
	}
	r.mu.Unlock()
	// Endpoint/tool metadata may have changed. Never route a stale live snapshot
	// across an update; a connected client or the next call will repopulate it.
	r.state.clearServer(name)
}
