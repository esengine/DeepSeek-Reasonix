package cli

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/config"
	"reasonix/internal/hook"
	"reasonix/internal/skill"
)

// runSkillSubcommand handles "/skill" (and "/skills"): list the discoverable
// skills, show one's body, scaffold a new one, or inspect the discovery paths.
// "/skill <name> [args]" with no recognised subcommand falls through to invoking
// the skill (handled by runSlashCommand's default branch), so this only owns the
// management verbs.
func (m *chatTUI) runSkillSubcommand(input string) {
	args := tokenizeArgs(input) // args[0] == "/skill"
	sub := ""
	if len(args) > 1 {
		sub = strings.ToLower(args[1])
	}
	switch sub {
	case "", "list", "ls":
		m.skillList()
	case "show", "cat":
		if len(args) < 3 {
			m.notice("usage: /skill show <name>")
			return
		}
		m.skillShow(args[2])
	case "enable", "disable":
		if len(args) < 3 {
			m.notice("usage: /skill " + sub + " <name>")
			return
		}
		m.skillSetEnabled(args[2], sub == "enable")
	case "new", "init":
		if len(args) < 3 {
			m.notice("usage: /skill new <name> [--global]")
			return
		}
		global := containsArg(args[3:], "--global")
		m.skillNew(args[2], global)
	case "paths":
		m.skillPaths()
	default:
		// /skill is management-only; a skill is invoked directly as /<name>.
		hint := ""
		if _, ok := m.ctrl.RunSkill("/" + args[1]); ok {
			hint = " (to run it, type /" + args[1] + ")"
		}
		m.notice("unknown /skill subcommand " + args[1] + hint + " — try: /skill, /skill show <name>, /skill enable <name>, /skill disable <name>, /skill new <name>, /skill paths")
	}
}

func (m *chatTUI) skillList() {
	skills := m.skills
	if m.ctrl != nil {
		skills = m.ctrl.AllSkills()
	}
	if len(skills) == 0 {
		m.notice("no skills found. Add SKILL.md / <name>.md under .reasonix/skills (project) or ~/.reasonix/skills (global); .agents/.agent/.claude skills dirs also work. Invoke with /<name> or run_skill.")
		return
	}
	m.commitLine(renderSkillList(m.width, skills, m.disabledSkillNames()))
}

func (m *chatTUI) skillShow(name string) {
	skills := m.skills
	if m.ctrl != nil {
		skills = m.ctrl.AllSkills()
	}
	for _, s := range skills {
		if s.Name == name {
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
	if m.buildController == nil {
		m.notice("skill toggle unavailable in this session")
		return
	}
	if m.ctrl.Running() {
		m.notice("cannot change skills while a turn is running")
		return
	}
	if err := m.ctrl.SetSkillEnabled(name, enabled); err != nil {
		m.notice("skill " + enableVerb(enabled) + ": " + err.Error())
		return
	}
	carried := m.ctrl.History()
	prevPath := m.ctrl.SessionPath()
	if err := m.ctrl.Snapshot(); err != nil {
		slog.Warn("skill toggle: snapshot failed", "err", err)
	}
	if enabled {
		m.notice("enabled skill " + name + " — refreshing session")
	} else {
		m.notice("disabled skill " + name + " — refreshing session")
	}
	oldCtrl := m.ctrl
	build := m.buildController
	ref := m.modelRef
	m.modelSwitchPending = true
	m.pendingModelSwitch = func() tea.Msg {
		c, err := build(ref, carried, prevPath)
		if err != nil {
			return modelSwitchMsg{ref: ref, err: err}
		}
		return modelSwitchMsg{
			ref:      ref,
			ctrl:     c,
			oldCtrl:  oldCtrl,
			label:    c.Label(),
			commands: c.Commands(),
			skills:   c.Skills(),
			host:     c.Host(),
		}
	}
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

// skillStore builds a Store reflecting this session's project root + configured
// custom paths, for the management verbs that need to write or enumerate roots.
func (m *chatTUI) skillStore() *skill.Store {
	cwd, _ := os.Getwd()
	var custom []string
	if cfg, err := config.Load(); err == nil {
		custom = cfg.SkillCustomPaths()
	}
	return skill.New(skill.Options{ProjectRoot: cwd, CustomPaths: custom})
}

// runHooksSubcommand handles "/hooks": list the active hooks and the project's
// trust state, or trust the current project so its hooks load next session.
func (m *chatTUI) runHooksSubcommand(input string) {
	args := tokenizeArgs(input) // args[0] == "/hooks"
	sub := ""
	if len(args) > 1 {
		sub = strings.ToLower(args[1])
	}
	cwd, _ := os.Getwd()
	switch sub {
	case "", "list", "ls":
		m.hooksList(cwd)
	case "trust":
		if err := hook.Trust(cwd, ""); err != nil {
			m.notice("hooks trust: " + err.Error())
			return
		}
		m.notice("trusted this project's hooks — they load on the next /new or restart")
	default:
		m.notice("unknown /hooks subcommand " + args[1] + " — try: /hooks, /hooks trust")
	}
}

func (m *chatTUI) hooksList(cwd string) {
	active := m.ctrl.HookRunner().Hooks()
	trusted := hook.IsTrusted(cwd, "")
	m.commitLine(renderHooks(m.width, active, trusted, hook.ProjectDefinesHooks(cwd)))
}

// containsArg reports whether flag appears in args.
func containsArg(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}
