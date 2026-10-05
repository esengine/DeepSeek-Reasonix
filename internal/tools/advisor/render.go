package advisor

import (
	"fmt"
	"strings"

	"reasonix/internal/contract/provider"
)

const (
	userClip   = 8 * 1024
	agentClip  = 8 * 1024
	argsClip   = 2 * 1024
	resultClip = 6 * 1024
)

// renderTranscript lays the conversation out as text for a model that reads it
// rather than continues it: tool calls cannot be replayed into a request that
// declares no tools. The first user message states the task and is always kept;
// the rest is kept from the newest back until the budget is spent.
func renderTranscript(msgs []provider.Message, budget int) string {
	var blocks []string
	first := -1
	for _, m := range msgs {
		block := renderMessage(m)
		if block == "" {
			continue
		}
		if first < 0 && m.Role == provider.RoleUser {
			first = len(blocks)
		}
		blocks = append(blocks, block)
	}
	var b strings.Builder
	b.WriteString("# The agent's conversation so far\n")
	if len(blocks) == 0 {
		b.WriteString("\n(empty)\n")
		return b.String()
	}
	spent, from := 0, len(blocks)
	if first >= 0 {
		spent = len(blocks[first])
	}
	for from > first+1 && spent+len(blocks[from-1]) <= budget {
		from--
		spent += len(blocks[from])
	}
	if first >= 0 {
		b.WriteString(blocks[first])
	}
	if omitted := from - (first + 1); omitted > 0 {
		fmt.Fprintf(&b, "\n[… %d earlier messages left out to fit …]\n", omitted)
	}
	for _, block := range blocks[from:] {
		b.WriteString(block)
	}
	return b.String()
}

func renderMessage(m provider.Message) string {
	if m.LocalOnly {
		return ""
	}
	switch m.Role {
	case provider.RoleUser:
		return "\n## User\n\n" + clip(m.Content, userClip) + "\n"
	case provider.RoleAssistant:
		var b strings.Builder
		b.WriteString("\n## Agent\n")
		if text := strings.TrimSpace(m.Content); text != "" {
			b.WriteString("\n" + clip(text, agentClip) + "\n")
		}
		for _, call := range m.ToolCalls {
			fmt.Fprintf(&b, "\n→ %s(%s)\n", call.Name, clip(call.Arguments, argsClip))
		}
		return b.String()
	case provider.RoleTool:
		return "\n## Result of " + m.Name + "\n\n" + clip(m.Content, resultClip) + "\n"
	default:
		return ""
	}
}

// clip keeps the head and the tail of a long text, where a result's command
// line and its verdict usually sit.
func clip(s string, limit int) string {
	s = strings.TrimSpace(s)
	if len(s) <= limit {
		return s
	}
	head, tail := limit*3/4, limit/4
	return strings.ToValidUTF8(s[:head], "") + "\n[… " + fmt.Sprint(len(s)-head-tail) + " bytes left out …]\n" +
		strings.ToValidUTF8(s[len(s)-tail:], "")
}
