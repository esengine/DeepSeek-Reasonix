package plugin

import (
	"slices"
	"strings"

	"reasonix/internal/contract/tool"
)

// ToolEnabled matches the server's raw tool name, before prefix stripping.
func (s Spec) ToolEnabled(rawName string) bool {
	return !slices.Contains(s.DisabledTools, rawName)
}

// EnabledCachedTools applies current policy even to a stale schema snapshot.
func (s Spec) EnabledCachedTools(tools []CachedTool) []CachedTool {
	if len(s.DisabledTools) == 0 {
		return tools
	}
	return slices.DeleteFunc(slices.Clone(tools), func(t CachedTool) bool { return !s.ToolEnabled(t.Name) })
}

func disabledToolNames(names []string) []string {
	if len(names) == 0 {
		return nil
	}
	names = slices.Clone(names)
	slices.Sort(names)
	return slices.Compact(names)
}

// DisabledMCPBindings describes the tools hidden by a server's disabled_tools
// policy. Registration records these bindings without registering the tools so a
// stale call can receive a typed refusal.
func DisabledMCPBindings(s Spec) []tool.MCPBinding {
	names := disabledToolNames(s.DisabledTools)
	out := make([]tool.MCPBinding, 0, len(names))
	for _, raw := range names {
		visible := raw
		if s.StripRawPrefix != "" {
			visible = strings.TrimPrefix(visible, s.StripRawPrefix)
		}
		out = append(out, tool.MCPBinding{
			Package:      s.Package,
			Server:       s.Name,
			RawName:      raw,
			VisibleName:  visible,
			CallableName: ModelToolName(s.Name, visible),
			CapabilityID: "mcp-tool:" + s.Name + "/" + raw,
		})
	}
	return out
}

// ApplyDisabledMCPPolicy replaces the registry's policy for one server. A
// complete replacement, rather than additive marks, prevents stale entries from
// surviving a reconnect whose disabled_tools list has shrunk or changed.
func ApplyDisabledMCPPolicy(reg *tool.Registry, s Spec) {
	if reg == nil {
		return
	}
	reg.ReplaceDisabledMCP(s.Name, DisabledMCPBindings(s))
}
