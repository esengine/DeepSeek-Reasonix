package control

import (
	"testing"

	"reasonix/internal/command"
	"reasonix/internal/skill"
)

func TestResolvedBuiltinDocsSlashNameMatchesRuntimeOwner(t *testing.T) {
	tests := []struct {
		name     string
		commands []command.Command
		skills   []skill.Skill
		owner    SlashCommandOwner
		builtin  string
	}{
		{name: "unoccupied", owner: SlashOwnerBuiltin, builtin: DocsSlashName},
		{
			name:     "visible custom command",
			commands: []command.Command{{Name: DocsSlashName}},
			owner:    SlashOwnerCustom,
			builtin:  ReasonixDocsSlashName,
		},
		{
			name:     "hidden plugin command alias",
			commands: []command.Command{{Name: DocsSlashName, Plugin: "manuals", Hidden: true}},
			owner:    SlashOwnerCustom,
			builtin:  ReasonixDocsSlashName,
		},
		{
			name:    "visible project skill",
			skills:  []skill.Skill{{Name: DocsSlashName}},
			owner:   SlashOwnerSkill,
			builtin: ReasonixDocsSlashName,
		},
		{
			name:    "compatible plugin skill alias",
			skills:  []skill.Skill{{Name: DocsSlashName, Plugin: "manuals"}},
			owner:   SlashOwnerSkill,
			builtin: ReasonixDocsSlashName,
		},
		{
			name: "ambiguous plugin skill aliases",
			skills: []skill.Skill{
				{Name: DocsSlashName, Plugin: "manuals"},
				{Name: DocsSlashName, Plugin: "handbook"},
			},
			owner:   SlashOwnerBuiltin,
			builtin: DocsSlashName,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ResolveSlashCommandOwner(DocsSlashName, tt.commands, tt.skills); got != tt.owner {
				t.Fatalf("owner = %v, want %v", got, tt.owner)
			}
			if got := ResolvedBuiltinSlashName(DocsSlashName, tt.commands, tt.skills); got != tt.builtin {
				t.Fatalf("built-in name = %q, want %q", got, tt.builtin)
			}
			if got := IsBuiltinDocsSlash(DocsSlashName, tt.commands, tt.skills); got != (tt.owner == SlashOwnerBuiltin) {
				t.Fatalf("short built-in = %v, owner=%v", got, tt.owner)
			}
			if !IsBuiltinDocsSlash(ReasonixDocsSlashName, tt.commands, tt.skills) {
				t.Fatal("preferred qualified Reasonix docs name should resolve to the built-in when unoccupied")
			}
		})
	}
}

func TestQualifiedBuiltinDocsSlashPreservesExistingQualifiedCommands(t *testing.T) {
	commands := []command.Command{
		{Name: DocsSlashName},
		{Name: ReasonixDocsSlashName},
		{Name: "reasonix:builtin:docs"},
	}
	want := "reasonix:builtin:docs:2"
	if got := ResolvedBuiltinSlashName(DocsSlashName, commands, nil); got != want {
		t.Fatalf("resolved built-in name = %q, want %q", got, want)
	}
	for _, existing := range []string{DocsSlashName, ReasonixDocsSlashName, "reasonix:builtin:docs"} {
		if IsBuiltinDocsSlash(existing, commands, nil) {
			t.Fatalf("existing command %q was displaced by the built-in", existing)
		}
	}
	if !IsBuiltinDocsSlash(want, commands, nil) {
		t.Fatalf("generated fallback %q does not resolve to the built-in", want)
	}
}
