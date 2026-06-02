package control

import (
	"fmt"
	"strings"

	"reasonix/internal/config"
	"reasonix/internal/i18n"
	"reasonix/internal/skill"
)

// SlashItem is one slash-completion suggestion. Insert is the token text placed
// at the current argument position (callers replace from the token's start, see
// SlashArgItems' returned offset); Descend hints the menu to re-open one level
// deeper after accepting (e.g. "/mcp " → "/mcp add ").
type SlashItem struct {
	Label   string `json:"label"`
	Insert  string `json:"insert"`
	Hint    string `json:"hint"`
	Descend bool   `json:"descend"`
}

// ArgData supplies the dynamic data SlashArgItems needs, so the completion logic
// is one shared function both frontends call with their own session data — the
// chat TUI (controller-free, from its cached lists) and the desktop (from the
// controller). This keeps the CLI and desktop sub-command hints identical.
type ArgData struct {
	Skills          []skill.Skill
	ServerNames     []string
	ConfiguredMCP   []string
	DisconnectedMCP []string
	ModelRefs       []string
	CurrentModel    string
}

// SlashArgItems completes the arguments of a management slash command
// (everything after the command word). It returns the suggestions filtered by
// the token being typed and the byte offset where that token begins, so a caller
// replaces just that token. Only structured commands participate (/mcp /model
// /skill /hooks /effort); others yield nil. Single source of truth for CLI +
// desktop.
func SlashArgItems(line string, d ArgData) ([]SlashItem, int) {
	cmdEnd := strings.IndexAny(line, " \t")
	if cmdEnd < 0 {
		return nil, 0
	}
	from := strings.LastIndexAny(line, " \t") + 1
	cur := line[from:]
	prior := strings.Fields(line[:from]) // committed tokens, including the command word
	var raw []SlashItem
	switch line[:cmdEnd] {
	case "/mcp":
		raw = mcpArgItems(prior, cur, d)
	case "/model":
		raw = modelArgItems(prior, d)
	case "/skill", "/skills":
		raw = skillArgItems(prior, d)
	case "/hooks":
		raw = hooksArgItems(prior)
	case "/effort":
		raw = effortArgItems(prior, d)
	default:
		return nil, from
	}
	return filterSlash(raw, line, from, cur), from
}

func effortArgItems(prior []string, d ArgData) []SlashItem {
	if len(prior) <= 1 {
		entry := currentEffortEntry(d)
		cap := config.EffortCapabilityForEntry(entry)
		var out []SlashItem
		for _, level := range cap.Levels {
			hint := ""
			switch level {
			case "auto":
				hint = i18n.M.ArgEffortAuto
			case "low":
				hint = i18n.M.ArgEffortLow
			case "medium":
				hint = i18n.M.ArgEffortMedium
			case "high":
				hint = i18n.M.ArgEffortHigh
			case "xhigh":
				hint = i18n.M.ArgEffortXHigh
			case "max":
				hint = i18n.M.ArgEffortMax
			}
			out = append(out, SlashItem{Label: level, Insert: level, Hint: hint})
		}
		return out
	}
	return nil
}

func currentEffortEntry(d ArgData) *config.ProviderEntry {
	if strings.TrimSpace(d.CurrentModel) == "" {
		return nil
	}
	cfg, err := config.Load()
	if err != nil {
		return nil
	}
	entry, _ := cfg.ResolveModel(d.CurrentModel)
	return entry
}

func mcpArgItems(prior []string, cur string, d ArgData) []SlashItem {
	if len(prior) <= 1 {
		return []SlashItem{
			{Label: "add", Insert: "add ", Hint: i18n.M.ArgMcpAdd, Descend: true},
			{Label: "connect", Insert: "connect ", Hint: "connect a configured MCP server", Descend: true},
			{Label: "remove", Insert: "remove ", Hint: i18n.M.ArgMcpRemove, Descend: true},
			{Label: "list", Insert: "list", Hint: i18n.M.ArgMcpList},
		}
	}
	switch prior[1] {
	case "remove", "rm":
		if len(prior) != 2 { // the single name arg is already placed
			return nil
		}
		var items []SlashItem
		for _, name := range d.ServerNames {
			items = append(items, SlashItem{Label: name, Insert: name, Hint: i18n.M.ArgMcpConnected})
		}
		return items
	case "connect":
		if len(prior) != 2 {
			return nil
		}
		var items []SlashItem
		for _, name := range d.DisconnectedMCP {
			items = append(items, SlashItem{Label: name, Insert: name, Hint: "configured"})
		}
		return items
	case "add":
		if strings.HasPrefix(cur, "-") {
			return []SlashItem{
				{Label: "--http", Insert: "--http ", Hint: "Streamable HTTP URL"},
				{Label: "--sse", Insert: "--sse ", Hint: "legacy SSE URL"},
				{Label: "--env", Insert: "--env ", Hint: "KEY=VALUE (stdio)"},
				{Label: "--header", Insert: "--header ", Hint: "KEY=VALUE (remote)"},
			}
		}
	}
	return nil
}

func modelArgItems(prior []string, d ArgData) []SlashItem {
	if len(prior) != 1 { // the single ref arg is already placed
		return nil
	}
	var items []SlashItem
	for _, ref := range d.ModelRefs {
		hint := ""
		if ref == d.CurrentModel {
			hint = i18n.M.ArgModelCurrent
		}
		items = append(items, SlashItem{Label: ref, Insert: ref, Hint: hint})
	}
	return items
}

