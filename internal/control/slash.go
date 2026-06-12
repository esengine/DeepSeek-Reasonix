package control

import (
	"fmt"
	"os"
	"strings"

	"reasonix/internal/config"
	"reasonix/internal/hook"
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
	DisabledSkills  []skill.Skill
	ServerNames     []string
	ConfiguredMCP   []string
	DisconnectedMCP []string
	ModelRefs       []string
	CurrentModel    string
	ProviderNames   []string
	CurrentProvider string
}

// SlashArgItems completes the arguments of a management slash command
// (everything after the command word). It returns the suggestions filtered by
// the token being typed and the byte offset where that token begins, so a caller
// replaces just that token. Only structured commands participate (/mcp /model
// /skills /hooks /effort /auto-plan /theme /language); others yield nil. Single
// source of truth for CLI + desktop.
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
	case "/provider":
		raw = providerArgItems(prior, d)
	case "/skill", "/skills":
		raw = skillArgItems(prior, d)
	case "/hooks":
		raw = hooksArgItems(prior)
	case "/effort":
		raw = effortArgItems(prior, d)
	case "/auto-plan":
		raw = autoPlanArgItems(prior)
	case "/theme":
		raw = themeArgItems(prior)
	case "/language":
		raw = languageArgItems(prior)
	default:
		return nil, from
	}
	return filterSlash(raw, line, from, cur), from
}

func autoPlanArgItems(prior []string) []SlashItem {
	if len(prior) > 1 {
		return nil
	}
	return []SlashItem{
		{Label: "off", Insert: "off", Hint: "manual plan mode only"},
		{Label: "on", Insert: "on", Hint: "auto-enter plan mode for complex tasks"},
	}
}

func languageArgItems(prior []string) []SlashItem {
	if len(prior) > 1 {
		return nil
	}
	return []SlashItem{
		{Label: "auto", Insert: "auto", Hint: i18n.M.ArgLanguageAuto},
		{Label: "en", Insert: "en", Hint: i18n.M.ArgLanguageEn},
		{Label: "zh", Insert: "zh", Hint: i18n.M.ArgLanguageZh},
	}
}

