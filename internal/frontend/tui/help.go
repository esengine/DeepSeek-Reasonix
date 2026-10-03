package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/frontend/termrender"
)

// helpMaxDynamicItems caps each group the session grows on its own; the
// built-ins are always listed whole.
const helpMaxDynamicItems = 8

type helpMsg struct {
	items []CompletionItem
	err   error
}

// isSetup is the command /setup and its 1.x alias.
func isSetup(line string) bool { return line == "/setup" || line == "/auth" }

// isHelp is the command /help and its 1.x alias.
func isHelp(line string) bool { return line == "/help" || line == "/?" }

// showHelp lists every command this session answers, read from the same
// catalogue the completion menu offers.
func (m *model) showHelp() tea.Cmd {
	return func() tea.Msg {
		c, err := m.client.Complete(m.ctx, "/", 1)
		return helpMsg{items: c.Items, err: err}
	}
}

func (m *model) onHelp(msg helpMsg) tea.Cmd {
	if msg.err != nil {
		m.tr.AddNotice("error", "help: "+msg.err.Error())
		return m.commit()
	}
	groups := map[string][]CompletionItem{}
	var builtin []CompletionItem
	for _, it := range msg.items {
		switch it.Kind {
		case "command":
			groups["custom"] = append(groups["custom"], it)
		case "skill":
			groups["skills"] = append(groups["skills"], it)
		case "subagent":
			it.Hint = "subagent · " + it.Hint
			groups["skills"] = append(groups["skills"], it)
		case "prompt":
			groups["MCP prompts"] = append(groups["MCP prompts"], it)
		default:
			builtin = append(builtin, it)
		}
	}
	builtin = append(builtin, m.localCommands("/")...)
	filtered := builtin[:0]
	for _, item := range builtin {
		if item.Label != "/paste-image" {
			filtered = append(filtered, item)
		}
	}
	builtin = filtered
	return m.emit(func(width int, _ bool) string {
		var b strings.Builder
		b.WriteString(termrender.Accent("commands") + "\n")
		writeHelpGroup(&b, width, "built-in", builtin, 0)
		for _, title := range []string{"custom", "skills", "MCP prompts"} {
			writeHelpGroup(&b, width, title, groups[title], helpMaxDynamicItems)
		}
		b.WriteString(termrender.Dim("  type a command, or press Tab after / for completion"))
		return b.String()
	})
}

func writeHelpGroup(b *strings.Builder, width int, title string, items []CompletionItem, limit int) {
	if len(items) == 0 {
		return
	}
	b.WriteString(termrender.Dim("  "+title) + "\n")
	n := len(items)
	if limit > 0 && n > limit {
		n = limit
	}
	for _, it := range items[:n] {
		used := 2 + max(termrender.VisibleWidth(it.Label), 18) + 1
		hint := strings.Join(strings.Fields(it.Hint), " ")
		hint = termrender.Truncate(hint, max(width-used, 1), "…")
		fmt.Fprintf(b, "  %-18s %s\n", it.Label, termrender.Dim(hint))
	}
	if extra := len(items) - n; extra > 0 {
		b.WriteString(termrender.Dim(fmt.Sprintf("  +%d more items", extra)) + "\n")
	}
}
