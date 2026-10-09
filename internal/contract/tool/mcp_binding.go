package tool

import (
	"strings"
)

// MCPBinding describes one stable MCP capability and the exact provider-visible
// name currently bound to it. Bindings are host metadata only: they never add
// aliases to provider schemas or alter schema ordering.
type MCPBinding struct {
	Package      string
	Server       string
	RawName      string
	VisibleName  string
	CallableName string
	CapabilityID string
}

func mcpBinding(t Tool) (MCPBinding, bool) {
	meta, ok := t.(MCPMetadata)
	if !ok {
		return MCPBinding{}, false
	}
	server := strings.TrimSpace(meta.MCPServerName())
	raw := strings.TrimSpace(meta.MCPRawToolName())
	if server == "" || raw == "" {
		return MCPBinding{}, false
	}
	visible := raw
	if v, ok := t.(MCPVisibleMetadata); ok && strings.TrimSpace(v.MCPVisibleToolName()) != "" {
		visible = strings.TrimSpace(v.MCPVisibleToolName())
	}
	pkg := ""
	if p, ok := t.(MCPPackageMetadata); ok {
		pkg = strings.TrimSpace(p.MCPPackageName())
	}
	return MCPBinding{
		Package:      pkg,
		Server:       server,
		RawName:      raw,
		VisibleName:  visible,
		CallableName: t.Name(),
		CapabilityID: "mcp-tool:" + server + "/" + raw,
	}, true
}

func mcpBindingAliases(b MCPBinding) []string {
	aliases := []string{
		b.RawName,
		b.VisibleName,
		b.Server + "/" + b.RawName,
		b.Server + "/" + b.VisibleName,
		b.CapabilityID,
		"mcp-tool:" + b.Server + "/" + b.VisibleName,
		"mcp__" + portableMCPPart(b.Server) + "__" + portableMCPPart(b.RawName),
		"mcp__" + portableMCPPart(b.Server) + "__" + portableMCPPart(b.VisibleName),
	}
	if b.Package != "" {
		prefix := "mcp__plugin_" + portableMCPPart(b.Package) + "_" + portableMCPPart(b.Server) + "__"
		aliases = append(aliases, prefix+portableMCPPart(b.RawName), prefix+portableMCPPart(b.VisibleName))
	}
	return aliases
}

// MCPBindingAliases returns accepted portable references for a binding. The
// canonical provider-visible name remains MCPBinding.CallableName.
func MCPBindingAliases(b MCPBinding) []string {
	return append([]string(nil), mcpBindingAliases(b)...)
}

func portableMCPPart(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}
