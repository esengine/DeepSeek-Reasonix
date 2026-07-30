package cli

import (
	"fmt"
	"strings"

	"reasonix/internal/command"
	"reasonix/internal/plugin"
	"reasonix/internal/skill"
)

const helpMaxDynamicItems = 8

func (m *chatTUI) showHelp() {
	m.commitLine(renderHelp(m.width, m.commands, m.skills, m.prompts()))
}

func renderHelp(width int, commands []command.Command, skills []skill.Skill, prompts []plugin.Prompt) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", viewHeader("commands"))
	writeHelpItems(&b, width, "built-in", builtinHelpItems(), 0)
	if len(commands) > 0 {
		writeHelpItems(&b, width, "custom", customHelpItems(commands), helpMaxDynamicItems)
	}
	if len(skills) > 0 {
		writeHelpItems(&b, width, "skills", skillHelpItems(skills), helpMaxDynamicItems)
	}
	if len(prompts) > 0 {
		writeHelpItems(&b, width, "MCP prompts", promptHelpItems(prompts), helpMaxDynamicItems)
	}
	b.WriteString(viewHint("type a command, or press Tab after / for completion"))
	return strings.TrimRight(b.String(), "\n")
}

func writeHelpItems(b *strings.Builder, width int, title string, items []compItem, limit int) {
	if len(items) == 0 {
		return
	}
	b.WriteString(viewSubhead(title) + "\n")
	n := len(items)
	if limit > 0 && n > limit {
		n = limit
	}
	for _, item := range items[:n] {
		name := item.label
		used := 2 + viewPadWidth(name, 18) + 1
		hint := viewCompactText(item.hint, viewBudget(width, used))
		fmt.Fprintf(b, "  %-18s %s\n", name, viewMeta(hint))
	}
	if extra := len(items) - n; extra > 0 {
		b.WriteString(viewMore(extra, "items") + "\n")
	}
}

func builtinHelpItems() []compItem {
	return builtinSlashHelpItems()
}

func customHelpItems(commands []command.Command) []compItem {
	items := make([]compItem, 0, len(commands))
	for _, c := range commands {
		if c.Hidden {
			continue
		}
		items = append(items, compItem{label: "/" + c.Name, hint: customCommandHint(c)})
	}
	return items
}

func customCommandHint(c command.Command) string {
	if c.Plugin == "" {
		return c.Description
	}
	source := "plugin " + c.Plugin
	if c.Description == "" {
		return source
	}
	return source + " · " + c.Description
}

func skillHelpItems(skills []skill.Skill) []compItem {
	items := make([]compItem, 0, len(skills))
	for _, s := range skills {
		hint := s.Description
		if s.RunAs == skill.RunSubagent {
			hint = "subagent · " + hint
		}
		items = append(items, compItem{label: "/" + s.SlashName(), hint: skillCommandHint(s, hint)})
	}
	return items
}

func skillCommandHint(s skill.Skill, hint string) string {
	if s.Plugin == "" {
		return hint
	}
	source := "plugin " + s.Plugin
	if hint == "" {
		return source
	}
	return source + " · " + hint
}

func promptHelpItems(prompts []plugin.Prompt) []compItem {
	items := make([]compItem, 0, len(prompts))
	for _, p := range prompts {
		items = append(items, compItem{label: "/" + p.Name, hint: p.Description})
	}
	return items
}
