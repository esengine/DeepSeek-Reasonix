package capdiag

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/skill"
	"reasonix/internal/tool"
)

func TestSkillToolIssuesUseObservedMCPBindings(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, ".reasonix", "skills", "mcp-example", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("---\nname: mcp-example\ndescription: Test\nauto-use: require\nrequires: [mcp-server:github]\nallowed-tools: [mcp-tool:github/search]\n---\nTest\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Plugins = []config.PluginEntry{{Name: "github"}}
	store := skill.DiagnosticStore(root, t.TempDir(), t.TempDir(), cfg)
	for _, tc := range []struct{ status, code string }{
		{"", "skill.tool_reference_unverified"},
		{"connected", ""},
		{"probed", ""},
		{"failed", "skill.mcp_dependency_failed"},
	} {
		t.Run(tc.status, func(t *testing.T) {
			mcp := MCPReport{Servers: []MCPServerInfo{{Name: "github", RuntimeStatus: tc.status, Error: "failed", Tools: []MCPToolInfo{{Name: "search"}}}}}
			if tc.status == "connected" || tc.status == "probed" {
				mcp.bindings = []tool.MCPBinding{{Server: "github", RawName: "search", CallableName: "mcp__github__search", CapabilityID: "mcp-tool:github/search"}}
			}
			issues := skillToolIssues(store, cfg, mcp, func(s string) string { return s })
			if tc.code == "" {
				if len(issues) != 0 {
					t.Fatalf("observed binding unresolved: %+v", issues)
				}
				return
			}
			for _, issue := range issues {
				if issue.Code == tc.code {
					return
				}
			}
			t.Fatalf("missing %s: %+v", tc.code, issues)
		})
	}
}
