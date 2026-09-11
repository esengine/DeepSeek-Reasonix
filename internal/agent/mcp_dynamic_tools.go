package agent

import (
	"encoding/json"
	"strings"

	"reasonix/internal/plugin"
	"reasonix/internal/tool"
)

func (r *MCPCapabilityRuntime) syncRegistryInventory(previous, next map[string]mcpRuntimeServer) {
	for name := range previous {
		if _, ok := next[name]; ok {
			continue
		}
		r.setRegistryServerEnabled(name, false)
		r.state.clearServer(name)
	}
	for name, server := range next {
		r.setRegistryServerEnabled(name, server.enabled)
		if !server.enabled {
			r.state.clearServer(name)
		}
	}
}

func (r *MCPCapabilityRuntime) setRegistryServerEnabled(name string, enabled bool) {
	if r == nil || r.registry == nil {
		return
	}
	prefix := plugin.ToolPrefix(name)
	if enabled {
		r.registry.ResumePrefix(prefix)
		return
	}
	r.registry.SuspendPrefix(prefix)
}

func (r *MCPCapabilityRuntime) applyToolListChange(spec plugin.Spec, tools []tool.Tool) {
	if r == nil {
		return
	}
	name := strings.TrimSpace(spec.Name)
	r.dispatchMu.Lock()
	r.mu.RLock()
	configured, ok := r.servers[name]
	r.mu.RUnlock()
	if !ok || !configured.enabled || !plugin.MCPRuntimeSpecMatches(configured.spec, spec) {
		r.dispatchMu.Unlock()
		return
	}
	if r.registry != nil {
		r.registry.RemovePrefix(plugin.ToolPrefix(name))
		for _, candidate := range tools {
			if candidate != nil && plugin.MCPToolMatchesSpec(candidate, configured.spec) {
				r.registry.Add(candidate)
			}
		}
	}
	r.state.markConnected(name)
	r.state.setLiveTools(name, snapshotMCPTools(tools))
	r.dispatchMu.Unlock()
	r.notifyToolListChanged(name, tools)
}

func snapshotMCPTools(tools []tool.Tool) []plugin.CachedTool {
	snapshot := make([]plugin.CachedTool, 0, len(tools))
	for _, candidate := range tools {
		metadata, ok := candidate.(tool.MCPMetadata)
		if !ok || metadata.MCPRawToolName() == "" {
			continue
		}
		snapshot = append(snapshot, plugin.CachedTool{
			Name:        metadata.MCPRawToolName(),
			Description: candidate.Description(),
			Schema:      candidate.Schema(),
			ReadOnly:    candidate.ReadOnly(),
			Destructive: mcpDestructiveHint(candidate),
		})
	}
	return snapshot
}

func (t *UseCapabilityTool) resolveUnconnectedMCPCall(id string, args json.RawMessage, base tool.ResolvedCall, spec plugin.Spec, server, raw, modelName string) tool.ResolvedCall {
	destructive := false
	if t.catalog != nil {
		if entry, found := t.catalog().Lookup(id); found {
			destructive = entry.Destructive
		}
	}
	readOnly := false
	var schema json.RawMessage
	if cached, _, found := t.localMCPTool(server, raw); found {
		destructive = destructive || cached.Destructive
		readOnly = cached.ReadOnly
		schema = cached.Schema
	} else if cached, found := plugin.CachedToolSafetyForSpec(spec, raw); found {
		destructive = destructive || cached.Destructive
		readOnly = cached.ReadOnly
	}
	lazy := &onDemandMCPTool{proxy: t, spec: spec, server: server, raw: raw, modelName: modelName, destructive: destructive, readOnly: readOnly, schema: schema}
	base.Target = lazy
	base.TargetName = modelName
	base.ReadOnly = lazy.ReadOnly()
	if len(args) == 0 {
		base.Args = json.RawMessage(`{}`)
	} else {
		base.Args = args
	}
	return base
}
