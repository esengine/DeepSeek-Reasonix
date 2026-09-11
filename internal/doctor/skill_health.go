package doctor

import (
	"fmt"
	"strings"

	"reasonix/internal/config"
	"reasonix/internal/skill"
	"reasonix/internal/tool"
	_ "reasonix/internal/tool/builtin" // Initialize compile-time tool identities.
)

// SkillHealthOptions configures skill/MCP capability diagnostics for doctor.
type SkillHealthOptions struct {
	Skills   []skill.Skill
	Tools    []tool.ContractEntry
	Bindings []tool.MCPBinding
	Plugins  []config.PluginEntry
	// FailedServers maps MCP server name → host-proven failure reason.
	FailedServers map[string]string
	// CacheMismatch lists MCP servers whose schema cache fingerprint mismatched.
	CacheMismatch []string
}

// CollectSkillHealthWarnings returns human-readable skill/MCP health warnings.
func CollectSkillHealthWarnings(opts SkillHealthOptions) []string {
	var out []string
	for _, diagnostic := range skill.CheckToolReferences(opts.Skills, skill.ToolReferenceOptions{Known: tool.KnownToolNames(), Registered: opts.Tools, Bindings: opts.Bindings}) {
		out = append(out, diagnostic.Message)
	}
	for _, d := range skill.CheckMCPRequirements(opts.Skills, opts.Plugins, opts.FailedServers) {
		out = append(out, d.Message)
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
			key := strings.Join(normalizedTriggers(sk.Triggers), "|")
			if key != "" {
				requireTriggers[key] = append(requireTriggers[key], name)
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
