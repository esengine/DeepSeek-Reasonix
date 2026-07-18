package runtimeservice

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"reasonix/internal/command"
	"reasonix/internal/control"
	"reasonix/internal/pluginpkg"
	"reasonix/internal/skill"
)

func TestSessionCatalogIsDeterministicSafeProjection(t *testing.T) {
	source := SessionCatalogSource{
		CustomCommands: []command.Command{{
			Name: "review", Description: "review changes", Body: "SECRET_COMMAND_BODY",
			Source: "/private/command/root/review.md", ArgHint: "secret-arg", Plugin: "workflow",
		}},
		AdditionalCommands: []CatalogCommandSource{{Name: "mcp__safe__prompt", Description: "safe prompt"}},
		MCPServers:         []CatalogMCPSource{{Name: "z-server", Available: false}, {Name: "a-server", Available: true, ToolCount: 3}},
		Skills: []skill.Skill{{
			Name: "audit", Description: "audit code", Scope: skill.ScopeProject,
			Body: "SECRET_SKILL_BODY", Path: "/private/skill/root/SKILL.md", Model: "secret-model",
			AllowedTools: []string{"secret-tool"},
		}},
		Plugins: []pluginpkg.InstalledPlugin{{
			Name: "workflow", Description: "workflow helpers", Enabled: true,
			Source: "https://secret-source.invalid/repo", Root: "/private/plugin/root", Commit: "secret-commit",
		}},
	}
	first, err := ProjectSessionCatalog(source)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ProjectSessionCatalog(SessionCatalogSource{
		CustomCommands:     append([]command.Command(nil), source.CustomCommands...),
		AdditionalCommands: append([]CatalogCommandSource(nil), source.AdditionalCommands...),
		MCPServers:         []CatalogMCPSource{source.MCPServers[1], source.MCPServers[0]},
		Skills:             append([]skill.Skill(nil), source.Skills...), Plugins: append([]pluginpkg.InstalledPlugin(nil), source.Plugins...),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) || first.Revision == "" {
		t.Fatalf("catalog is not deterministic\nfirst: %+v\nsecond: %+v", first, second)
	}
	if len(first.Skills) != 1 || !strings.HasPrefix(string(first.Skills[0].ID), "skill_") || first.Skills[0].Scope != "project" {
		t.Fatalf("skill projection = %+v", first.Skills)
	}
	if len(first.Plugins) != 1 || !strings.HasPrefix(first.Plugins[0].ID, "plugin_") || first.Plugins[0].Name != "workflow" {
		t.Fatalf("plugin projection = %+v", first.Plugins)
	}
	raw, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{
		"SECRET_COMMAND_BODY", "/private/command", "secret-arg", "SECRET_SKILL_BODY",
		"/private/skill", "secret-model", "secret-tool", "secret-source", "/private/plugin", "secret-commit",
	} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("catalog leaked %q: %s", forbidden, raw)
		}
	}
	if len(BuiltinCommandDescriptors()) == 0 {
		t.Fatal("neutral builtin command descriptors are empty")
	}
}

func TestProjectSlashArgsUsesSharedControlCompletionDeterministically(t *testing.T) {
	source := control.ArgData{
		Skills:         []skill.Skill{{Name: "zeta", Scope: skill.ScopeProject}, {Name: "alpha", Scope: skill.ScopeGlobal}},
		DisabledSkills: []skill.Skill{{Name: "disabled", Scope: skill.ScopeGlobal}},
		ServerNames:    []string{"z-server", "a-server"},
		ConfiguredMCP:  []string{"configured"}, DisconnectedMCP: []string{"offline"},
		PluginNames: []string{"z-plugin", "a-plugin"},
	}
	original := append([]string(nil), source.ServerNames...)
	result, err := ProjectSlashArgs("/mcp show ", source)
	if err != nil {
		t.Fatal(err)
	}
	labels := make([]string, len(result.Items))
	for index, item := range result.Items {
		labels[index] = item.Label
	}
	if !reflect.DeepEqual(labels, []string{"a-server", "z-server", "configured", "offline"}) {
		t.Fatalf("MCP slash args = %+v", result)
	}
	if !reflect.DeepEqual(source.ServerNames, original) {
		t.Fatalf("ProjectSlashArgs mutated caller data: %v", source.ServerNames)
	}
	skills, err := ProjectSlashArgs("/skills show ", source)
	if err != nil || len(skills.Items) != 2 || skills.Items[0].Label != "alpha" || skills.Items[1].Label != "zeta" {
		t.Fatalf("skill slash args = %+v, %v", skills, err)
	}
}
