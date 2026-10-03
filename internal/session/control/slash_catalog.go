package control

import (
	"strings"

	"reasonix/internal/base/i18n"
	"reasonix/internal/contract/config"
	"reasonix/internal/ext/command"
	"reasonix/internal/ext/pluginpkg"
	"reasonix/internal/ext/skill"
)

// SubmitSlashCommands is the built-in surface a frontend gets for free by
// routing raw input through Submit: the verbs submitCommandOrTurnReady and
// managementNotice dispatch, plus the two serve intercepts. The chat TUI's
// catalogue is larger because it implements commands of its own — listing
// those here would offer a window a command nothing answers.
func SubmitSlashCommands(m i18n.Messages) []SlashItem {
	return []SlashItem{
		{Label: "/compact", Insert: "/compact ", Hint: m.CmdCompact},
		{Label: "/context", Insert: "/context", Hint: m.CmdContext},
		{Label: "/new", Insert: "/new", Hint: m.CmdNew},
		{Label: "/clear", Insert: "/clear", Hint: m.CmdClear},
		{Label: "/rewind", Insert: "/rewind", Hint: m.CmdRewind},
		{Label: "/tree", Insert: "/tree", Hint: m.CmdTree},
		{Label: "/branch", Insert: "/branch ", Hint: m.CmdBranch},
		{Label: "/switch", Insert: "/switch ", Hint: m.CmdSwitchBranch},
		{Label: "/model", Insert: "/model ", Hint: m.CmdModel, Descend: true},
		{Label: "/provider", Insert: "/provider ", Hint: m.CmdProvider, Descend: true},
		{Label: "/effort", Insert: "/effort ", Hint: m.CmdEffort, Descend: true},
		{Label: "/goal", Insert: "/goal ", Hint: m.CmdGoal, Descend: true},
		{Label: "/memory", Insert: "/memory ", Hint: m.CmdMemory, Descend: true},
		{Label: "/remember", Insert: "/remember ", Hint: m.CmdRemember},
		{Label: "/skills", Insert: "/skills ", Hint: m.CmdSkill, Descend: true},
		{Label: "/plugins", Insert: "/plugins ", Hint: m.CmdPlugins, Descend: true},
		{Label: "/hooks", Insert: "/hooks ", Hint: m.CmdHooks, Descend: true},
		{Label: "/feedback", Insert: "/feedback ", Hint: m.CmdFeedback, Descend: true},
		{Label: "/mcp", Insert: "/mcp ", Hint: m.CmdMcp, Descend: true},
		{Label: "/remote", Insert: "/remote", Hint: m.CmdRemote},
		{Label: "/docs", Insert: "/docs ", Hint: m.CmdDocs},
		{Label: "/migrate", Insert: "/migrate", Hint: m.CmdMigrate},
		{Label: "/reload-cmd", Insert: "/reload-cmd", Hint: m.CmdReloadCmd},
		{Label: "/reload", Insert: "/reload", Hint: m.CmdReload},
		{Label: "/rename", Insert: "/rename ", Hint: m.CmdRename},
		{Label: "/forget", Insert: "/forget ", Hint: m.CmdForget},
		{Label: "/sandbox", Insert: "/sandbox", Hint: m.CmdSandbox},
		{Label: "/output-style", Insert: "/output-style", Hint: m.CmdOutputStyle},
		{Label: "/reasoning-language", Insert: "/reasoning-language ", Hint: m.CmdReasonLang, Descend: true},
		{Label: "/language", Insert: "/language ", Hint: m.CmdLanguage, Descend: true},
		{Label: "/currency", Insert: "/currency ", Hint: m.CmdCurrency, Descend: true},
	}
}

