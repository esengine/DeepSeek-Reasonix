package doctor

import (
	"fmt"
	"strings"

	"reasonix/internal/config"
	"reasonix/internal/skill"
	"reasonix/internal/tool"
)

// SkillHealthOptions configures skill/MCP capability diagnostics for doctor.
type SkillHealthOptions struct {
	Skills  []skill.Skill
	Tools   []tool.ContractEntry
	Plugins []config.PluginEntry
	// FailedServers maps MCP server name → host-proven failure reason.
	FailedServers map[string]string
	// CacheMismatch lists MCP servers whose schema cache fingerprint mismatched.
	CacheMismatch []string
}

// CollectSkillHealthWarnings returns human-readable skill/MCP health warnings.
func CollectSkillHealthWarnings(opts SkillHealthOptions) []string {
	var out []string
	toolNames := map[string]bool{}
	for _, t := range opts.Tools {
		toolNames[t.Name] = true
	}
	pluginNames := map[string]bool{}
	for _, p := range opts.Plugins {
		pluginNames[strings.TrimSpace(p.Name)] = true
	}

	// Detect require skills with identical trigger sets (ambiguous conflicts).
	requireTriggers := map[string][]string{} // key=sorted triggers → skill names

	for _, sk := range opts.Skills {
		name := sk.Name
		desc := strings.TrimSpace(sk.Description)
		if desc == "" || strings.Contains(desc, "no description") || desc == "(no description)" {
			out = append(out, fmt.Sprintf("skill %q has a missing or placeholder description", name))
		}
		// Trigger / negative-trigger conflicts.
		neg := map[string]bool{}
		for _, n := range sk.NegativeTriggers {
			neg[strings.ToLower(strings.TrimSpace(n))] = true
		}
		for _, tr := range sk.Triggers {
			if neg[strings.ToLower(strings.TrimSpace(tr))] {
				out = append(out, fmt.Sprintf("skill %q trigger %q also appears in negative-triggers", name, tr))
			}
		}
		// auto-use require with missing dependencies.
		if strings.EqualFold(sk.AutoUse, "require") {
			for _, dep := range sk.Requires {
				dep = strings.TrimSpace(dep)
				if dep == "" {
					continue
				}
				if strings.HasPrefix(dep, "mcp-server:") {
					srv := strings.TrimPrefix(dep, "mcp-server:")
					if !pluginNames[srv] {
						out = append(out, fmt.Sprintf("skill %q requires %s but that MCP server is not configured", name, dep))
					} else if reason, ok := opts.FailedServers[srv]; ok && reason != "" {
						out = append(out, fmt.Sprintf("skill %q requires %s which is host-failed: %s", name, dep, reason))
					}
				}
			}
			key := strings.Join(normalizedTriggers(sk.Triggers), "|")
			if key != "" {
				requireTriggers[key] = append(requireTriggers[key], name)
			}
		}
		// allowed-tools references unavailable tools.
		for _, at := range sk.AllowedTools {
			at = strings.TrimSpace(at)
			if at == "" {
				continue
			}
			if !toolNames[at] && !isBuiltinOrMetaTool(at) {
				// Soft: only warn when the name looks concrete and missing.
				out = append(out, fmt.Sprintf("skill %q allowed-tools references %q which is not in the current registry", name, at))
			}
		}
		// The parser drops illegal profiles values from Profiles but preserves
		// them in InvalidProfiles precisely so this check can reach them.
		for _, p := range sk.InvalidProfiles {
			out = append(out, fmt.Sprintf("skill %q has illegal profiles value %q (valid: economy, balanced, delivery)", name, p))
		}
	}

	for key, names := range requireTriggers {
		if len(names) > 1 {
			out = append(out, fmt.Sprintf("multiple require skills share identical triggers [%s]: %s", key, strings.Join(names, ", ")))
		}
	}

	for _, srv := range opts.CacheMismatch {
		out = append(out, fmt.Sprintf("MCP server %q schema cache fingerprint mismatched; tools may be stale until reconnect", srv))
	}
	for srv, reason := range opts.FailedServers {
		out = append(out, fmt.Sprintf("MCP server %q is in a host-failed state: %s", srv, reason))
	}
	return out
}

func normalizedTriggers(in []string) []string {
	out := make([]string, 0, len(in))
	for _, t := range in {
		t = strings.ToLower(strings.TrimSpace(t))
		if t != "" {
			out = append(out, t)
		}
	}
	// sort-like: simple insertion for small lists
	for i := 1; i < len(out); i++ {
		j := i
		for j > 0 && out[j] < out[j-1] {
			out[j], out[j-1] = out[j-1], out[j]
			j--
		}
	}
	return out
}

func isBuiltinOrMetaTool(name string) bool {
	switch name {
	case "bash", "read_file", "write_file", "edit_file", "grep", "glob", "ls",
		"todo_write", "complete_step", "ask", "task", "read_only_task",
		"run_skill", "read_skill", "read_only_skill", "explore", "research",
		"review", "security_review", "web_fetch", "multi_edit", "move_file",
		"code_index", "wait", "bash_output", "kill_shell":
		return true
	default:
		return strings.HasPrefix(name, "mcp__") || strings.HasPrefix(name, "lsp_")
	}
}
