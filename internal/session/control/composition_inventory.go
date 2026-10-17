package control

import (
	"fmt"
	"sort"

	"reasonix/internal/contract/agentcomposition"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/tool"
	"reasonix/internal/ext/command"
	"reasonix/internal/ext/hook"
	"reasonix/internal/ext/pluginpkg"
	"reasonix/internal/ext/skill"
)

func (c *Controller) compositionInventory() (map[string][]agentcomposition.Module, error) {
	s := c.PresetStore()
	cfg, err := config.RootsForHome(s.Home).LoadForRootReadOnly(s.Workspace)
	if err != nil {
		return nil, err
	}
	out := map[string][]agentcomposition.Module{}
	add := func(kind, id string) { out[kind] = append(out[kind], agentcomposition.Module{ID: id}) }
	for _, kind := range []string{"tools", "skills", "mcp", "extensions", "hooks", "commands"} {
		out[kind] = []agentcomposition.Module{}
	}
	for _, t := range tool.Builtins() {
		add("tools", t.Name())
	}
	for _, name := range []string{"task", "run_skill", "read_only_skill", "use_capability"} {
		add("tools", name)
	}
	store := skill.New(skill.Options{ProjectRoot: s.Workspace, ReasonixHomeDir: s.Home, CustomPaths: cfg.SkillCustomPaths(), PluginPaths: cfg.PluginPackageSkillOwners(), PluginAgentPaths: cfg.PluginPackageAgentOwners(), ExcludedPaths: cfg.SkillExcludedPaths(), SuppressWarnings: true})
	for _, sk := range store.CompositionSkills() {
		add("skills", sk.SlashName())
	}
	for _, m := range cfg.EnabledPlugins(s.Workspace, config.DefaultActivationStore()) {
		add("mcp", m.Name)
	}
	installed, _ := pluginpkg.LoadInstalled(s.Home)
	for _, p := range installed {
		add("extensions", p.Installed.Name)
	}
	cmds, err := command.LoadRoots(cfg.Roots().CommandRootsForRoot(s.Workspace)...)
	if err != nil {
		return nil, err
	}
	for _, cmd := range cmds {
		add("commands", cmd.Name)
	}
	for i, h := range hook.Load(hook.LoadOptions{ProjectRoot: s.Workspace, ReasonixHomeDir: s.Home}) {
		add("hooks", fmt.Sprintf("%s:%s:%d", h.Scope, h.Event, i))
	}
	for _, items := range out {
		sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	}
	return out, nil
}
