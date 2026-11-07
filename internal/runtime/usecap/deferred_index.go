package usecap

import (
	"strings"

	"reasonix/internal/contract/tool"
)

// deferredIndexTools are the built-ins a model reaches for by name before it
// searches: hidden from the provider schema, so without a line here a
// directly visible browser is the only way it can see to read a URL.
var deferredIndexTools = []string{"web_fetch", "web_search", "browser_open", "browser_read", "browser_act", "grep", "glob", "ls", "present_files", "setting", "compress", "context_budget"}

const deferredLeadBytes = 72

// deferredIndex is one line naming the indexed tools the provider cannot see,
// each with the first sentence of its own description. It reads the registry,
// so it cannot promise a tool the session does not have, and it is the same
// bytes for the same registry.
func deferredIndex(reg *tool.Registry) string {
	if reg == nil {
		return ""
	}
	var rows []string
	hasFetch := false
	for _, name := range deferredIndexTools {
		t, ok := reg.Get(name)
		if !ok || reg.ProviderVisible(name) {
			continue
		}
		if name == "web_fetch" {
			hasFetch = true
		}
		row := "tool:" + name
		description, _, _ := strings.Cut(t.Description(), " (")
		if lead := boundedLead(description, deferredLeadBytes); lead != "" {
			row += " (" + lead + ")"
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		return ""
	}
	out := " Deferred tools, called by id: " + strings.Join(rows, "; ") + "."
	if hasFetch {
		out += " Read JSON or static text with tool:web_fetch; open a browser only for pages that need rendering, clicks or a login."
	}
	return out
}

func (t *UseCapabilityTool) Description() string {
	if t == nil {
		return useCapabilityBase
	}
	return useCapabilityBase + deferredIndex(t.registry)
}

const useCapabilityBase = "Search and call deferred tools, skills, and MCP through stable IDs such as tool:grep, skill:review, task:subagent, and mcp-tool:server/tool. Before saying a tool, network, or integration is unavailable, search by natural-language goal; results include ranked IDs and available schemas. list enumerates the catalog, inspect loads one definition, call invokes it, and decline records why a preferred capability is skipped. task:subagent delegates 1-8 tasks that run in parallel. Calls retain all permission, sandbox, and execution checks."
