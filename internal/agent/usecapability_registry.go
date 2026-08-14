package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"reasonix/internal/plugin"
	"reasonix/internal/tool"
)

// resolveRegistryTool binds a registry tool by name for use_capability call.
// MCP adapters additionally cross the current runtime authorization boundary.
func (t *UseCapabilityTool) resolveRegistryTool(ctx context.Context, name, id string, args json.RawMessage, base tool.ResolvedCall) (tool.ResolvedCall, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return tool.ResolvedCall{}, fmt.Errorf("capability id %q is missing a tool name", id)
	}
	if name == "use_capability" {
		return tool.ResolvedCall{}, fmt.Errorf("cannot proxy use_capability through itself")
	}
	if t.registry == nil {
		return t.resolveUnavailable(base, id, name, "tool registry is unavailable"), nil
	}
	target, ok := t.registry.Get(name)
	if !ok {
		return t.resolveUnavailable(base, id, name, fmt.Sprintf("tool %q is not registered in this session", name)), nil
	}
	if metadata, isMCP := target.(tool.MCPMetadata); isMCP && t.runtime != nil {
		server := strings.TrimSpace(metadata.MCPServerName())
		raw := strings.TrimSpace(metadata.MCPRawToolName())
		if server == "" || raw == "" {
			return t.resolveUnavailable(base, id, name, fmt.Sprintf("MCP tool %q has incomplete runtime identity", name)), nil
		}
		spec, unlock, err := t.lockAuthorizedRuntimeServer(ctx, server)
		if err != nil {
			return t.resolveUnavailable(base, id, name, err.Error()), nil
		}
		matches := plugin.MCPToolMatchesSpec(target, spec)
		unlock()
		if !matches {
			return t.resolveUnavailable(base, id, name, fmt.Sprintf("connected MCP server %q identity does not match the current runtime configuration", server)), nil
		}
		target = t.bindRuntimeMCP(spec, target)
	}
	base.Target = target
	base.TargetName = name
	base.ReadOnly = target.ReadOnly()
	if len(args) == 0 {
		base.Args = json.RawMessage(`{}`)
	} else {
		base.Args = args
	}
	return base, nil
}
