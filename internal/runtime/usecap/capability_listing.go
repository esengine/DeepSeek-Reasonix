package usecap

import (
	"context"
	"encoding/json"
	"maps"
	"slices"
	"strings"

	"reasonix/internal/ext/plugin"
	"reasonix/internal/runtime/capability"
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

// listCapabilities returns the unified catalog summary: MCP servers plus
// non-provider-visible tools and skills available through this proxy. The
// top-level "servers" key stays compatible with restricted subagent list
// filtering.
func (t *UseCapabilityTool) argumentNames(e capability.Entry) (requires, accepts []string) {
	if e.Kind != capability.KindTool || t.registry == nil {
		return nil, nil
	}
	tl, ok := t.registry.Get(e.ToolName)
	if !ok {
		return nil, nil
	}
	var level schemaLevel
	if json.Unmarshal(tl.Schema(), &level) != nil || len(level.Properties) == 0 {
		return nil, nil
	}
	return level.Required, slices.Sorted(maps.Keys(level.Properties))
}

func (t *UseCapabilityTool) listCapabilities() (string, error) {
	type capInfo struct {
		ID          string   `json:"id"`
		Aliases     []string `json:"aliases,omitempty"`
		Kind        string   `json:"kind"`
		Name        string   `json:"name"`
		Status      string   `json:"status,omitempty"`
		ReadOnly    bool     `json:"read_only,omitempty"`
		Description string   `json:"description,omitempty"`
		Requires    []string `json:"requires,omitempty"`
		Accepts     []string `json:"accepts,omitempty"`
	}
	var caps []capInfo
	if t.catalog != nil {
		for _, e := range t.catalog().Entries {
			// Skip provider-visible core tools — they are already top-level.
			if e.Kind == capability.KindTool && t.registry != nil && t.registry.ProviderVisible(e.ToolName) {
				continue
			}
			info := capInfo{
				ID:          e.ID,
				Aliases:     e.Aliases,
				Kind:        string(e.Kind),
				Name:        e.Name,
				Status:      string(e.Status),
				ReadOnly:    e.ReadOnly,
				Description: capabilityLead(e.Description),
			}
			info.Requires, info.Accepts = t.argumentNames(e)
			caps = append(caps, info)
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
	payload := struct {
		Note         string           `json:"note"`
		Capabilities []capInfo        `json:"capabilities"`
		Servers      []listServerInfo `json:"servers"`
	}{
		Note:         "This is the full catalog and may be truncated in conversation. Prefer action=search with capability terms, then action=call with a returned id.",
		Capabilities: caps,
		Servers:      serversPayload.Servers,
	}
	if serversPayload.Note != "" {
		payload.Note += " " + serversPayload.Note
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

// emptyCapabilityListResult is the fail-closed list payload: no server metadata.
func emptyCapabilityListResult(note string) string {
	if strings.TrimSpace(note) == "" {
		note = "list is filtered to this subagent's allowed MCP servers."
	}
	b, err := json.MarshalIndent(map[string]any{
		"servers": []listServerInfo{},
		"note":    note,
	}, "", "  ")
	if err != nil {
		return `{"servers":[],"note":"list is filtered to this subagent's allowed MCP servers."}`
	}
	return string(b)
}

// FilterCapabilityListResult keeps only servers in the allowlist for restricted
// proxies. Empty allowlist or unreadable payloads fail closed (empty server
// list) so discovery never leaks the full configured MCP inventory.
func FilterCapabilityListResult(raw string, servers map[string]bool) string {
	const baseNote = "list is filtered to this subagent's allowed MCP servers."
	if len(servers) == 0 {
		return emptyCapabilityListResult(baseNote + " No allowed MCP servers were resolved from the profile allowlist.")
	}
	var payload struct {
		Servers []listServerInfo `json:"servers"`
		Note    string           `json:"note"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return emptyCapabilityListResult(baseNote + " List payload was unreadable; returning no servers (fail-closed).")
	}
	filtered := make([]listServerInfo, 0, len(payload.Servers))
	for _, s := range payload.Servers {
		if servers[strings.TrimSpace(s.Name)] {
			filtered = append(filtered, s)
		}
	}
	payload.Servers = filtered
	if payload.Note == "" {
		payload.Note = baseNote
	} else if !strings.Contains(payload.Note, "Filtered to this subagent") {
		payload.Note = payload.Note + " Filtered to this subagent's allowed MCP servers."
	}
	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return emptyCapabilityListResult(baseNote + " Failed to encode filtered list (fail-closed).")
	}
	return string(b)
}
