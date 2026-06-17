package plugin

import "strings"

// ApplyKnownOverrides fills compatibility hints for known MCP servers. These
// are runtime-only adjustments; they do not make a server built-in or change
// startup behavior.
func ApplyKnownOverrides(s Spec, workspaceRoot string) Spec {
	if isCodeGraphSpecName(s.Name) {
		s.ReadOnlyToolNames = mergeReadOnlyToolNames(s.ReadOnlyToolNames, codeGraphReadOnlyToolNames())
		if s.Dir == "" && isStdioSpecType(s.Type) {
			s.Dir = strings.TrimSpace(workspaceRoot)
		}
	}
	return s
}

// ApplyKnownReadOnlyOverrides fills compatibility read-only hints for MCP
// servers whose read surfaces are stable but older runtimes may omit MCP
// annotations. It does not make the server built-in or change startup behavior.
func ApplyKnownReadOnlyOverrides(s Spec) Spec {
	return ApplyKnownOverrides(s, "")
}

func isCodeGraphSpecName(name string) bool {
	return strings.EqualFold(strings.TrimSpace(name), "codegraph")
}

func isStdioSpecType(typ string) bool {
	typ = strings.ToLower(strings.TrimSpace(typ))
	return typ == "" || typ == "stdio"
}

func mergeReadOnlyToolNames(existing map[string]bool, extra map[string]bool) map[string]bool {
	out := make(map[string]bool, len(existing)+len(extra))
	for name, ok := range existing {
		out[name] = ok
	}
	for name, ok := range extra {
		if ok {
			out[name] = true
		}
	}
	return out
}

func codeGraphReadOnlyToolNames() map[string]bool {
	base := []string{
		"callees",
		"callers",
		"context",
		"explore",
		"files",
		"impact",
		"node",
		"search",
		"status",
		"trace",
	}
	out := make(map[string]bool, len(base)*2)
	for _, name := range base {
		out[name] = true
		out["codegraph_"+name] = true
	}
	return out
}
