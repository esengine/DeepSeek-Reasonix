package cli

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"

	"reasonix/internal/plugin"
)

const mcpMaxItemsPerSection = 6

func renderMCPStatus(width int, servers []plugin.ServerStatus, prompts []plugin.Prompt, resources []plugin.Resource, failures []plugin.Failure, views []plugin.CapabilityView) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", viewHeader("MCP servers (%d)", len(servers)))
	if len(views) > 0 {
		b.WriteString(viewHeader("Capability matrix"))
		b.WriteString("\n")
		b.WriteString(plugin.FormatCapabilityViews(views))
		b.WriteString("\n")
	}

	promptsByServer := map[string][]plugin.Prompt{}
	for _, p := range prompts {
		promptsByServer[p.Server] = append(promptsByServer[p.Server], p)
	}
	resourcesByServer := map[string][]plugin.Resource{}
	for _, r := range resources {
		resourcesByServer[r.Server] = append(resourcesByServer[r.Server], r)
	}

	seen := map[string]bool{}
	for _, s := range servers {
		seen[s.Name] = true
		writeMCPServer(&b, width, s, promptsByServer[s.Name], resourcesByServer[s.Name])
	}
	for _, name := range extraMCPServers(seen, promptsByServer, resourcesByServer) {
		writeMCPServer(&b, width, plugin.ServerStatus{Name: name}, promptsByServer[name], resourcesByServer[name])
	}
	for _, f := range failures {
		writeMCPFailure(&b, width, f)
	}
	return strings.TrimRight(b.String(), "\n")
}