func themeArgItems(prior []string) []SlashItem {
	if len(prior) > 1 {
		return nil
	}
	items := []SlashItem{
		{Label: "auto", Insert: "auto", Hint: "mode · detect system or terminal background"},
		{Label: "light", Insert: "light", Hint: "mode · force light shell"},
		{Label: "dark", Insert: "dark", Hint: "mode · force dark shell"},
	}
	for _, st := range []struct {
		name string
		mode string
		desc string
	}{
		{"graphite", "dark", "warm clay accent"},
		{"ember", "dark", "hot orange accent"},
		{"aurora", "dark", "cool teal accent"},
		{"midnight", "dark", "quiet violet accent"},
		{"sandstone", "light", "default warm light accent"},
		{"porcelain", "light", "soft violet light accent"},
		{"linen", "light", "muted coral light accent"},
		{"glacier", "light", "cool blue accent"},
	} {
		items = append(items, SlashItem{Label: st.name, Insert: st.name, Hint: st.mode + " · " + st.desc})
	}
	return items
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
			{Label: "show", Insert: "show ", Hint: "show MCP server details", Descend: true},
			{Label: "tools", Insert: "tools ", Hint: "show MCP server tools", Descend: true},
			{Label: "remove", Insert: "remove ", Hint: i18n.M.ArgMcpRemove, Descend: true},
			{Label: "import", Insert: "import", Hint: "import Codex-enabled servers from cc-switch"},
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
	case "show", "tools":
		if len(prior) != 2 {
			return nil
		}
		var items []SlashItem
		for _, name := range allMCPArgNames(d) {
			items = append(items, SlashItem{Label: name, Insert: name})
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

func allMCPArgNames(d ArgData) []string {
	seen := map[string]bool{}
	var out []string
	for _, list := range [][]string{d.ServerNames, d.ConfiguredMCP, d.DisconnectedMCP} {
		for _, name := range list {
			if strings.TrimSpace(name) == "" || seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, name)
		}
	}
	return out
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

func providerArgItems(prior []string, d ArgData) []SlashItem {
	if len(prior) != 1 { // the single name arg is already placed
		return nil
	}
	var items []SlashItem
	for _, name := range d.ProviderNames {
		hint := ""
		if name == d.CurrentProvider {
			hint = i18n.M.ArgModelCurrent
		}
		items = append(items, SlashItem{Label: name, Insert: name, Hint: hint})
	}
	return items
}

func skillArgItems(prior []string, d ArgData) []SlashItem {
	if len(prior) <= 1 {
		return []SlashItem{
			{Label: "show", Insert: "show ", Hint: i18n.M.ArgSkillShow, Descend: true},
			{Label: "enable", Insert: "enable ", Hint: "enable a disabled skill", Descend: true},
			{Label: "disable", Insert: "disable ", Hint: "disable an enabled skill", Descend: true},
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
	if prior[1] == "disable" && len(prior) == 2 {
		var items []SlashItem
		for _, s := range d.Skills {
			items = append(items, SlashItem{Label: s.Name, Insert: s.Name, Hint: string(s.Scope)})
		}
		return items
	}
	if prior[1] == "enable" && len(prior) == 2 {
		var items []SlashItem
		for _, s := range d.DisabledSkills {
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
// the line because the token is already fully typed (e.g. "/skills list" offering
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
	case "/provider":
		if len(fields) >= 2 {
			c.notice(c.providerSwitchText(fields[1]))
		} else {
			c.notice(c.providerListText())
		}
	case "/memory":
		c.notice(c.memoryListText())
	case "/skill", "/skills":
		sub := ""
		if len(fields) >= 2 {
			sub = strings.ToLower(fields[1])
		}
		if len(fields) >= 3 && (sub == "enable" || sub == "disable") {
			enabled := sub == "enable"
			if err := c.SetSkillEnabled(fields[2], enabled); err != nil {
				c.notice("skill " + sub + ": " + err.Error())
			} else if enabled {
				c.notice("enabled skill " + fields[2] + " — restart or refresh the session for the prompt and tools to update")
			} else {
				c.notice("disabled skill " + fields[2] + " — restart or refresh the session for the prompt and tools to update")
			}
			return true
		}
		c.notice(c.skillListText())
	case "/hooks":
		sub := ""
		if len(fields) >= 2 {
			sub = strings.ToLower(fields[1])
		}
		switch sub {
		case "", "list", "ls":
			c.notice(c.hookListText())
		case "trust":
			root := c.cpRoot
			if root == "" {
				root, _ = os.Getwd()
			}
			if err := hook.Trust(root, ""); err != nil {
				c.notice("hooks trust: " + err.Error())
			} else {
				c.notice("trusted this project's hooks — they load on the next /new or restart")
			}
		default:
			c.notice("unknown /hooks subcommand " + fields[1] + " — try: /hooks, /hooks trust")
		}
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
		for _, m := range p.ChatModelList() {
			fmt.Fprintf(&b, "  %s/%s\n", p.Name, m)
		}
	}
	b.WriteString(i18n.M.ListModelsHint)
	return strings.TrimRight(b.String(), "\n")
}

func (c *Controller) providerListText() string {
	cfg, err := config.Load()
	if err != nil {
		return "provider: " + err.Error()
	}
	curProvider := ""
	if parts := strings.Fields(c.label); len(parts) > 0 {
		curProvider = parts[0]
	}
	var b strings.Builder
	b.WriteString(i18n.M.ProviderListHeader + "\n")
	for i := range cfg.Providers {
		p := &cfg.Providers[i]
		if !p.Configured() {
			continue
		}
		models := p.ChatModelList()
		if len(models) == 0 {
			models = p.ModelList()
		}
		suffix := ""
		if p.Name == curProvider {
			suffix = " (active)"
		}
		fmt.Fprintf(&b, "  %s — %d models%s\n", p.Name, len(models), suffix)
	}
	b.WriteString("switch with /provider <name>")
	return strings.TrimRight(b.String(), "\n")
}

func (c *Controller) providerSwitchText(name string) string {
	cfg, err := config.Load()
	if err != nil {
		return "provider: " + err.Error()
	}
	for i := range cfg.Providers {
		p := &cfg.Providers[i]
		if p.Name == name && p.Configured() {
			models := p.ChatModelList()
			if len(models) == 0 {
				models = p.ModelList()
			}
			if len(models) == 0 {
				return fmt.Sprintf(i18n.M.ProviderNoModelsFmt, name)
			}
			if len(models) == 1 {
				return fmt.Sprintf("provider %s — model: %s (switch with /model %s/%s)", name, models[0], name, models[0])
			}
			var b strings.Builder
			fmt.Fprintf(&b, "provider %s — %d models:\n", name, len(models))
			for _, m := range models {
				fmt.Fprintf(&b, "  %s/%s\n", name, m)
			}
			fmt.Fprintf(&b, "switch with /model %s/<model>", name)
			return strings.TrimRight(b.String(), "\n")
		}
	}
	return fmt.Sprintf(i18n.M.ProviderUnknownFmt, name)
}

func (c *Controller) memoryListText() string {
	if c.mem == nil {
		return i18n.M.ListMemoryNone
	}
	saved := c.mem.Store.List()
	archived := c.mem.Store.ListArchived()
	if len(c.mem.Docs) == 0 && len(saved) == 0 && len(archived) == 0 {
		return i18n.M.ListMemoryNone
	}
	var b strings.Builder
	if len(c.mem.Docs) > 0 {
		b.WriteString(i18n.M.ListMemoryHeader + "\n")
		for _, d := range c.mem.Docs {
			fmt.Fprintf(&b, "  (%s) %s\n", d.Scope, d.Path)
		}
	}
	if len(saved) > 0 {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(i18n.M.ListMemorySaved + "\n")
		for _, m := range saved {
			fmt.Fprintf(&b, "  [%s](%s.md) (%s) %s\n", memoryDisplayTitle(m.Title, m.Name), m.Name, m.Type, memoryOneLine(m.Description))
		}
	}
	if len(archived) > 0 {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(i18n.M.ListMemoryArchived + "\n")
		for _, m := range archived {
			when := ""
			if !m.ArchivedAt.IsZero() {
				when = " — " + m.ArchivedAt.Format("2006-01-02 15:04:05Z")
			}
			fmt.Fprintf(&b, "  [%s](%s) (%s)%s %s\n", memoryDisplayTitle(m.Title, m.Name), m.Path, m.Type, when, memoryOneLine(m.Description))
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func memoryDisplayTitle(title, name string) string {
	if t := memoryOneLine(title); t != "" {
		return t
	}
	return strings.ReplaceAll(name, "-", " ")
}

func memoryOneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
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
