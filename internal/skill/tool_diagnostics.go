package skill

import (
	"fmt"
	"path"
	"strings"

	"reasonix/internal/tool"
)

// ToolReferenceOptions separates known identities from a session snapshot.
// Registered must include hidden tools, not only provider-visible schemas.
type ToolReferenceOptions struct {
	Known      []string
	Registered []tool.ContractEntry
	Bindings   []tool.MCPBinding
}

// ToolReferenceDiagnostic describes a reference without granting any permission.
type ToolReferenceDiagnostic struct {
	Skill, Reference, Code, Severity, Message string
}

// CheckToolReferences is an offline check over the supplied inventory.
func CheckToolReferences(skills []Skill, opts ToolReferenceOptions) []ToolReferenceDiagnostic {
	names := make(map[string]bool)
	for _, name := range opts.Known {
		names[name] = true
	}
	for _, entry := range opts.Registered {
		names[entry.Name] = true
	}
	for _, binding := range opts.Bindings {
		if binding.CallableName != "" {
			names[binding.CallableName] = true
		}
		if binding.CapabilityID != "" {
			names[binding.CapabilityID] = true
		}
	}
	var out []ToolReferenceDiagnostic
	for _, sk := range skills {
		bindings := ToolBindingsForSkill(sk, opts.Bindings)
		for _, ref := range sk.AllowedTools {
			ref = strings.TrimSpace(ref)
			if ref == "" {
				continue
			}
			code, severity, reason := checkToolReference(ref, names, bindings)
			if code != "" {
				out = append(out, ToolReferenceDiagnostic{sk.Name, ref, code, severity,
					fmt.Sprintf("skill %q allowed-tools reference %q %s", sk.Name, ref, reason)})
			}
		}
	}
	return out
}

func checkToolReference(ref string, names map[string]bool, bindings []tool.MCPBinding) (string, string, string) {
	pattern := strings.ContainsAny(ref, "*?[")
	if pattern {
		if _, err := path.Match(ref, ""); err != nil {
			return "skill.tool_reference_invalid", "warning", "has invalid glob syntax"
		}
	}
	if names[ref] {
		return "", "", ""
	}
	if !pattern && invalidMCPReference(ref) {
		return "skill.tool_reference_invalid", "warning", "has an incomplete or invalid MCP reference"
	}
	matched := func(name string) bool {
		if !pattern {
			return ref == name
		}
		ok, _ := path.Match(ref, name)
		return ok
	}
	for name := range names {
		if matched(name) {
			return "", "", ""
		}
	}
	targets := map[string]bool{}
	for _, b := range bindings {
		for _, alias := range append(tool.MCPBindingAliases(b), b.CallableName) {
			if matched(alias) {
				targets[b.CallableName] = true
			}
		}
	}
	if len(targets) == 1 || pattern && len(targets) > 0 {
		return "", "", ""
	}
	if len(targets) > 1 {
		return "skill.tool_reference_ambiguous", "warning", "matches multiple MCP tools; use a qualified reference"
	}
	if pattern || dynamicToolReference(ref) {
		return "skill.tool_reference_unverified", "info", "is unverified by the offline inventory; resolve it in the target session"
	}
	return "skill.tool_reference_unknown", "warning", "is not a known tool identity"
}

func dynamicToolReference(ref string) bool {
	return strings.HasPrefix(ref, "mcp__") || strings.HasPrefix(ref, "mcp-tool:") ||
		strings.HasPrefix(ref, "mcp-server:") || strings.HasPrefix(ref, "mcp_connect__") ||
		strings.HasPrefix(ref, "tool:") || strings.HasPrefix(ref, "skill:") ||
		strings.HasPrefix(ref, "session:") || strings.HasPrefix(ref, "task:") ||
		strings.HasPrefix(ref, "workflow:") || strings.HasPrefix(ref, "source:") ||
		strings.HasPrefix(ref, "web:") || strings.HasPrefix(ref, "lsp:") ||
		strings.HasPrefix(ref, "memory:") || strings.Contains(ref, "/")
}

func invalidMCPReference(ref string) bool {
	switch {
	case strings.HasPrefix(ref, "mcp-tool:"):
		_, _, ok := tool.ParseMCPToolReference(ref)
		return !ok
	case strings.HasPrefix(ref, "mcp-server:"):
		_, ok := tool.ParseMCPServerReference(ref)
		return !ok
	case strings.HasPrefix(ref, "mcp__"):
		_, _, ok := tool.SplitMCPName(ref)
		return !ok
	default:
		return false
	}
}
