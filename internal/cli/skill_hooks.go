package cli

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/skill"
)

func (m *chatTUI) runSkillSubcommand(input string) {
	args := tokenizeArgs(input)
	sub := ""
	if len(args) > 1 {
		sub = strings.ToLower(args[1])
	}
	switch sub {
	case "":
		m.openSkillPicker()
	case "list", "ls":
		m.skillList()
	case "manage", "picker":
		m.openSkillPicker()
	case "show", "cat":
		if len(args) < 3 {
			m.notice("usage: /skills show <name>")
			return
		}
		m.skillShow(args[2])
	case "enable", "disable":
		if len(args) < 3 {
			m.notice("usage: /skills " + sub + " <name>")
			return
		}
		m.skillSetEnabled(args[2], sub == "enable")
	case "new", "init":
		if len(args) < 3 {
			m.notice("usage: /skills new <name> [--global]")
			return
		}
		global := containsArg(args[3:], "--global")
		m.skillNew(args[2], global)
	case "paths":
		m.skillPaths()
	default:
		hint := ""
		if _, ok := m.ctrl.RunSkill("/" + args[1]); ok {
			hint = " (to run it, type /" + args[1] + ")"
		}
		m.notice("unknown /skills subcommand " + args[1] + hint + " — try: /skills, /skills manage, /skills show <name>, /skills enable <name>, /skills disable <name>, /skills new <name>, /skills paths")
	}
}

func (m *chatTUI) skillList() {
	skills := m.skills
	if m.ctrl != nil {
		skills = managementSlashSkills(m.ctrl)
	}
	if len(skills) == 0 {
		m.notice("no skills found. Add SKILL.md / <name>.md under .reasonix/skills (project) or ~/.reasonix/skills (global); .agents/.agent/.claude skills dirs also work. Invoke with /<name> or run_skill.")
		return
	}
	m.commitLine(renderSkillList(m.width, sortedSkills(skills), m.disabledSkillNames()))
}

func (m *chatTUI) skillShow(name string) {
	skills := m.skills
	if m.ctrl != nil {
		skills = managementSlashSkills(m.ctrl)
	}
	for _, s := range skills {
		if s.Name == name || s.SlashName() == strings.TrimPrefix(name, "/") {
			disabled := false
			if m.ctrl != nil {
				disabled = !m.ctrl.SkillEnabled(s.Name)
			}
			m.commitLine(renderSkillShow(m.width, s, disabled))
			return
		}
	}
	m.notice("unknown skill: " + name)
}

func managementSlashSkills(ctrl control.SessionAPI) []skill.Skill {
	if ctrl == nil {
		return nil
	}
	// AllSkills preserves disabled entries; SlashSkills adds every enabled
	// package-qualified alias when multiple plugins export the same bare name.
	all := append([]skill.Skill(nil), ctrl.AllSkills()...)
	all = append(all, ctrl.SlashSkills()...)
	return skill.VisibleSlashSkills(all)
}

func (m *chatTUI) disabledSkillNames() map[string]bool {
	out := map[string]bool{}
	if m.ctrl == nil {
		return out
	}
	for _, s := range m.ctrl.DisabledSkills() {
		out[s.Name] = true
	}
	return out
}

func (m *chatTUI) skillSetEnabled(name string, enabled bool) {
	m.skillSaveEnabledChanges(map[string]bool{name: enabled})
}

func (m *chatTUI) skillSaveEnabledChanges(changes map[string]bool) {
	if len(changes) == 0 {
		return
	}
	if m.buildController == nil {
		m.notice("skill toggle unavailable in this session")
		return
	}
	if m.ctrl == nil {
		m.notice("skill toggle unavailable in this session")
		return
	}
	if m.runtimeSwitchBusy() {
		m.notice("finish or cancel active work and stop background jobs before changing skills")
		return
	}
	if m.modelSwitchPending {
		m.notice("wait for the current runtime switch to finish")
		return
	}
	known := map[string]string{}
	for _, sk := range m.ctrl.AllSkills() {
		known[config.SkillNameKey(sk.Name)] = sk.Name
	}
	for _, sk := range m.ctrl.SlashSkills() {
		known[sk.SlashName()] = sk.Name
	}
	// Lock only the load-modify-save cycle; the session refresh below runs
	// off-lock. The closure returns a non-empty notice on failure.
	if failNotice := func() string {
		unlock := config.LockUserConfigEdits()
		defer unlock()
		cfg := config.LoadForEdit(config.UserConfigPath())
		for name, enabled := range changes {
			key := config.SkillNameKey(name)
			if key == "" {
				key = strings.TrimPrefix(strings.TrimSpace(name), "/")
			}
			canonical, ok := known[key]
			if !ok {
				return "skill " + enableVerb(enabled) + ": unknown skill: " + name
			}
			if err := cfg.SetSkillEnabled(canonical, enabled); err != nil {
				return "skill " + enableVerb(enabled) + ": " + err.Error()
			}
		}
		if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
			return "skill toggle: " + err.Error()
		}
		return ""
	}(); failNotice != "" {
		m.notice(failNotice)
		return
	}
	notice := ""
	if len(changes) == 1 {
		name := ""
		enabled := false
		for n, e := range changes {
			name, enabled = n, e
		}
		if enabled {
			notice = "enabled skill " + name + " — refreshing session"
		} else {
			notice = "disabled skill " + name + " — refreshing session"
		}
	} else {
		notice = fmt.Sprintf("updated %d skills — refreshing session", len(changes))
	}
	m.scheduleSkillSessionRefresh("skill toggle", notice)
}

