package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/config"
	"reasonix/internal/frontmatter"
)

var reservedSubagentSlashNames = map[string]bool{
	"new": true, "clear": true, "compact": true, "model": true, "provider": true,
	"effort": true, "memory": true, "memory-v5": true, "migrate": true, "migration": true,
	"goal": true, "remember": true, "mcp": true, "hooks": true, "plugin": true, "plugins": true,
	"theme": true, "skill": true, "skills": true, "reload-cmd": true, "tree": true,
	"branch": true, "switch": true, "rewind": true, "plan-exec": true, "prometheus": true,
	"resume": true, "rename": true, "todo": true, "verbose": true, "mouse": true,
	"sandbox": true, "work-mode": true, "profile": true, "auto-plan": true,
	"reasoning-language": true, "paste-image": true, "output-style": true,
	"output-styles": true, "diff-fold": true, "language": true, "help": true,
	"quit": true, "exit": true, "copy": true, "export": true, "forget": true,
}

// ValidateSubagentProfileName protects the shared slash namespace used by
// built-in verbs, custom commands, MCP prompts, Skills, and Subagents.
func ValidateSubagentProfileName(name string, occupied []string) error {
	if !IsValidName(name) {
		return fmt.Errorf("invalid name %q — use letters, digits, '_', '-', '.'", name)
	}
	key := config.SkillNameKey(name)
	if reservedSubagentSlashNames[key] || strings.HasPrefix(key, "mcp__") {
		return fmt.Errorf("%q is reserved by the slash command namespace", name)
	}
	for _, existing := range occupied {
		if config.SkillNameKey(existing) == key {
			return fmt.Errorf("%q already exists in the slash command namespace", name)
		}
	}
	return nil
}

// subagentProfileManagedKeys are the frontmatter keys the desktop and CLI
// profile editors can round-trip without changing execution semantics.
var subagentProfileManagedKeys = map[string]bool{
	"name": true, "description": true, "color": true, "invocation": true,
	"runas": true, "model": true, "effort": true, "allowed-tools": true,
}

// ValidateEditableSubagentProfile verifies that a loaded skill is a manual
// subagent profile whose backing file can be losslessly rewritten by the
// profile editors. Rich hand-authored skills remain owned by the Skills
// workflow so read-only, routing, references, and scripts are never dropped.
func ValidateEditableSubagentProfile(sk Skill) error {
	if sk.RunAs != RunSubagent {
		return fmt.Errorf("%q is not a subagent profile (runAs is not \"subagent\") — manage it as a skill file instead", sk.Name)
	}
	if sk.Invocation != "manual" {
		return fmt.Errorf("%q was not created by a subagent profile editor (invocation is not \"manual\") — manage it as a skill file instead", sk.Name)
	}
	if sk.Scope != ScopeProject && sk.Scope != ScopeGlobal {
		return fmt.Errorf("%q is scope %q and cannot be edited as a project/global subagent profile", sk.Name, sk.Scope)
	}
	if sk.Path == "" || sk.Path == "(builtin)" {
		return fmt.Errorf("%q has no editable file", sk.Name)
	}
	raw, err := os.ReadFile(sk.Path)
	if err != nil {
		return err
	}
	fm, _ := frontmatter.Split(string(raw))
	for key := range fm {
		if !subagentProfileManagedKeys[key] {
			return fmt.Errorf("%q carries frontmatter this editor does not manage (%s) and would silently drop — edit it as a skill file instead", sk.Name, key)
		}
	}
	if filepath.Base(sk.Path) == SkillFile {
		dir := filepath.Dir(sk.Path)
		for _, sibling := range []string{"references", "scripts"} {
			if info, err := os.Stat(filepath.Join(dir, sibling)); err == nil && info.IsDir() {
				return fmt.Errorf("%q has a %s/ directory whose content is folded into the body at load time — editing here would bake it into the main file; edit it as a skill file instead", sk.Name, sibling)
			}
		}
	}
	return nil
}
