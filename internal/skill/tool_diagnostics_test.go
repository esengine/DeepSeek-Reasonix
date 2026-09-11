package skill

import (
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/tool"
	_ "reasonix/internal/tool/builtin"
)

func TestToolReferenceDiagnostics(t *testing.T) {
	binding := tool.MCPBinding{Package: "example-plugin", Server: "github", RawName: "search", VisibleName: "search", CallableName: "mcp__github__search", CapabilityID: "mcp-tool:github/search"}
	other := binding
	other.Server, other.CallableName, other.CapabilityID = "other", "mcp__other__search", "mcp-tool:other/search"
	for _, tc := range []struct {
		ref, code string
		bindings  []tool.MCPBinding
	}{
		{"use_capability", "", nil},
		{"grep", "", nil},
		{"new_hidden_tool", "", nil},
		{"read_*", "", nil},
		{"*", "", nil},
		{"[", "skill.tool_reference_invalid", nil},
		{"mcp-tool:github", "skill.tool_reference_invalid", nil},
		{"mcp-server:", "skill.tool_reference_invalid", nil},
		{"mcp__github__", "skill.tool_reference_invalid", nil},
		{"typo_read_file", "skill.tool_reference_unknown", nil},
		{"lsp_typo", "skill.tool_reference_unknown", nil},
		{"mcp__future__search", "skill.tool_reference_unverified", nil},
		{"future/*", "skill.tool_reference_unverified", nil},
		{"session:tool_result", "skill.tool_reference_unverified", nil},
		{"tool:docs", "skill.tool_reference_unverified", nil},
		{"skill:review", "skill.tool_reference_unverified", nil},
		{"memory:remember", "skill.tool_reference_unverified", nil},
		{"github/search", "", []tool.MCPBinding{binding}},
		{"search", "", []tool.MCPBinding{binding}},
		{"mcp-tool:github/search", "", []tool.MCPBinding{binding}},
		{"mcp__github__search", "", []tool.MCPBinding{binding}},
		{"search", "skill.tool_reference_ambiguous", []tool.MCPBinding{binding, other}},
		{"mcp__*", "", []tool.MCPBinding{binding, other}},
	} {
		t.Run(tc.ref+tc.code, func(t *testing.T) {
			d := CheckToolReferences([]Skill{{Name: "example", Plugin: "example-plugin", AllowedTools: []string{tc.ref}}}, ToolReferenceOptions{
				Known: tool.KnownToolNames(), Registered: []tool.ContractEntry{{Name: "new_hidden_tool"}}, Bindings: tc.bindings,
			})
			if tc.code == "" {
				if len(d) != 0 {
					t.Fatalf("unexpected diagnostic: %+v", d)
				}
				return
			}
			if len(d) != 1 || d[0].Code != tc.code || d[0].Reference != tc.ref || d[0].Skill != "example" {
				t.Fatalf("got %+v, want %s", d, tc.code)
			}
			wantSeverity := "warning"
			if tc.code == "skill.tool_reference_unverified" {
				wantSeverity = "info"
			}
			if d[0].Severity != wantSeverity {
				t.Fatalf("severity: %+v", d)
			}
		})
	}
}

func TestBuiltinSkillReferencesAndMCPRequirements(t *testing.T) {
	store := DiagnosticStore(t.TempDir(), t.TempDir(), t.TempDir(), config.Default())
	if len(store.List()) == 0 {
		t.Fatal("no built-in skills loaded")
	}
	if d := CheckToolReferences(store.List(), ToolReferenceOptions{Known: tool.KnownToolNames()}); len(d) != 0 {
		t.Fatalf("builtin references: %+v", d)
	}
	sk := []Skill{{Name: "dependent", AutoUse: "require", Requires: []string{"mcp-server:github"}, AllowedTools: []string{"use_capability"}}}
	for _, tc := range []struct {
		configured   bool
		failed, code string
	}{
		{false, "", "skill.mcp_dependency_missing"},
		{true, "", ""},
		{true, "spawn failed", "skill.mcp_dependency_failed"},
	} {
		var plugins []config.PluginEntry
		if tc.configured {
			plugins = []config.PluginEntry{{Name: "github"}}
		}
		if d := CheckToolReferences(sk, ToolReferenceOptions{Known: tool.KnownToolNames()}); len(d) != 0 {
			t.Fatal(d)
		}
		d := CheckMCPRequirements(sk, plugins, map[string]string{"github": tc.failed})
		if tc.code == "" {
			if len(d) != 0 {
				t.Fatal(d)
			}
			continue
		}
		if len(d) != 1 || d[0].Code != tc.code {
			t.Fatalf("got %+v, want %s", d, tc.code)
		}
	}
}