func (m *chatTUI) scheduleSkillSessionRefresh(reason, notice string) bool {
	if m.buildController == nil {
		m.notice("skill refresh unavailable in this session")
		return false
	}
	if m.ctrl == nil {
		return false
	}
	if m.runtimeSwitchBusy() {
		m.notice("finish or cancel active work and stop background jobs before refreshing skills")
		return false
	}
	if m.modelSwitchPending {
		m.notice("wait for the current runtime switch to finish")
		return false
	}
	if err := m.ctrl.Snapshot(); err != nil {
		slog.Warn(reason+": snapshot failed", "err", err)
	}
	// Snapshot can retarget the controller to a recovery branch. Carry the
	// post-snapshot path so the rebuild does not bind recovered history back to
	// the stale original transcript.
	carried := m.ctrl.History()
	prevPath := m.ctrl.SessionPath()
	// Move the lease before the rebuilt controller binds prevPath for writing
	// (AdoptHistory resumes there): after a snapshot retarget the lease still
	// guards the old path, and the async build must not open an unguarded
	// writer on the recovery branch.
	if err := m.rebindSessionLease(prevPath); err != nil {
		m.notice(reason + ": " + sessionLeaseHeldNotice(err))
		return false
	}
	if notice != "" {
		m.notice(notice)
	}
	oldCtrl := m.ctrl
	build := m.buildController
	ref := m.modelRef
	m.modelSwitchPending = true
	m.pendingModelSwitch = func() tea.Msg {
		c, err := build(controllerBuildSpec{
			ModelRef:         ref,
			RuntimeProfile:   m.runtimeProfile,
			ToolApprovalMode: oldCtrl.ToolApprovalMode(),
			PlanMode:         oldCtrl.PlanMode(),
		}, carried, prevPath, oldCtrl)
		if err != nil {
			return modelSwitchMsg{ref: ref, err: err}
		}
		return modelSwitchMsg{
			ref:      ref,
			ctrl:     c,
			oldCtrl:  oldCtrl,
			label:    c.Label(),
			commands: c.Commands(),
			skills:   c.SlashSkills(),
			host:     c.Host(),
		}
	}
	return true
}

func enableVerb(enabled bool) string {
	if enabled {
		return "enable"
	}
	return "disable"
}

func (m *chatTUI) skillNew(name string, global bool) {
	st := m.skillStore()
	scope := skill.ScopeProject
	if global || !st.HasProjectScope() {
		scope = skill.ScopeGlobal
	}
	path, err := st.Create(name, scope)
	if err != nil {
		m.notice("skill new: " + err.Error())
		return
	}
	m.notice(fmt.Sprintf("created skill %q at %s — edit it, then /new (or restart) to pick it up", name, path))
}

func (m *chatTUI) skillPaths() {
	st := m.skillStore()
	m.commitLine(renderSkillPaths(m.width, st.Roots()))
}

func (m *chatTUI) skillStore() *skill.Store {
	cwd, _ := os.Getwd()
	var custom []string
	var excluded []string
	var pluginPaths map[string][]string
	var pluginAgentPaths map[string][]string
	maxDepth := 3
	if cfg, err := config.Load(); err == nil {
		custom = cfg.SkillCustomPaths()
		excluded = cfg.SkillExcludedPaths()
		pluginPaths = cfg.PluginPackageSkillOwners()
		pluginAgentPaths = cfg.PluginPackageAgentOwners()
		maxDepth = cfg.SkillMaxDepth()
	}
	return skill.New(skill.Options{ProjectRoot: cwd, CustomPaths: custom, PluginPaths: pluginPaths, PluginAgentPaths: pluginAgentPaths, ExcludedPaths: excluded, MaxDepth: maxDepth})
}

func (m *chatTUI) runHooksSubcommand(input string) {
	args := tokenizeArgs(input)
	sub := ""
	if len(args) > 1 {
		sub = strings.ToLower(args[1])
	}
	cwd, _ := os.Getwd()
	switch sub {
	case "", "list", "ls":
		m.hooksList(cwd)
	case "trust":
		// Backward-compatible response for old clients and saved commands.
		m.notice("project hooks are enabled automatically; no trust action is required")
	default:
		m.notice("unknown /hooks subcommand " + args[1] + " — try: /hooks or /hooks list")
	}
}

func (m *chatTUI) hooksList(cwd string) {
	active := m.ctrl.HookRunner().Hooks()
	m.commitLine(renderHooks(m.width, active))
}

func containsArg(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}
