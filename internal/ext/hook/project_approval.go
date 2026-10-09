package hook

import (
	"fmt"
	"path/filepath"
	"strings"

	"mvdan.cc/sh/v3/syntax"

	"reasonix/internal/base/shellparse"
	"reasonix/internal/contract/config"
)

// IssueAwaitingApproval marks a project hook held back until the user approves
// the project's hooks as they stand now.
const IssueAwaitingApproval = "awaiting_approval"

// ProjectHooksProgram is the project's hooks file as one program the user
// approves: the declarations plus every workspace file a command names.
func ProjectHooksProgram(projectRoot string) (config.ProjectProgram, bool) {
	path := ProjectSettingsPath(projectRoot)
	s := readSettings(path)
	if s == nil {
		return config.ProjectProgram{}, false
	}
	return projectHooksProgram(projectRoot, path, s)
}

func projectHooksProgram(root, path string, s *Settings) (config.ProjectProgram, bool) {
	var hooks []ResolvedHook
	appendResolved(&hooks, s, ScopeProject, path)
	if len(hooks) == 0 {
		return config.ProjectProgram{}, false
	}
	ws := root
	if real, err := filepath.EvalSymlinks(root); err == nil {
		ws = real
	}
	var words, commands []string
	for _, h := range hooks {
		base := ws
		if cwd := strings.TrimSpace(h.Cwd); cwd != "" {
			base = cwd
			if !filepath.IsAbs(cwd) {
				base = filepath.Join(ws, cwd)
			}
		}
		for _, word := range commandWords(h.Command) {
			// Joined, not cleaned: the OS follows a link before "..".
			if !filepath.IsAbs(word) {
				word = base + string(filepath.Separator) + word
			}
			words = append(words, word)
		}
		line := string(h.Event) + ": " + h.Command
		if h.Cwd != "" {
			line += " (cwd " + h.Cwd + ")"
		}
		commands = append(commands, line+config.EnvSummary(h.Env))
	}
	detail := strings.Join(commands, "; ")
	return config.NewProjectProgram(config.ProjectProgramHooks, SettingsDirname+"/"+SettingsFilename, detail, ws, ws, s.Hooks, words), true
}

// commandWords lists the static words of every command in a hook line. A line
// the Bash parser cannot read (a cmd.exe one) falls back to its fields.
func commandWords(command string) []string {
	file, err := shellparse.ParseBash(command)
	if err != nil || file == nil {
		return strings.Fields(command)
	}
	var out []string
	syntax.Walk(file, func(node syntax.Node) bool {
		if word, ok := node.(*syntax.Word); ok {
			if value, ok := shellparse.StaticWord(word); ok && strings.ContainsAny(value, `/\.`) {
				out = append(out, value)
			}
			return false
		}
		return true
	})
	return out
}

// PendingProjectHooks reports the project's hooks when they are held back.
func PendingProjectHooks(opts LoadOptions) (config.ProjectProgram, bool) {
	if opts.ProjectRoot == "" {
		return config.ProjectProgram{}, false
	}
	program, ok := ProjectHooksProgram(opts.ProjectRoot)
	if !ok || projectProgramApproved(opts, program) {
		return config.ProjectProgram{}, false
	}
	return program, true
}

// ApproveSavedProjectHooks approves the hooks block Save writes for settings,
// not whatever the file holds afterwards: a write that lands in between is
// then a different declaration, and stays unapproved.
func ApproveSavedProjectHooks(opts LoadOptions, settings Settings) error {
	hooks, err := normalizedHooks(settings)
	if err != nil {
		return err
	}
	program, ok := projectHooksProgram(opts.ProjectRoot, ProjectSettingsPath(opts.ProjectRoot), &Settings{Hooks: hooks})
	if !ok {
		return nil
	}
	return config.NewProjectProgramStore(reasonixHomeForOptions(opts)).Approve(opts.ProjectRoot, program)
}

// appendApprovedProjectHooks adds the project's hooks only when approved as
// they stand, each carrying the approval so it is checked again before it runs.
func appendApprovedProjectHooks(out *[]ResolvedHook, opts LoadOptions, path string, s *Settings) {
	program, ok := projectHooksProgram(opts.ProjectRoot, path, s)
	if ok && !projectProgramApproved(opts, program) {
		return
	}
	start := len(*out)
	appendResolved(out, s, ScopeProject, path)
	for i := start; ok && i < len(*out); i++ {
		(*out)[i].approval = &program
	}
}

func projectProgramApproved(opts LoadOptions, program config.ProjectProgram) bool {
	ok, err := config.NewProjectProgramStore(reasonixHomeForOptions(opts)).Approved(opts.ProjectRoot, program)
	return err == nil && ok
}

// refuseChangedHook records, instead of running, a project hook whose approval
// no longer holds because a file it names changed after loading. A gating hook
// blocks: the script that guards an action is the one thing the agent could
// otherwise edit to switch the guard off.
func refuseChangedHook(report *Report, h ResolvedHook) bool {
	if h.approval == nil {
		return false
	}
	err := h.approval.Verify()
	if err == nil {
		return false
	}
	recordUnevaluable(report, h, fmt.Errorf("%w: %w", ErrApprovalChanged, err))
	report.Outcomes[len(report.Outcomes)-1].Refusal = err
	return true
}
