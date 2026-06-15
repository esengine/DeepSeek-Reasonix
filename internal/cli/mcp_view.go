package cli

import (
	"fmt"
	"sort"
	"strings"

	"reasonix/internal/plugin"
)

const mcpMaxItemsPerSection = 6

func renderMCPStatus(width int, servers []plugin.ServerStatus, prompts []plugin.Prompt, resources []plugin.Resource, failures []plugin.Failure) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", viewHeader("MCP servers (%d)", len(servers)))

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
	name := viewCompactText(s.Name, viewBudget(width, 4+2+1+visibleWidth(meta)))
	fmt.Fprintf(b, "    %s %s %s\n", accent("✓"), bold(name), viewMeta(meta))
	if len(prompts) > 0 {
		writeMCPPromptList(b, width, prompts)
	}
	if len(resources) > 0 {
		writeMCPResourceList(b, width, resources)
	}
}

func writeMCPFailure(b *strings.Builder, width int, f plugin.Failure) {
	transport := f.Transport
	if transport == "" {
		transport = "unknown"
	}
	meta := fmt.Sprintf("(%s)  %s", transport, oneLineText(f.Error))
	name := viewCompactText(f.Name, viewBudget(width, 4+2+1+visibleWidth(meta)))
	fmt.Fprintf(b, "    %s %s %s\n", yellow("!"), bold(name), viewMeta(viewCompactText(meta, viewBudget(width, 10+visibleWidth(name)))))
}

func writeMCPPromptList(b *strings.Builder, width int, prompts []plugin.Prompt) {
	b.WriteString(viewSubhead("    prompts") + "\n")
	limit := len(prompts)
	if limit > mcpMaxItemsPerSection {
		limit = mcpMaxItemsPerSection
	}
	for _, p := range prompts[:limit] {
		writeMCPItem(b, width, "      ", "/"+p.Name, p.Description)
	}
	if extra := len(prompts) - limit; extra > 0 {
		fmt.Fprintf(b, "    %s\n", viewMore(extra, "prompts"))
	}
}

func writeMCPResourceList(b *strings.Builder, width int, resources []plugin.Resource) {
	b.WriteString(viewSubhead("    resources") + "\n")
	limit := len(resources)
	if limit > mcpMaxItemsPerSection {
		limit = mcpMaxItemsPerSection
	}
	for _, r := range resources[:limit] {
		label := strings.TrimSpace(r.Name)
		if label == "" {
			label = strings.TrimSpace(r.Description)
		}
		if r.MimeType != "" {
			if label == "" {
				label = r.MimeType
			} else {
				label += " [" + r.MimeType + "]"
			}
		}
		writeMCPItem(b, width, "      ", "@"+r.Server+":"+r.URI, label)
	}
	if extra := len(resources) - limit; extra > 0 {
		fmt.Fprintf(b, "    %s\n", viewMore(extra, "resources"))
	}
}

func writeMCPItem(b *strings.Builder, width int, indent, ref, desc string) {
	desc = oneLineText(desc)
	ref = oneLineText(ref)
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

func oneLineText(s string) string {
	return strings.Join(strings.Fields(s), " ")
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