func skillArgItems(prior []string, d ArgData) []SlashItem {
	if len(prior) <= 1 {
		return []SlashItem{
			{Label: "list", Insert: "list", Hint: i18n.M.ArgSkillList},
			{Label: "show", Insert: "show ", Hint: i18n.M.ArgSkillShow, Descend: true},
			{Label: "new", Insert: "new ", Hint: i18n.M.ArgSkillNew},
			{Label: "paths", Insert: "paths", Hint: i18n.M.ArgSkillPaths},
		}
	}
	if (prior[1] == "show" || prior[1] == "cat") && len(prior) == 2 {
		var items []SlashItem
		for _, s := range d.Skills {
			items = append(items, SlashItem{Label: s.Name, Insert: s.Name, Hint: string(s.Scope)})
		}
		return items
	}
	return nil
}

func hooksArgItems(prior []string) []SlashItem {
	if len(prior) <= 1 {
		return []SlashItem{
			{Label: "list", Insert: "list", Hint: i18n.M.ArgHooksList},
			{Label: "trust", Insert: "trust", Hint: i18n.M.ArgHooksTrust},
		}
	}
	return nil
}

// filterSlash keeps items whose label starts with the typed token (case-
// insensitive) and drops no-op suggestions — ones whose insert wouldn't change
// the line because the token is already fully typed (e.g. "/skill list" offering
// "list"). Without this the menu lingers on a complete command and Enter keeps
// "accepting" the no-op instead of sending.
func filterSlash(items []SlashItem, line string, from int, cur string) []SlashItem {
	lp := strings.ToLower(cur)
	prefix := line[:from]
	var out []SlashItem
	for _, it := range items {
		if !strings.HasPrefix(strings.ToLower(it.Label), lp) {
			continue
		}
		if prefix+it.Insert == line {
			continue // token already complete: nothing to add
		}
		out = append(out, it)
	}
	return out
}

// managementNotice handles the read-only management slash commands on the Submit
// path (used by the desktop and HTTP frontends, which route raw input through
// Submit — the chat TUI has its own richer handlers). It emits a Notice listing
// and reports whether it handled the verb. Skills and custom commands are NOT
// here — those resolve to a turn in Submit.
func (c *Controller) managementNotice(trimmed string) bool {
	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return false
	}
	switch fields[0] {
	case "/model":
		c.notice(c.modelListText())
	case "/memory":
		c.notice(c.memoryListText())
	case "/skill", "/skills":
		c.notice(c.skillListText())
	case "/hooks":
		c.notice(c.hookListText())
	case "/mcp":
		if len(fields) >= 3 && fields[1] == "connect" {
			n, err := c.ConnectConfiguredMCPServer(fields[2])
			if err != nil {
				c.notice("mcp connect: " + err.Error())
			} else {
				c.notice(fmt.Sprintf("connected %s — %d tools", fields[2], n))
			}
			return true
		}
		c.notice(c.mcpListText())
	default:
		return false
	}
	return true
}

func (c *Controller) modelListText() string {
	cfg, err := config.Load()
	if err != nil {
		return "model: " + err.Error()
	}
	var b strings.Builder
	fmt.Fprintf(&b, i18n.M.ListModelsHeaderFmt+"\n", c.label)
	for i := range cfg.Providers {
		p := &cfg.Providers[i]
		if !p.Configured() {
			continue
		}
		for _, m := range p.ModelList() {
			fmt.Fprintf(&b, "  %s/%s\n", p.Name, m)
		}
	}
	b.WriteString(i18n.M.ListModelsHint)
	return strings.TrimRight(b.String(), "\n")
}

func (c *Controller) memoryListText() string {
	if c.mem == nil || len(c.mem.Docs) == 0 {
		return i18n.M.ListMemoryNone
	}
	var b strings.Builder
	b.WriteString(i18n.M.ListMemoryHeader + "\n")
	for _, d := range c.mem.Docs {
		fmt.Fprintf(&b, "  (%s) %s\n", d.Scope, d.Path)
	}
	return strings.TrimRight(b.String(), "\n")
}

func (c *Controller) skillListText() string {
	if len(c.skills) == 0 {
		return i18n.M.ListSkillsNone
	}
	var b strings.Builder
	fmt.Fprintf(&b, i18n.M.ListSkillsHeaderFmt+"\n", len(c.skills))
	for _, s := range c.skills {
		tag := ""
		if s.RunAs == "subagent" {
			tag = " 🧬"
		}
		fmt.Fprintf(&b, "  /%s%s — %s\n", s.Name, tag, s.Description)
	}
	return strings.TrimRight(b.String(), "\n")
}

func (c *Controller) hookListText() string {
	hooks := c.hooks.Hooks()
	if len(hooks) == 0 {
		return i18n.M.ListHooksNone
	}
	var b strings.Builder
	fmt.Fprintf(&b, i18n.M.ListHooksHeaderFmt+"\n", len(hooks))
	for _, h := range hooks {
		match := h.Match
		if match == "" {
			match = "*"
		}
		fmt.Fprintf(&b, "  %s [%s] %s — %s\n", h.Event, h.Scope, match, h.Command)
	}
	return strings.TrimRight(b.String(), "\n")
}

func (c *Controller) mcpListText() string {
	if c.host == nil || (len(c.host.ServerNames()) == 0 && len(c.host.Failures()) == 0) {
		return i18n.M.ListMcpNone
	}
	var b strings.Builder
	if len(c.host.ServerNames()) > 0 {
		b.WriteString(i18n.M.ListMcpHeader + "\n")
		for _, name := range c.host.ServerNames() {
			fmt.Fprintf(&b, "  %s\n", name)
		}
	}
	if failures := c.host.Failures(); len(failures) > 0 {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString("MCP startup failures:\n")
		for _, f := range failures {
			fmt.Fprintf(&b, "  %s (%s): %s\n", f.Name, f.Transport, f.Error)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
