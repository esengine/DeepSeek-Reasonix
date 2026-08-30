package serve

import (
	"net/http"

	"reasonix/internal/control"
	"reasonix/internal/i18n"
	"reasonix/internal/skill"
)

type commandCatalogEntry struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Hint        string `json:"hint,omitempty"`
	Kind        string `json:"kind"`
	Group       string `json:"group,omitempty"`
	Plugin      string `json:"plugin,omitempty"`
	Color       string `json:"color,omitempty"`
}

// commands publishes the active Serve controller's slash catalog. Remote
// desktop composers must not read commands from whichever local tab happens
// to be active on the client machine.
func (s *Server) commands(w http.ResponseWriter, _ *http.Request) {
	ctrl := s.ctl()
	commands, skills := ctrl.Commands(), ctrl.SlashSkills()
	out := []commandCatalogEntry{
		{Name: "new", Description: i18n.M.CmdNew, Kind: "builtin", Group: "actions"},
		{Name: "clear", Description: i18n.M.CmdClear, Kind: "builtin", Group: "actions"},
		{Name: "compact", Description: i18n.M.CmdCompact, Kind: "builtin", Group: "actions"},
		{Name: "model", Description: i18n.M.CmdModel, Kind: "builtin", Group: "actions"},
		{Name: "provider", Description: i18n.M.CmdProvider, Kind: "builtin", Group: "management"},
		{Name: "effort", Description: i18n.M.CmdEffort, Kind: "builtin", Group: "actions"},
		{Name: "memory", Description: i18n.M.CmdMemory, Kind: "builtin", Group: "management"},
		{Name: "migrate", Description: i18n.M.CmdMigrate, Kind: "builtin", Group: "management"},
		{Name: "goal", Description: i18n.M.CmdGoal, Kind: "builtin", Group: "actions"},
		{Name: "remember", Description: i18n.M.CmdRemember, Kind: "builtin", Group: "management"},
		{Name: "mcp", Description: i18n.M.CmdMcp, Kind: "builtin", Group: "integrations"},
		{Name: "hooks", Description: i18n.M.CmdHooks, Kind: "builtin", Group: "management"},
		{Name: "plugins", Description: i18n.M.CmdPlugins, Kind: "builtin", Group: "integrations"},
		{Name: "skill", Description: i18n.M.CmdSkill, Kind: "builtin", Group: "skills"},
		{Name: "reload-cmd", Description: i18n.M.CmdReloadCmd, Kind: "builtin", Group: "management"},
		{Name: control.ResolvedBuiltinSlashName(control.DocsSlashName, commands, skills), Description: i18n.M.CmdDocs, Hint: "<question>", Kind: "builtin", Group: "integrations"},
	}
	for _, sk := range skills {
		kind, group := "skill", "skills"
		if sk.RunAs == skill.RunSubagent {
			kind, group = "subagent", "subagents"
		}
		out = append(out, commandCatalogEntry{Name: sk.SlashName(), Description: sk.Description, Kind: kind, Group: group, Plugin: sk.Plugin, Color: sk.Color})
	}
	for _, command := range commands {
		if !command.Hidden {
			out = append(out, commandCatalogEntry{Name: command.Name, Description: command.Description, Hint: command.ArgHint, Kind: "custom", Group: "skills", Plugin: command.Plugin})
		}
	}
	if host := ctrl.Host(); host != nil {
		for _, prompt := range host.Prompts() {
			out = append(out, commandCatalogEntry{Name: prompt.Name, Description: prompt.Description, Kind: "mcp", Group: "integrations"})
		}
	}
	writeJSON(w, out)
}
