package plugin

import (
	"path/filepath"
	"strings"
)

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
	if isCodebaseMemorySpec(s) {
		if isStdioSpecType(s.Type) && s.Dir == "" {
			s.Dir = strings.TrimSpace(workspaceRoot)
		}
		// codebase-memory-mcp detects the session root from its subprocess cwd
		// during initialize, then optionally starts its own auto-index thread.
		// Its initial full-tree indexing can be CPU-heavy; keep it out of the
		// foreground scheduling lane just like CodeGraph.
		s.LowPriority = true
	}
	return s
}

func isCodeGraphSpecName(name string) bool {
	return strings.EqualFold(strings.TrimSpace(name), "codegraph")
}

func isCodebaseMemorySpec(s Spec) bool {
	if isCodebaseMemoryID(s.Name) || isCodebaseMemoryCommand(s.Command) {
		return true
	}
	for _, arg := range s.Args {
		if isCodebaseMemoryID(arg) {
			return true
		}
	}
	return false
}

func isCodebaseMemoryCommand(command string) bool {
	command = strings.TrimSpace(command)
	if command == "" {
		return false
	}
	command = strings.ReplaceAll(command, `\`, `/`)
	return isCodebaseMemoryID(filepath.Base(command))
}

func isCodebaseMemoryID(raw string) bool {
	id := strings.ToLower(strings.TrimSpace(raw))
	id = strings.TrimSuffix(id, ".exe")
	id = strings.TrimPrefix(id, "io.github.deusdata/")
	if strings.HasPrefix(id, "codebase-memory-mcp@") {
		return true
	}
	switch id {
	case "codebase-memory-mcp", "codebase-memory":
		return true
	default:
		return false
	}
}

func isStdioSpecType(typ string) bool {
	typ = strings.ToLower(strings.TrimSpace(typ))
	return typ == "" || typ == "stdio"
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
