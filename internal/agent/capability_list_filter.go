package agent

import (
	"encoding/json"
	"maps"
	"strings"

	"reasonix/internal/tool"
)

func (t *restrictedCapabilityProxy) bindToolResultSession(session func() *Session) {
	if binder, ok := t.Tool.(toolResultSessionBinder); ok {
		binder.bindToolResultSession(session)
	}
}

func (t *restrictedCapabilityProxy) bindMCPListObserver(observer func(mcpListObservation)) {
	if binder, ok := t.Tool.(mcpListObserverBinder); ok {
		binder.bindMCPListObserver(observer)
	}
}

func (t *restrictedCapabilityProxy) activateMCPListObserver() func() {
	if activator, ok := t.Tool.(mcpListObserverActivator); ok {
		return activator.activateMCPListObserver()
	}
	return func() {}
}

// cloneCapabilityFrontend creates an Agent-owned frontend whenever binding a
// session reader could otherwise mutate a parent Agent's tool. Unknown tools
// remain shareable only when they cannot participate in session binding.
func cloneCapabilityFrontend(frontend tool.Tool) tool.Tool {
	switch typed := frontend.(type) {
	case nil:
		return nil
	case *UseCapabilityTool:
		if typed == nil {
			return nil
		}
		return typed.CloneForAgent(nil, nil)
	case *restrictedCapabilityProxy:
		if typed == nil {
			return nil
		}
		return typed.cloneForAgent()
	case pathBoundCapabilityProxy:
		inner := cloneCapabilityFrontend(typed.inner)
		resolver, ok := inner.(tool.CallResolver)
		if inner == nil || !ok {
			return nil
		}
		return pathBoundCapabilityProxy{inner: inner, resolver: resolver}
	default:
		if _, sessionBindable := frontend.(toolResultSessionBinder); sessionBindable {
			return nil
		}
		return frontend
	}
}

func (t *restrictedCapabilityProxy) cloneForAgent() *restrictedCapabilityProxy {
	if t == nil {
		return nil
	}
	inner := cloneCapabilityFrontend(t.Tool)
	resolver, ok := inner.(tool.CallResolver)
	if inner == nil || !ok {
		return nil
	}
	return &restrictedCapabilityProxy{
		Tool:     inner,
		resolver: resolver,
		allowed:  cloneBoolMap(t.allowed),
		servers:  cloneBoolMap(t.servers),
	}
}

func cloneBoolMap(src map[string]bool) map[string]bool {
	if src == nil {
		return nil
	}
	dst := make(map[string]bool, len(src))
	maps.Copy(dst, src)
	return dst
}

// emptyCapabilityListResult is the fail-closed list payload: no server metadata.
func emptyCapabilityListResult(note string) string {
	if strings.TrimSpace(note) == "" {
		note = "list is filtered to this subagent's allowed MCP servers."
	}
	b, err := json.MarshalIndent(map[string]any{
		"capabilities": []any{},
		"servers":      []listServerInfo{},
		"note":         note,
	}, "", "  ")
	if err != nil {
		return `{"servers":[],"note":"list is filtered to this subagent's allowed MCP servers."}`
	}
	return string(b)
}

// filterCapabilityListResult keeps only servers in the allowlist for restricted
// proxies. Empty allowlist or unreadable payloads fail closed (empty server
// list) so discovery never leaks the full configured MCP inventory.
func filterCapabilityListResult(raw string, servers map[string]bool) string {
	const baseNote = "list is filtered to this subagent's allowed MCP servers."
	if len(servers) == 0 {
		return emptyCapabilityListResult(baseNote + " No allowed MCP servers were resolved from the profile allowlist.")
	}
	var payload struct {
		Capabilities []json.RawMessage `json:"capabilities"`
		Servers      []listServerInfo  `json:"servers"`
		Note         string            `json:"note"`
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
	filteredCapabilities := make([]json.RawMessage, 0, 1)
	for _, raw := range payload.Capabilities {
		var entry struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(raw, &entry) == nil && strings.TrimSpace(entry.ID) == sessionToolResultCapabilityID {
			filteredCapabilities = append(filteredCapabilities, raw)
		}
	}
	payload.Capabilities = filteredCapabilities
	if payload.Note == "" {
		payload.Note = baseNote
	} else if !strings.Contains(payload.Note, "Filtered to this subagent") {
		payload.Note += " Filtered to this subagent's allowed MCP servers."
	}
	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return emptyCapabilityListResult(baseNote + " Failed to encode filtered list (fail-closed).")
	}
	return string(b)
}

func filterCapabilitySearchResult(raw string, allowed map[string]bool) string {
	var payload struct {
		Query   string            `json:"query"`
		Results []json.RawMessage `json:"results"`
		Note    string            `json:"note"`
	}
	if json.Unmarshal([]byte(raw), &payload) != nil {
		return `{"query":"","results":[],"note":"search result was unreadable; filtered fail-closed"}`
	}
	filtered := make([]json.RawMessage, 0, len(payload.Results))
	for _, rawResult := range payload.Results {
		var result struct {
			CapabilityID string `json:"capability_id"`
		}
		if json.Unmarshal(rawResult, &result) == nil && allowed[strings.TrimSpace(result.CapabilityID)] {
			filtered = append(filtered, rawResult)
		}
	}
	payload.Results = filtered
	if payload.Note != "" {
		payload.Note += " Filtered to this subagent's allowed capabilities."
	}
	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return `{"query":"","results":[],"note":"search result filtering failed closed"}`
	}
	return string(b)
}
