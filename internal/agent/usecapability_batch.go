package agent

import (
	"encoding/json"
	"strings"

	"reasonix/internal/tool"
)

// ClassifyCall resolves only local metadata. Execution still rechecks the live
// MCP generation and safety annotations under the runtime dispatch lock.
func (t *UseCapabilityTool) ClassifyCall(raw json.RawMessage) tool.CallClass {
	args, action, id, err := parseUseCapabilityArgs(raw)
	if err != nil {
		return tool.CallClass{}
	}
	switch action {
	case "list", "search", "inspect":
		return tool.CallClass{
			Known: true, ReadOnly: true, ParallelSafe: true,
			ResourceKey: "capability-catalog", Generation: "use-capability-v2",
		}
	case "call":
		return t.classifyCapabilityTarget(id, args.Arguments)
	default:
		return tool.CallClass{}
	}
}

func (t *UseCapabilityTool) classifyCapabilityTarget(id string, args json.RawMessage) tool.CallClass {
	if server, raw, err := parseMCPCapabilityID(id); err == nil {
		cached, source, ok := t.localMCPTool(server, raw)
		if !ok || source != "shared_host" || !cached.ReadOnly || cached.Destructive || t.host == nil || !t.host.HasClient(server) {
			return tool.CallClass{}
		}
		if t.runtime != nil && t.runtime.serverIsSerial(server) {
			return tool.CallClass{Known: true, ReadOnly: true, ResourceKey: server + "/" + raw, Generation: tool.SchemaFingerprint(cached.Schema)}
		}
		return tool.CallClass{
			Known: true, ReadOnly: true, ParallelSafe: true,
			ResourceKey: server + "/" + raw, Generation: tool.SchemaFingerprint(cached.Schema),
		}
	}
	if t.registry == nil || strings.HasPrefix(id, "skill:") || strings.HasPrefix(id, "task:") {
		return tool.CallClass{}
	}
	name := ""
	switch {
	case strings.HasPrefix(id, "tool:"):
		name = strings.TrimSpace(strings.TrimPrefix(id, "tool:"))
	case strings.HasPrefix(id, "workflow:"):
		name = strings.TrimSpace(strings.TrimPrefix(id, "workflow:"))
	case strings.HasPrefix(id, "web:"), strings.HasPrefix(id, "lsp:"), strings.HasPrefix(id, "session:"), strings.HasPrefix(id, "memory:"):
		if _, rest, ok := strings.Cut(id, ":"); ok {
			name = strings.TrimSpace(rest)
		}
	}
	if name == "" {
		return tool.CallClass{}
	}
	target, ok := t.registry.Get(name)
	if !ok || !target.ReadOnly() {
		return tool.CallClass{}
	}
	// Evidence/order control tools remain serial even if their schema is
	// nominally read-only.
	switch target.Name() {
	case "complete_step", "todo_write", "wait", "bash_output", "compress":
		return tool.CallClass{Known: true, ReadOnly: true}
	}
	_ = args // Reserved for target-specific classifiers.
	return tool.CallClass{
		Known: true, ReadOnly: true, ParallelSafe: true,
		ResourceKey: target.Name(), Generation: tool.SchemaFingerprint(target.Schema()),
	}
}
