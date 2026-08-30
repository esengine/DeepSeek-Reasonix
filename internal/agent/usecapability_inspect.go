package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"reasonix/internal/capability"
	"reasonix/internal/plugin"
	"reasonix/internal/tool"
)

const maxInspectBytes = 16 << 10

func (t *UseCapabilityTool) resolveDiscovery(ctx context.Context, p useCapabilityArgs, action, id string, base tool.ResolvedCall) (tool.ResolvedCall, error) {
	switch action {
	case "list":
		out, err := t.listCapabilities()
		if err != nil {
			if t.audit != nil {
				t.audit.RecordMCPProxy(true, false, true)
			}
			return tool.ResolvedCall{}, err
		}
		if t.audit != nil {
			t.audit.RecordMCPProxy(true, false, false)
			t.audit.RecordCapabilityDiscovery("list", 0, len(out), false)
		}
		base.SkipExecute = true
		base.Result = out
		base.ReadOnly = true
		return base, nil
	case "search":
		query := strings.TrimSpace(p.Query)
		if query == "" {
			return tool.ResolvedCall{}, fmt.Errorf("query is required for action=search")
		}
		out, resultCount, err := t.searchCapabilities(query, p.Limit)
		if err != nil {
			return tool.ResolvedCall{}, err
		}
		base.SkipExecute = true
		base.Result = out
		base.ReadOnly = true
		if t.audit != nil {
			t.audit.RecordCapabilityDiscovery("search", resultCount, len(out), false)
		}
		return base, nil
	default:
		if id == "" {
			return tool.ResolvedCall{}, fmt.Errorf("capability_id is required for action=inspect")
		}
		if id == sessionToolResultCapabilityID {
			out, err := t.inspectSessionToolResult()
			if err != nil {
				return tool.ResolvedCall{}, err
			}
			base.SkipExecute = true
			base.Result = out
			base.ReadOnly = true
			return base, nil
		}
		out, err := t.inspect(ctx, id)
		if err != nil {
			if t.audit != nil {
				t.audit.RecordMCPProxy(true, false, true)
			}
			return tool.ResolvedCall{}, err
		}
		if t.audit != nil {
			t.audit.RecordMCPProxy(true, false, false)
			t.audit.RecordCapabilityDiscovery("inspect", 1, len(out), false)
		}
		base.SkipExecute = true
		base.Result = out
		base.ReadOnly = true
		return base, nil
	}
}

func (t *UseCapabilityTool) inspect(_ context.Context, id string) (string, error) {
	cat := t.currentCatalog()
	e, ok := cat.Lookup(id)
	if !ok {
		return "", fmt.Errorf("unknown capability_id %q", id)
	}
	payload := map[string]any{
		"id":           e.ID,
		"kind":         e.Kind,
		"name":         e.Name,
		"description":  e.Description,
		"status":       e.Status,
		"read_only":    e.ReadOnly,
		"auto_use":     e.AutoUse,
		"requires":     e.Requires,
		"profiles":     e.Profiles,
		"tool_name":    e.ToolName,
		"auto_start":   e.AutoStart,
		"network_call": false,
	}
	if strings.HasPrefix(id, "skill:") {
		if contract, ok := t.capabilityArgumentContract(id); ok {
			payload["input_schema"] = contract.Schema
			payload["call_example"] = contract.Example
			payload["schema_fingerprint"] = tool.SchemaFingerprint(contract.Schema)
		}
	}
	if e.Kind == capability.KindMCPServer || e.Kind == capability.KindMCPTool {
		t.decorateMCPInspect(payload, e)
	}
	return marshalBoundedInspect(payload), nil
}

