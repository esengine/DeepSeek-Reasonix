package agent

import (
	"context"
	"encoding/json"
	"strings"

	"reasonix/internal/capability"
	"reasonix/internal/plugin"
)

// listServerInfo is one configured MCP server entry returned by action=list.
// It never starts a server or opens a network connection.
type listServerInfo struct {
	Name         string `json:"name"`
	CapabilityID string `json:"capability_id"`
	Status       string `json:"status"`
	Authorized   bool   `json:"authorized"`
	Connected    bool   `json:"connected"`
}

// listCapabilities returns non-MCP catalog entries plus compact MCP server
// summaries. Concrete MCP directories stay behind action=inspect so one global
// list cannot grow with every cached tool description. The top-level "servers"
// key stays compatible with restricted subagent list filtering.
func (t *UseCapabilityTool) listCapabilities() (string, error) {
	type capInfo struct {
		ID          string `json:"id"`
		Kind        string `json:"kind"`
		Name        string `json:"name"`
		Status      string `json:"status,omitempty"`
		ReadOnly    bool   `json:"read_only,omitempty"`
		Description string `json:"description,omitempty"`
	}
	var caps []capInfo
	if t.currentToolResultTarget() != nil {
		caps = append(caps, capInfo{
			ID: sessionToolResultCapabilityID, Kind: "session", Name: "tool_result", Status: "ready", ReadOnly: true,
			Description: "Read one bounded page from a complete tool result retained in this agent's current session.",
		})
	}
	if t.catalog != nil {
		for _, e := range t.catalog().Entries {
			// Servers already have a compact representation below. Keep concrete
			// MCP tools in the internal catalog for routing, inspect, and known-ID
			// calls, but do not inject every cached directory into model context.
			if e.Kind == capability.KindMCPServer || e.Kind == capability.KindMCPTool {
				continue
			}
			// Skip provider-visible core tools — they are already top-level.
			if e.Kind == capability.KindTool && t.registry != nil && t.registry.ProviderVisible(e.ToolName) {
				continue
			}
			caps = append(caps, capInfo{
				ID:          e.ID,
				Kind:        string(e.Kind),
				Name:        e.Name,
				Status:      string(e.Status),
				ReadOnly:    e.ReadOnly,
				Description: e.Description,
			})
		}
	}
	serversJSON, err := t.listServers()
	if err != nil {
		return "", err
	}
	var serversPayload struct {
		Servers []listServerInfo `json:"servers"`
		Note    string           `json:"note"`
	}
	_ = json.Unmarshal([]byte(serversJSON), &serversPayload)
	payload := map[string]any{
		"capabilities": caps,
		"servers":      serversPayload.Servers,
		"note":         "MCP servers are summarized below. Call action=inspect with capability_id=mcp-server:<name> to list one enabled server's tools without starting it, or action=call with a concrete capability_id to invoke a non-core tool, skill, MCP tool, or other catalog entry without changing the provider tool schema.",
	}
	if serversPayload.Note != "" {
		payload["note"] = payload["note"].(string) + " " + serversPayload.Note
	}
	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// listServers returns sorted configured MCP server names, status, and
// capability IDs without starting servers. Used by Planner discovery when no
// specific capability route was provided.
func (t *UseCapabilityTool) listServers() (string, error) {
	configured := t.configuredServers()
	list := make([]listServerInfo, 0, len(configured))
	for _, server := range configured {
		spec := server.spec
		name := strings.TrimSpace(spec.Name)
		if name == "" {
			continue
		}
		// Apply stored project grants without process/network side effects so
		// list status matches resolve/execute authorization.
		resolved := plugin.ResolveStoredAuthorization(context.Background(), spec)
		connected := server.enabled && resolved.ServerAuthorized() && t.host != nil && t.host.HasClientForSpec(resolved)
		status := "configured"
		if !server.enabled {
			status = "disabled"
		} else if connected {
			status = "ready"
		} else if t.host != nil {
			for _, f := range t.host.Failures() {
				if f.Name == name && strings.TrimSpace(f.Error) != "" {
					status = "failed"
					break
				}
			}
		}
		list = append(list, listServerInfo{
			Name:         name,
			CapabilityID: "mcp-server:" + name,
			Status:       status,
			Authorized:   resolved.ServerAuthorized(),
			Connected:    connected,
		})
	}
	b, err := json.MarshalIndent(map[string]any{
		"servers": list,
		"note":    "list does not start MCP servers. Call action=call on mcp-server:<name> to connect after authorization, or mcp-tool:<server>/<tool> for a concrete tool.",
	}, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}
