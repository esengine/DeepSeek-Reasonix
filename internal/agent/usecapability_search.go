package agent

import (
	"encoding/json"
	"sort"
	"strings"
	"unicode"

	"reasonix/internal/capability"
	"reasonix/internal/plugin"
	"reasonix/internal/tool"
)

type capabilitySearchResult struct {
	CapabilityID string   `json:"capability_id"`
	Kind         string   `json:"kind"`
	Name         string   `json:"name"`
	Description  string   `json:"description,omitempty"`
	Status       string   `json:"status,omitempty"`
	ReadOnly     bool     `json:"read_only"`
	Arguments    []string `json:"argument_names,omitempty"`
	score        int
}

// searchCapabilities ranks the in-memory catalog and schema cache only. It is
// deliberately incapable of starting an MCP server or issuing tools/list.
func (t *UseCapabilityTool) searchCapabilities(query string, limit int) (string, int, error) {
	if limit == 0 {
		limit = 5
	}
	query = strings.TrimSpace(query)
	queryNorm := normalizeSearchText(query)
	queryTokens := searchTokens(query)
	cat := t.currentCatalog()
	mcpSchemas := t.mcpSearchSchemaIndex()
	results := make([]capabilitySearchResult, 0, len(cat.Entries))
	for _, entry := range cat.Entries {
		arguments, schemaText := t.capabilitySchemaSearchData(entry, mcpSchemas)
		document := strings.Join([]string{entry.ID, entry.Name, entry.Source, entry.ToolName, entry.Description, schemaText}, " ")
		score := capabilitySearchScore(entry, document, queryNorm, queryTokens)
		if score == 0 {
			continue
		}
		results = append(results, capabilitySearchResult{
			CapabilityID: entry.ID,
			Kind:         string(entry.Kind),
			Name:         entry.Name,
			Description:  truncateSearchDescription(entry.Description),
			Status:       string(entry.Status),
			ReadOnly:     entry.ReadOnly,
			Arguments:    arguments,
			score:        score,
		})
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].score != results[j].score {
			return results[i].score > results[j].score
		}
		return results[i].CapabilityID < results[j].CapabilityID
	})
	if len(results) > limit {
		results = results[:limit]
	}
	payload := struct {
		Query   string                   `json:"query"`
		Results []capabilitySearchResult `json:"results"`
		Note    string                   `json:"note"`
	}{
		Query:   query,
		Results: results,
		Note:    "Local catalog search only; no MCP process, network request, or tools/list call was made. Inspect one exact capability_id before calling when its argument contract is unfamiliar.",
	}
	b, err := json.MarshalIndent(payload, "", "  ")
	return string(b), len(results), err
}

func capabilitySearchScore(entry capability.Entry, document, queryNorm string, queryTokens []string) int {
	id := normalizeSearchText(entry.ID)
	name := normalizeSearchText(entry.Name)
	doc := normalizeSearchText(document)
	score := 0
	switch {
	case id == queryNorm:
		score += 10000
	case name == queryNorm:
		score += 8000
	case strings.Contains(id, queryNorm):
		score += 3000
	case strings.Contains(name, queryNorm):
		score += 2000
	case strings.Contains(doc, queryNorm):
		score += 1000
	}
	for _, token := range queryTokens {
		if token == "" {
			continue
		}
		switch {
		case containsSearchToken(id, token):
			score += 300
		case containsSearchToken(name, token):
			score += 200
		case containsSearchToken(doc, token):
			score += 80
		}
	}
	return score
}

func (t *UseCapabilityTool) capabilitySchemaSearchData(entry capability.Entry, mcpSchemas map[string]plugin.CachedTool) ([]string, string) {
	var schema json.RawMessage
	if entry.Kind == capability.KindMCPTool {
		server, raw, err := parseMCPCapabilityID(entry.ID)
		if err == nil {
			if cached, ok := mcpSchemas[server+"\x00"+raw]; ok {
				schema = cached.Schema
			}
		}
	} else if strings.HasPrefix(entry.ID, "skill:") {
		if contract, ok := t.capabilityArgumentContract(entry.ID); ok {
			schema = contract.Schema
		}
	} else if t.registry != nil {
		name := strings.TrimSpace(entry.ToolName)
		if name == "" {
			name = strings.TrimSpace(strings.TrimPrefix(entry.ID, "tool:"))
		}
		if target, ok := t.registry.Get(name); ok {
			schema = target.Schema()
		}
	}
	return schemaSearchData(schema)
}

