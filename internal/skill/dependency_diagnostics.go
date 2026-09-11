package skill

import (
	"fmt"
	"strings"

	"reasonix/internal/config"
)

// CheckMCPRequirements keeps dependency readiness separate from tool identity.
func CheckMCPRequirements(skills []Skill, plugins []config.PluginEntry, failed map[string]string) []ToolReferenceDiagnostic {
	configured := map[string]bool{}
	for _, p := range plugins {
		configured[strings.TrimSpace(p.Name)] = true
	}
	var out []ToolReferenceDiagnostic
	for _, sk := range skills {
		if !strings.EqualFold(sk.AutoUse, "require") {
			continue
		}
		for _, dep := range sk.Requires {
			dep = strings.TrimSpace(dep)
			server, ok := strings.CutPrefix(dep, "mcp-server:")
			if !ok {
				continue
			}
			code, message := "", ""
			if !configured[server] {
				code = "skill.mcp_dependency_missing"
				message = fmt.Sprintf("skill %q requires %s but that MCP server is not configured", sk.Name, dep)
			} else if reason := failed[server]; reason != "" {
				code = "skill.mcp_dependency_failed"
				message = fmt.Sprintf("skill %q requires %s which is host-failed: %s", sk.Name, dep, reason)
			}
			if code != "" {
				out = append(out, ToolReferenceDiagnostic{sk.Name, dep, code, "warning", message})
			}
		}
	}
	return out
}
