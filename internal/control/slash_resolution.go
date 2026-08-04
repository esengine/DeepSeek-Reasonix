package control

import (
	"fmt"
	"strings"

	"reasonix/internal/command"
	"reasonix/internal/skill"
)

const (
	DocsSlashName         = "docs"
	ReasonixDocsSlashName = "reasonix:docs"
)

// SlashCommandOwner identifies the runtime owner of a short slash name. Custom
// commands win over skills, matching Controller dispatch. A single plugin skill
// may own a compatible short name even though only its package-qualified name is
// shown in discovery surfaces.
type SlashCommandOwner uint8

const (
	SlashOwnerBuiltin SlashCommandOwner = iota
	SlashOwnerSkill
	SlashOwnerCustom
)

// ResolveSlashCommandOwner is the shared precedence decision used by dispatch
// and every slash-command discovery surface.
func ResolveSlashCommandOwner(name string, commands []command.Command, skills []skill.Skill) SlashCommandOwner {
	name = strings.TrimPrefix(strings.TrimSpace(name), "/")
	if name == "" {
		return SlashOwnerBuiltin
	}
	for _, cmd := range commands {
		if cmd.Name == name {
			return SlashOwnerCustom
		}
	}
	if _, ok := skill.ResolveSlashSkill(skills, name); ok {
		return SlashOwnerSkill
	}
	return SlashOwnerBuiltin
}

// ResolvedBuiltinSlashName returns the user-facing name for a built-in slash
// command. Its short name remains the default; when a compatible custom command
// or skill owns that short name, the built-in stays reachable through Reasonix's
// explicit namespace.
func ResolvedBuiltinSlashName(name string, commands []command.Command, skills []skill.Skill) string {
	name = strings.TrimPrefix(strings.TrimSpace(name), "/")
	if ResolveSlashCommandOwner(name, commands, skills) == SlashOwnerBuiltin {
		return name
	}
	return QualifiedBuiltinSlashName(name, commands, skills)
}

// QualifiedBuiltinSlashName returns a deterministic Reasonix-owned fallback
// that does not displace any existing custom command or skill. The preferred
// form is /reasonix:<name>; progressively more explicit names are used only if
// a user already owns that spelling too.
func QualifiedBuiltinSlashName(name string, commands []command.Command, skills []skill.Skill) string {
	name = strings.TrimPrefix(strings.TrimSpace(name), "/")
	candidates := []string{"reasonix:" + name, "reasonix:builtin:" + name}
	for _, candidate := range candidates {
		if ResolveSlashCommandOwner(candidate, commands, skills) == SlashOwnerBuiltin {
			return candidate
		}
	}
	for suffix := 2; ; suffix++ {
		candidate := fmt.Sprintf("reasonix:builtin:%s:%d", name, suffix)
		if ResolveSlashCommandOwner(candidate, commands, skills) == SlashOwnerBuiltin {
			return candidate
		}
	}
}

// IsBuiltinDocsSlash reports whether name should execute the embedded Reasonix
// documentation command. The short name is built-in only when unoccupied; the
// qualified fallback is selected without displacing an existing command or skill.
func IsBuiltinDocsSlash(name string, commands []command.Command, skills []skill.Skill) bool {
	name = strings.TrimPrefix(strings.TrimSpace(name), "/")
	if name == DocsSlashName {
		return ResolveSlashCommandOwner(name, commands, skills) == SlashOwnerBuiltin
	}
	return name == QualifiedBuiltinSlashName(DocsSlashName, commands, skills)
}