// CompletionData assembles what the composer's menu needs from the live
// session. Frontends read it from the controller rather than gathering it
// themselves, so the same session answers every one of them identically.
func (c *Controller) CompletionData(lang string) CompletionData {
	m := i18n.M
	if lang != "" {
		m = i18n.Catalog(lang)
	}
	commands := c.Commands()
	skills := c.SlashSkills()
	d := CompletionData{
		ArgData: ArgData{
			Skills:          skills,
			DisabledSkills:  c.DisabledSkills(),
			ConfiguredMCP:   c.ConfiguredMCPNames(),
			DisconnectedMCP: c.DisconnectedMCPNames(),
			ModelRefs:       configuredModelRefs(),
			CurrentModel:    c.ModelRef(),
			ProviderNames:   configuredProviderNames(),
			PluginNames:     installedPluginNames(),
		},
		Names:         c.completionNames(commands, skills, m),
		WorkspaceRoot: c.WorkspaceRoot(),
	}
	d.CurrentProvider, _, _ = strings.Cut(d.CurrentModel, "/")
	d.MemoryRefs, d.MemoryArchives = MemoryCompletionData(c.Memory())
	if h := c.Host(); h != nil {
		d.ServerNames = h.ServerNames()
		d.Resources = h.Resources()
	}
	return d
}

// completionNames orders the catalogue the way Submit resolves it: a custom
// command beats a skill of the same name, and both beat nothing — so the menu's
// answer and the kernel's are the same answer.
func (c *Controller) completionNames(commands []command.Command, skills []skill.Skill, m i18n.Messages) []SlashItem {
	docs := "/" + ResolvedBuiltinSlashName(DocsSlashName, commands, skills)
	builtin := SubmitSlashCommands(m)
	items := make([]SlashItem, 0, len(commands)+len(skills)+len(builtin))
	for _, b := range builtin {
		if b.Label == "/docs" {
			b.Insert = docs + strings.TrimPrefix(b.Insert, b.Label)
			b.Label = docs
		}
		b.Kind = "builtin"
		items = append(items, b)
	}
	seen := map[string]bool{}
	for _, cmd := range commands {
		if cmd.Hidden || seen[cmd.Name] {
			continue
		}
		seen[cmd.Name] = true
		items = append(items, SlashItem{
			Label: "/" + cmd.Name, Insert: "/" + cmd.Name + " ",
			Hint: sourcedHint(cmd.Plugin, cmd.Description), Kind: "command",
		})
	}
	docsOwner := ResolveSlashCommandOwner(DocsSlashName, commands, skills)
	for _, sk := range skills {
		name := sk.SlashName()
		if name == "" || seen[name] || (docsOwner == SlashOwnerCustom && name == DocsSlashName) {
			continue
		}
		seen[name] = true
		kind := "skill"
		if sk.RunAs == skill.RunSubagent {
			kind = "subagent"
		}
		items = append(items, SlashItem{
			Label: "/" + name, Insert: "/" + name + " ",
			Hint: sourcedHint(sk.Plugin, sk.Description), Kind: kind,
		})
	}
	if h := c.Host(); h != nil {
		for _, p := range h.Prompts() {
			if seen[p.Name] {
				continue
			}
			seen[p.Name] = true
			items = append(items, SlashItem{
				Label: "/" + p.Name, Insert: "/" + p.Name + " ",
				Hint: p.Description, Kind: "prompt",
			})
		}
	}
	return items
}

// Where a command came from matters as much as what it does once plugins can
// install them: two rows named the same differ only by their source.
func sourcedHint(plugin, description string) string {
	if plugin == "" {
		return description
	}
	if description == "" {
		return "plugin " + plugin
	}
	return "plugin " + plugin + " · " + description
}

func configuredModelRefs() []string {
	cfg, err := config.Load()
	if err != nil {
		return nil
	}
	var out []string
	for i := range cfg.Providers {
		p := &cfg.Providers[i]
		if !p.Configured() {
			continue
		}
		for _, model := range p.ChatModelList() {
			out = append(out, p.Name+"/"+model)
		}
	}
	return out
}

func configuredProviderNames() []string {
	cfg, err := config.Load()
	if err != nil {
		return nil
	}
	var out []string
	for i := range cfg.Providers {
		p := &cfg.Providers[i]
		if p.Configured() {
			out = append(out, p.Name)
		}
	}
	return out
}

func installedPluginNames() []string {
	names, err := pluginpkg.InstalledNames(config.ReasonixHomeDir())
	if err != nil {
		return nil
	}
	return names
}
