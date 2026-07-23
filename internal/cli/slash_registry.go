package cli

import (
	"strings"

	"reasonix/internal/i18n"
)

// builtinSlashSpec is the single source of truth for a built-in command's
// discoverable surface. Execution stays in runSlashCommand, while completion,
// help, aliases, and argument descent are generated from this catalogue.
type builtinSlashSpec struct {
	name       string
	aliases    []string
	insert     string
	hint       string
	descend    bool
	showInHelp bool
}

func builtinSlashSpecs() []builtinSlashSpec {
	return []builtinSlashSpec{
		{name: "/compact", insert: "/compact ", hint: i18n.M.CmdCompact, showInHelp: true},
		{name: "/new", insert: "/new ", hint: i18n.M.CmdNew, showInHelp: true},
		{name: "/clear", insert: "/clear", hint: i18n.M.CmdClear, showInHelp: true},
		{name: "/cls", insert: "/cls", hint: i18n.M.CmdCls, showInHelp: true},
		{name: "/resume", insert: "/resume ", hint: i18n.M.CmdResume, showInHelp: true},
		{name: "/rename", insert: "/rename ", hint: i18n.M.CmdRename, showInHelp: true},
		{name: "/rewind", insert: "/rewind", hint: i18n.M.CmdRewind, showInHelp: true},
		{name: "/tree", insert: "/tree", hint: i18n.M.CmdTree, showInHelp: true},
		{name: "/branch", insert: "/branch ", hint: i18n.M.CmdBranch, showInHelp: true},
		{name: "/switch", insert: "/switch ", hint: i18n.M.CmdSwitchBranch, showInHelp: true},
		{name: "/todo", insert: "/todo", hint: i18n.M.CmdTodo, showInHelp: true},
		{name: "/mcp", insert: "/mcp", hint: i18n.M.CmdMcp, showInHelp: true},
		{name: "/remote", insert: "/remote", hint: i18n.M.CmdRemote, showInHelp: true},
		{name: "/plugins", aliases: []string{"/plugin"}, insert: "/plugins", hint: i18n.M.CmdPlugins, showInHelp: true},
		{name: "/model", insert: "/model", hint: i18n.M.CmdModel, descend: true, showInHelp: true},
		{name: "/status", insert: "/status", hint: i18n.M.CmdStatus, showInHelp: true},
		{name: "/work-mode", aliases: []string{"/profile"}, insert: "/work-mode ", hint: i18n.M.CmdWorkMode, descend: true, showInHelp: true},
		{name: "/provider", insert: "/provider", hint: i18n.M.CmdProvider, descend: true, showInHelp: true},
		{name: "/skills", aliases: []string{"/skill"}, insert: "/skills", hint: i18n.M.CmdSkill, showInHelp: true},
		{name: "/reload-cmd", insert: "/reload-cmd", hint: i18n.M.CmdReloadCmd, showInHelp: true},
		{name: "/hooks", insert: "/hooks ", hint: i18n.M.CmdHooks, descend: true, showInHelp: true},
		{name: "/paste-image", insert: "/paste-image", hint: i18n.M.CmdPasteImage},
		{name: "/output-style", aliases: []string{"/output-styles"}, insert: "/output-style", hint: i18n.M.CmdOutputStyle, showInHelp: true},
		{name: "/verbose", insert: "/verbose", hint: i18n.M.CmdVerbose, showInHelp: true},
		{name: "/mouse", insert: "/mouse", hint: i18n.M.CmdMouse, showInHelp: true},
		{name: "/diff-fold", insert: "/diff-fold", hint: i18n.M.CmdDiffFold, showInHelp: true},
		{name: "/sandbox", insert: "/sandbox", hint: i18n.M.CmdSandbox, showInHelp: true},
		{name: "/effort", insert: "/effort ", hint: i18n.M.CmdEffort, descend: true},
		{name: "/reasoning-language", insert: "/reasoning-language ", hint: i18n.M.CmdReasonLang, descend: true, showInHelp: true},
		{name: "/theme", insert: "/theme ", hint: i18n.M.CmdTheme, descend: true},
		{name: "/language", insert: "/language ", hint: i18n.M.CmdLanguage, descend: true, showInHelp: true},
		{name: "/help", insert: "/help", hint: i18n.M.CmdHelp, showInHelp: true},
		{name: "/memory", insert: "/memory ", hint: i18n.M.CmdMemory, showInHelp: true},
		{name: "/migrate", aliases: []string{"/migration"}, insert: "/migrate", hint: i18n.M.CmdMigrate, showInHelp: true},
		{name: "/goal", insert: "/goal ", hint: i18n.M.CmdGoal, descend: true},
		{name: "/remember", insert: "/remember ", hint: i18n.M.CmdRemember},
		{name: "/forget", insert: "/forget ", hint: i18n.M.CmdForget},
		{name: "/quit", aliases: []string{"/exit"}, insert: "/quit", hint: i18n.M.CmdQuit},
		{name: "/copy", insert: "/copy", hint: i18n.M.CmdCopy, showInHelp: true},
		{name: "/export", insert: "/export", hint: i18n.M.CmdExport, showInHelp: true},
	}
}

func builtinSlashItems() []compItem {
	specs := builtinSlashSpecs()
	items := make([]compItem, 0, len(specs))
	for _, spec := range specs {
		items = append(items, compItem{
			label: spec.name, insert: spec.insert, hint: spec.hint, descend: spec.descend,
		})
	}
	return items
}

func builtinSlashHelpItems() []compItem {
	var items []compItem
	for _, spec := range builtinSlashSpecs() {
		if spec.showInHelp {
			items = append(items, compItem{label: spec.name, hint: spec.hint})
		}
	}
	return items
}

func canonicalBuiltinSlashCommand(name string) string {
	name = strings.TrimSpace(name)
	for _, spec := range builtinSlashSpecs() {
		if name == spec.name {
			return spec.name
		}
		for _, alias := range spec.aliases {
			if name == alias {
				return spec.name
			}
		}
	}
	return name
}
