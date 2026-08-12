package agent

import (
	"strings"

	"reasonix/internal/plugin"
	"reasonix/internal/tool"
)

func (r *MCPCapabilityRuntime) applyToolListChange(spec plugin.Spec, tools []tool.Tool) {
	if r == nil {
		return
	}
	name := strings.TrimSpace(spec.Name)
	r.dispatchMu.Lock()
	defer r.dispatchMu.Unlock()
	r.mu.RLock()
	configured, ok := r.servers[name]
	r.mu.RUnlock()
	if !ok || !configured.enabled || !plugin.MCPRuntimeSpecMatches(configured.spec, spec) {
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
