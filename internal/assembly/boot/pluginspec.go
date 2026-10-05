package boot

import (
	"strings"

	"reasonix/internal/contract/tool"
	"reasonix/internal/ext/plugin"
	"reasonix/internal/ext/skill"
)

func skillMCPBindings(sk skill.Skill, reg *tool.Registry, specs []plugin.Spec, cachedTools map[string][]plugin.CachedTool, cacheKeyOK map[string]bool) []tool.MCPBinding {
	var out []tool.MCPBinding
	liveServers := map[string]bool{}
	if reg != nil {
		bindings := reg.MCPBindings()
		out = make([]tool.MCPBinding, 0, len(bindings))
		for _, binding := range bindings {
			liveServers[binding.Server] = true
			if binding.Package == sk.Plugin {
				out = append(out, binding)
			}
		}
	}
	// A valid cached schema also supplies stable bindings for an on-demand
	// package server before it is connected. The skill can then route through
	// use_capability without inventing Reasonix's canonical name.
	for _, spec := range specs {
		if spec.Package != sk.Plugin || liveServers[spec.Name] || !cacheKeyOK[spec.Name] {
			continue
		}
		for _, cached := range cachedTools[spec.Name] {
			visible := cached.Name
			if spec.StripRawPrefix != "" {
				visible = strings.TrimPrefix(visible, spec.StripRawPrefix)
			}
			out = append(out, tool.MCPBinding{
				Package:      spec.Package,
				Server:       spec.Name,
				RawName:      cached.Name,
				VisibleName:  visible,
				CallableName: plugin.ModelToolName(spec.Name, visible),
				CapabilityID: "mcp-tool:" + spec.Name + "/" + cached.Name,
			})
		}
	}
	return out
}