func (t *UseCapabilityTool) decorateMCPInspect(payload map[string]any, e capability.Entry) {
	server := e.Source
	if server == "" {
		server = e.ConnectName
	}
	if server == "" {
		return
	}
	if !t.serverEnabled(server) {
		payload["note"] = t.serverUnavailableReason(server)
		return
	}
	tools, source := t.localMCPTools(server)
	payload["source"] = source
	schemaBytes := 0
	for _, item := range tools {
		schemaBytes += len(item.Schema)
	}
	if t.capabilityAudit() != nil {
		t.capabilityAudit().RecordMCPList(source, "inspect", 0, len(tools), schemaBytes)
	}
	t.observeMCPList(mcpListObservation{
		Server: server, Source: source, Trigger: "inspect",
		ToolCount: len(tools), SchemaBytes: schemaBytes, NetworkCall: false,
	})
	if e.Kind == capability.KindMCPTool {
		_, raw, err := parseMCPCapabilityID(e.ID)
		if err != nil {
			return
		}
		selected, selectedSource, found := t.localMCPTool(server, raw)
		if !found {
			payload["note"] = "Exact schema is not present in the shared-host or disk cache. Call the server capability once to connect, then inspect this exact tool."
			return
		}
		payload["source"] = selectedSource
		payload["description"] = selected.Description
		payload["read_only"] = selected.ReadOnly
		payload["input_schema"] = selected.Schema
		payload["schema_fingerprint"] = tool.SchemaFingerprint(selected.Schema)
		payload["call_example"] = map[string]any{
			"action":        "call",
			"capability_id": e.ID,
			"arguments":     map[string]any{},
		}
		return
	}
	if len(tools) == 0 {
		payload["note"] = "Server not connected and no cached tool schema; call action=call on mcp-server:" + server + " to connect after authorization."
		return
	}
	payload["tools"] = compactInspectToolList(server, tools)
	payload["note"] = "Compact directory only. Inspect one mcp-tool capability_id to load its full input schema."
}

func (t *UseCapabilityTool) capabilityAudit() *capability.Audit {
	return t.audit
}

type inspectToolInfo struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	ReadOnly    bool            `json:"read_only"`
	Fingerprint string          `json:"schema_fingerprint,omitempty"`
	Schema      json.RawMessage `json:"input_schema,omitempty"`
}

func compactInspectToolList(server string, tools []plugin.CachedTool) []inspectToolInfo {
	list := make([]inspectToolInfo, 0, len(tools))
	for _, candidate := range tools {
		list = append(list, inspectToolInfo{
			ID:          "mcp-tool:" + server + "/" + candidate.Name,
			Name:        plugin.ModelToolName(server, candidate.Name),
			Description: truncateSearchDescription(candidate.Description),
			ReadOnly:    candidate.ReadOnly,
			Fingerprint: tool.SchemaFingerprint(candidate.Schema),
		})
	}
	return list
}

func marshalBoundedInspect(payload map[string]any) string {
	b, _ := json.MarshalIndent(payload, "", "  ")
	if len(b) <= maxInspectBytes {
		return string(b)
	}
	if tools, ok := payload["tools"].([]inspectToolInfo); ok {
		for len(tools) > 0 && len(b) > maxInspectBytes {
			tools = tools[:len(tools)-1]
			payload["tools"] = tools
			payload["truncated"] = true
			b, _ = json.MarshalIndent(payload, "", "  ")
		}
	}
	if len(b) > maxInspectBytes {
		delete(payload, "input_schema")
		payload["schema_omitted"] = "input schema exceeded the 16KB inspect response limit"
		b, _ = json.MarshalIndent(payload, "", "  ")
	}
	if len(b) > maxInspectBytes {
		// Third-party descriptions and metadata are untrusted and may exceed the
		// limit even after schemas/tool rows are removed. Preserve only bounded
		// scalar identity fields; never return oversized or invalid JSON.
		bounded := map[string]any{
			"truncated": true,
			"note":      "inspect response exceeded the 16KB limit; narrow the capability_id or page the underlying result",
		}
		for _, key := range []string{"id", "kind", "name", "status", "source", "schema_fingerprint"} {
			if value, ok := payload[key].(string); ok && value != "" {
				bounded[key] = truncateInspectString(value, 1024)
			}
		}
		for _, key := range []string{"read_only", "network_call", "auto_start"} {
			if value, ok := payload[key].(bool); ok {
				bounded[key] = value
			}
		}
		b, _ = json.MarshalIndent(bounded, "", "  ")
	}
	if len(b) > maxInspectBytes {
		return `{"truncated":true,"note":"inspect response exceeded the 16KB limit"}`
	}
	return string(b)
}

func truncateInspectString(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit-len("...")] + "..."
}

// inspectToolListJSON renders the compact directory returned after a server
// connection. Exact schemas stay behind inspect(mcp-tool:server/tool).
func inspectToolListJSON(server string, tools []tool.Tool) string {
	var list []inspectToolInfo
	for _, tl := range tools {
		raw := ""
		if m, ok := tl.(tool.MCPMetadata); ok {
			raw = m.MCPRawToolName()
		}
		list = append(list, inspectToolInfo{
			ID:          "mcp-tool:" + server + "/" + raw,
			Name:        tl.Name(),
			Description: tl.Description(),
			ReadOnly:    tl.ReadOnly(),
			Fingerprint: tool.SchemaFingerprint(tl.Schema()),
		})
	}
	extra, _ := json.MarshalIndent(list, "", "  ")
	return string(extra)
}