func extraMCPServers(seen map[string]bool, prompts map[string][]plugin.Prompt, resources map[string][]plugin.Resource) []string {
	set := map[string]bool{}
	for name := range prompts {
		if !seen[name] {
			set[name] = true
		}
	}
	for name := range resources {
		if !seen[name] {
			set[name] = true
		}
	}
	var names []string
	for name := range set {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func writeMCPServer(b *strings.Builder, width int, s plugin.ServerStatus, prompts []plugin.Prompt, resources []plugin.Resource) {
	transport := s.Transport
	if transport == "" {
		transport = "unknown"
	}
	meta := fmt.Sprintf("(%s)  %s · %s · %s", transport, countText(s.Tools, "tool"), countText(len(prompts), "prompt"), countText(len(resources), "resource"))
	if src := sanitizeExternalDisplayText(s.ConfigSource); src != "" {
		meta += " · source=" + src
	}
	if protocol := sanitizeExternalDisplayText(s.ProtocolVersion); protocol != "" {
		meta += " · protocol=" + protocol
	}
	if state := sanitizeExternalDisplayText(string(s.SessionState)); state != "" {
		meta += " · session=" + state
	}
	if s.ReconnectAttempts > 0 {
		meta += fmt.Sprintf(" · reconnect=%d/5", s.ReconnectAttempts)
	}
	if kind := sanitizeExternalDisplayText(string(s.LastErrorKind)); kind != "" {
		meta += " · error=" + kind
	}
	invalidTools := invalidMCPTools(s.ToolList)
	availableTools := validMCPTools(s.ToolList)
	if len(invalidTools) > 0 {
		meta += " · " + countText(len(invalidTools), "unavailable tool")
	}
	name := viewCompactText(sanitizeExternalDisplayText(s.Name), viewBudget(width, 4+2+1+visibleWidth(meta)))
	fmt.Fprintf(b, "    %s %s %s\n", accent("✓"), bold(name), viewMeta(meta))
	if len(availableTools) > 0 {
		writeMCPToolList(b, width, s, availableTools)
	}
	if len(prompts) > 0 {
		writeMCPPromptList(b, width, prompts)
	}
	if len(resources) > 0 {
		writeMCPResourceList(b, width, resources)
	}
	if len(invalidTools) > 0 {
		b.WriteString(viewSubhead("    unavailable tools") + "\n")
		limit := min(len(invalidTools), mcpMaxItemsPerSection)
		for _, t := range invalidTools[:limit] {
			writeMCPItem(b, width, "      ", sanitizeExternalDisplayText(t.Name), sanitizeExternalDisplayText(t.SchemaError))
		}
		if extra := len(invalidTools) - limit; extra > 0 {
			fmt.Fprintf(b, "    %s\n", viewMore(extra, "unavailable tools"))
		}
	}
}

// writeMCPToolList lists valid connected tools under the server, tagging the
// config-plane source when known so operators can trace a tool back to its
// registration (#6578 / Integration D/E). Callers pass pre-filtered tools
// (SchemaError == "") so quarantined tools only appear under unavailable.
func writeMCPToolList(b *strings.Builder, width int, s plugin.ServerStatus, tools []plugin.ToolInfo) {
	if len(tools) == 0 {
		return
	}
	b.WriteString(viewSubhead("    tools") + "\n")
	limit := min(len(tools), mcpMaxItemsPerSection)
	src := sanitizeExternalDisplayText(s.ConfigSource)
	for _, t := range tools[:limit] {
		detail := sanitizeExternalDisplayText(t.Description)
		if src != "" {
			if detail != "" {
				detail = detail + " · source=" + src
			} else {
				detail = "source=" + src
			}
		}
		writeMCPItem(b, width, "      ", sanitizeExternalDisplayText(t.Name), detail)
	}
	if extra := len(tools) - limit; extra > 0 {
		fmt.Fprintf(b, "    %s\n", viewMore(extra, "tools"))
	}
}

func invalidMCPTools(tools []plugin.ToolInfo) []plugin.ToolInfo {
	out := make([]plugin.ToolInfo, 0)
	for _, t := range tools {
		if t.SchemaError != "" {
			out = append(out, t)
		}
	}
	return out
}

func validMCPTools(tools []plugin.ToolInfo) []plugin.ToolInfo {
	out := make([]plugin.ToolInfo, 0, len(tools))
	for _, t := range tools {
		if t.SchemaError == "" {
			out = append(out, t)
		}
	}
	return out
}

func writeMCPFailure(b *strings.Builder, width int, f plugin.Failure) {
	transport := f.Transport
	if transport == "" {
		transport = "unknown"
	}
	meta := fmt.Sprintf("(%s)  %s", transport, sanitizeExternalDisplayText(f.Error))
	name := viewCompactText(sanitizeExternalDisplayText(f.Name), viewBudget(width, 4+2+1+visibleWidth(meta)))
	fmt.Fprintf(b, "    %s %s %s\n", yellow("!"), bold(name), viewMeta(viewCompactText(meta, viewBudget(width, 10+visibleWidth(name)))))
}

func writeMCPPromptList(b *strings.Builder, width int, prompts []plugin.Prompt) {
	b.WriteString(viewSubhead("    prompts") + "\n")
	limit := min(len(prompts), mcpMaxItemsPerSection)
	for _, p := range prompts[:limit] {
		writeMCPItem(b, width, "      ", "/"+sanitizeExternalDisplayText(p.Name), sanitizeExternalDisplayText(p.Description))
	}
	if extra := len(prompts) - limit; extra > 0 {
		fmt.Fprintf(b, "    %s\n", viewMore(extra, "prompts"))
	}
}

func writeMCPResourceList(b *strings.Builder, width int, resources []plugin.Resource) {
	b.WriteString(viewSubhead("    resources") + "\n")
	limit := min(len(resources), mcpMaxItemsPerSection)
	for _, r := range resources[:limit] {
		label := sanitizeExternalDisplayText(r.Name)
		if label == "" {
			label = sanitizeExternalDisplayText(r.Description)
		}
		if r.MimeType != "" {
			mime := sanitizeExternalDisplayText(r.MimeType)
			if label == "" {
				label = mime
			} else {
				label += " [" + mime + "]"
			}
		}
		writeMCPItem(b, width, "      ", "@"+sanitizeExternalDisplayText(r.Server)+":"+sanitizeExternalDisplayText(r.URI), label)
	}
	if extra := len(resources) - limit; extra > 0 {
		fmt.Fprintf(b, "    %s\n", viewMore(extra, "resources"))
	}
}

func writeMCPItem(b *strings.Builder, width int, indent, ref, desc string) {
	desc = sanitizeExternalDisplayText(desc)
	ref = sanitizeExternalDisplayText(ref)
	available := viewBudget(width, visibleWidth(indent))
	if desc == "" || available < 30 {
		b.WriteString(indent + compactMiddle(ref, available))
		b.WriteByte('\n')
		return
	}
	descBudget := min(40, max(12, available/2))
	refBudget := available - 2 - descBudget
	if refBudget < 16 {
		refBudget = min(16, available)
		descBudget = available - refBudget - 2
	}
	line := indent + compactMiddle(ref, refBudget)
	if descBudget >= 12 {
		line += "  " + viewMeta(viewCompactText(desc, descBudget))
	}
	b.WriteString(line)
	b.WriteByte('\n')
}

// sanitizeExternalDisplayText strips ANSI/OSC/C0 control sequences from text
// that originated outside Reasonix (MCP tool/prompt/resource fields, source
// labels, failure messages) before it is rendered into the TUI. TrimSpace and
// Fields alone leave CSI sequences intact and would let a malicious MCP rewrite
// the terminal, spoof chrome, or poke the clipboard.
func sanitizeExternalDisplayText(s string) string {
	s = ansi.Strip(s)
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\t' || r == '\n' || r == '\r':
			b.WriteByte(' ')
		case r < 0x20 || r == 0x7f:
			// Drop remaining C0 controls and DEL.
		case r >= 0x80 && r <= 0x9f:
			// Drop C1 controls (including after partial decode).
		case unicode.Is(unicode.Cc, r):
			// Other control categories.
		default:
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func oneLineText(s string) string {
	return sanitizeExternalDisplayText(s)
}

func countText(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

func compactEnd(s string, maxWidth int) string {
	if maxWidth <= 0 || visibleWidth(s) <= maxWidth {
		return s
	}
	if maxWidth <= 1 {
		return "…"
	}
	var out strings.Builder
	for _, r := range s {
		next := out.String() + string(r)
		if visibleWidth(next)+1 > maxWidth {
			break
		}
		out.WriteRune(r)
	}
	return out.String() + "…"
}

func compactMiddle(s string, maxWidth int) string {
	if maxWidth <= 0 || visibleWidth(s) <= maxWidth {
		return s
	}
	if maxWidth <= 3 {
		return compactEnd(s, maxWidth)
	}
	keep := maxWidth - 1
	leftWidth := keep / 2
	rightWidth := keep - leftWidth
	left := takeLeftWidth(s, leftWidth)
	right := takeRightWidth(s, rightWidth)
	return left + "…" + right
}

func takeLeftWidth(s string, maxWidth int) string {
	var out strings.Builder
	for _, r := range s {
		next := out.String() + string(r)
		if visibleWidth(next) > maxWidth {
			break
		}
		out.WriteRune(r)
	}
	return out.String()
}

func takeRightWidth(s string, maxWidth int) string {
	var out []rune
	width := 0
	for _, r := range reverseRunes([]rune(s)) {
		w := visibleWidth(string(r))
		if width+w > maxWidth {
			break
		}
		out = append(out, r)
		width += w
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return string(out)
}

func reverseRunes(in []rune) []rune {
	out := append([]rune(nil), in...)
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}
