package plugin

import "strings"

const (
	codeGraphDaemonIdleTimeoutEnv = "CODEGRAPH_DAEMON_IDLE_TIMEOUT_MS"
	// Keep CodeGraph's shared daemon enabled, but do not leave it holding
	// watchers for the upstream default 300s after the last MCP client exits.
	codeGraphDaemonIdleTimeoutDefaultMS = "5000"
)

// ApplyKnownOverrides fills compatibility hints for known MCP servers. These
// are runtime-only adjustments; they do not make a server built-in or change
// startup behavior.
func ApplyKnownOverrides(s Spec, workspaceRoot string) Spec {
	if isCodeGraphSpecName(s.Name) {
		s.ReadOnlyToolNames = mergeReadOnlyToolNames(s.ReadOnlyToolNames, codeGraphReadOnlyToolNames())
		if isStdioSpecType(s.Type) {
			if s.Dir == "" {
				s.Dir = strings.TrimSpace(workspaceRoot)
			}
			s.Env = mergeDefaultEnv(s.Env, codeGraphDaemonIdleTimeoutEnv, codeGraphDaemonIdleTimeoutDefaultMS)
		}
		// CodeGraph does full-tree indexing + file-watching; run it below normal
		// scheduling priority so a background indexer can never starve the user's
		// machine (#3797, #2992). The proc-level mechanism already exists but was
		// never wired to the spec, so it stayed disabled.
		s.LowPriority = true
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

func mergeDefaultEnv(existing map[string]string, key, value string) map[string]string {
	out := make(map[string]string, len(existing)+1)
	for name, v := range existing {
		out[name] = v
	}
	if _, ok := out[key]; !ok {
		out[key] = value
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