// mcpSearchSchemaIndex takes one local snapshot per search. The old per-entry
// localMCPTool path deep-copied the whole runtime catalog for every MCP tool,
// making a large 88KB catalog quadratic even though discovery is local-only.
func (t *UseCapabilityTool) mcpSearchSchemaIndex() map[string]plugin.CachedTool {
	index := map[string]plugin.CachedTool{}
	add := func(server string, tools []plugin.CachedTool) {
		for _, cached := range tools {
			key := server + "\x00" + cached.Name
			if _, exists := index[key]; !exists {
				index[key] = cached
			}
		}
	}
	if t.runtime != nil {
		_, cached, _, _, live := t.runtime.CapabilityCatalogState()
		for server, tools := range live {
			add(server, tools)
		}
		for server, tools := range cached {
			add(server, tools)
		}
	} else {
		for server, tools := range t.ensureState().snapshotLiveTools() {
			add(server, tools)
		}
		if t.host != nil {
			for _, spec := range t.specs {
				if live, ok := t.host.CachedTools(spec.Name); ok {
					add(spec.Name, snapshotMCPTools(live))
				}
			}
		}
	}
	if t.registry != nil {
		for _, name := range t.registry.AllNames() {
			target, ok := t.registry.Get(name)
			if !ok {
				continue
			}
			metadata, ok := target.(tool.MCPMetadata)
			if !ok {
				continue
			}
			add(metadata.MCPServerName(), []plugin.CachedTool{{
				Name: metadata.MCPRawToolName(), Description: target.Description(),
				Schema: target.Schema(), ReadOnly: target.ReadOnly(),
			}})
		}
	}
	for _, spec := range t.specs {
		if cached, ok := plugin.LoadCachedSchemaForSpecProfile(spec, t.hostProfileFor()); ok {
			add(spec.Name, cached.Tools)
		}
	}
	return index
}

func schemaSearchData(raw json.RawMessage) ([]string, string) {
	var root struct {
		Properties map[string]struct {
			Type        any    `json:"type"`
			Description string `json:"description"`
		} `json:"properties"`
	}
	if json.Unmarshal(raw, &root) != nil || len(root.Properties) == 0 {
		return nil, ""
	}
	names := make([]string, 0, len(root.Properties))
	parts := make([]string, 0, len(root.Properties))
	for name := range root.Properties {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		property := root.Properties[name]
		parts = append(parts, name+" "+property.Description)
	}
	return names, strings.Join(parts, " ")
}

func (t *UseCapabilityTool) capabilityArgumentContract(id string) (tool.CapabilityArgumentContract, bool) {
	if t.registry == nil {
		return tool.CapabilityArgumentContract{}, false
	}
	for _, name := range []string{"run_skill", "read_only_skill", "read_skill"} {
		target, ok := t.registry.Get(name)
		if !ok {
			continue
		}
		provider, ok := target.(tool.CapabilityArgumentProvider)
		if !ok {
			continue
		}
		if contract, ok := provider.CapabilityArguments(id); ok {
			return contract, true
		}
	}
	return tool.CapabilityArgumentContract{}, false
}

// localMCPTools returns live/shared-host metadata first, then the already
// registered adapter, then disk/schema cache metadata. It performs no remote
// calls and never starts a server.
func (t *UseCapabilityTool) localMCPTools(server string) ([]plugin.CachedTool, string) {
	if t.runtime != nil {
		_, cached, _, _, live := t.runtime.CapabilityCatalogState()
		if tools := live[server]; len(tools) > 0 {
			return cloneCachedTools(tools), "shared_host"
		}
		if tools := cached[server]; len(tools) > 0 {
			return cloneCachedTools(tools), "disk_cache"
		}
	} else if tools := t.ensureState().snapshotLiveTools()[server]; len(tools) > 0 {
		return cloneCachedTools(tools), "shared_host"
	} else if t.host != nil {
		if live, ok := t.host.CachedTools(server); ok && len(live) > 0 {
			cached := snapshotMCPTools(live)
			t.ensureState().setLiveTools(server, cached)
			return cloneCachedTools(cached), "shared_host"
		}
	}
	if t.registry != nil {
		var registered []plugin.CachedTool
		for _, name := range t.registry.AllNames() {
			target, ok := t.registry.Get(name)
			if !ok {
				continue
			}
			metadata, ok := target.(tool.MCPMetadata)
			if !ok || metadata.MCPServerName() != server {
				continue
			}
			registered = append(registered, plugin.CachedTool{
				Name:        metadata.MCPRawToolName(),
				Description: target.Description(),
				Schema:      target.Schema(),
				ReadOnly:    target.ReadOnly(),
			})
		}
		if len(registered) > 0 {
			sort.Slice(registered, func(i, j int) bool { return registered[i].Name < registered[j].Name })
			return registered, "shared_host"
		}
	}
	if spec, ok := t.specFor(server); ok {
		if cached, ok := plugin.LoadCachedSchemaForSpecProfile(spec, t.hostProfileFor()); ok && len(cached.Tools) > 0 {
			return cloneCachedTools(cached.Tools), "disk_cache"
		}
	}
	return nil, ""
}

func (t *UseCapabilityTool) localMCPTool(server, raw string) (plugin.CachedTool, string, bool) {
	tools, source := t.localMCPTools(server)
	for _, candidate := range tools {
		if candidate.Name == raw {
			return candidate, source, true
		}
	}
	return plugin.CachedTool{}, source, false
}

func searchTokens(value string) []string {
	normalized := normalizeSearchText(value)
	if normalized == "" {
		return nil
	}
	return strings.Fields(normalized)
}

func normalizeSearchText(value string) string {
	var b strings.Builder
	var previous rune
	for i, r := range value {
		if i > 0 && unicode.IsUpper(r) && (unicode.IsLower(previous) || unicode.IsDigit(previous)) {
			b.WriteByte(' ')
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
		} else {
			b.WriteByte(' ')
		}
		previous = r
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func containsSearchToken(document, token string) bool {
	for candidate := range strings.FieldsSeq(document) {
		if candidate == token || strings.Contains(candidate, token) {
			return true
		}
	}
	return false
}

func truncateSearchDescription(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 240 {
		return value
	}
	return value[:237] + "..."
}
