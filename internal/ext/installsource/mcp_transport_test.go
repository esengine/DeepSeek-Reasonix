package installsource

import (
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
)

func TestRemoteMCPTransportUsesEndpointPath(t *testing.T) {
	cases := []struct {
		name, source, transport, want string
	}{
		{"host", "https://assets.example.test/mcp", "auto", "http"},
		{"query", "https://example.test/mcp?tenant=assets", "auto", "http"},
		{"fragment", "https://example.test/mcp#assets", "auto", "http"},
		{"userinfo", "https://assets@example.test/mcp", "auto", "http"},
		{"path substring", "https://example.test/assets/mcp", "auto", "http"},
		{"segment suffix", "https://example.test/sse-other/mcp", "auto", "http"},
		{"sse path", "https://example.test/sse/stream", "auto", "sse"},
		{"sse nested", "https://example.test/mcp/sse", "auto", "sse"},
		{"sse trailing slash", "https://example.test/mcp/sse/", "auto", "sse"},
		{"sse case", "https://example.test/SSE/stream", "auto", "sse"},
		{"sse encoded", "https://example.test/%73se/stream", "auto", "sse"},
		{"encoded separator before sse", "https://example.test/mcp%2F%73%73%65", "auto", "http"},
		{"encoded separator after sse", "https://example.test/mcp/%73%73%65%2Fstream", "auto", "http"},
		{"encoded leading separator", "https://example.test/mcp/%2Fsse", "auto", "http"},
		{"double encoded sse", "https://example.test/mcp/%2573se", "auto", "http"},
		{"explicit http", "https://example.test/sse/stream", "http", "http"},
		{"explicit sse", "https://assets.example.test/mcp", "sse", "sse"},
	}
	for _, kind := range []string{"auto", "mcp"} {
		for _, tc := range cases {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				project := testenv.TempDir(t)
				stub := &stubConnector{toolCount: 2}
				approved := 0
				tl := NewTool(Options{
					ProjectRoot: project, HomeDir: testenv.TempDir(t), ConnectMCP: stub.connector(), RequireApprovedPlan: true,
					Approval: func(actions []action) error {
						approved++
						if len(actions) != 1 || actions[0].Transport != tc.want || actions[0].URL != tc.source {
							t.Fatalf("approved actions = %+v", actions)
						}
						return nil
					},
				})
				args := map[string]any{"source": tc.source, "kind": kind, "transport": tc.transport, "scope": "project", "name": "endpoint"}
				plan := execInstall(t, tl, args)
				if !plan.OK || plan.Status != "planned" || len(plan.Actions) != 1 || plan.PlanID == "" {
					t.Fatalf("plan = %+v", plan)
				}
				if plan.Actions[0].Transport != tc.want || plan.Actions[0].URL != tc.source {
					t.Fatalf("plan transport/url = %q %q, want %q %q", plan.Actions[0].Transport, plan.Actions[0].URL, tc.want, tc.source)
				}
				if approved != 0 || len(stub.connected) != 0 || len(config.LoadForEdit(filepath.Join(project, "reasonix.toml")).Plugins) != 0 {
					t.Fatal("preview approved, connected, or persisted a server")
				}
				args["apply"], args["planId"] = true, plan.PlanID
				installed := execInstall(t, tl, args)
				if !installed.OK || installed.Status != "done" || approved != 1 || len(stub.connected) != 1 {
					t.Fatalf("apply = %+v, approvals = %d, connected = %d", installed, approved, len(stub.connected))
				}
				entry := stub.connected[0]
				if entry.Type != tc.want || entry.URL != tc.source || entry.Source != config.MCPSourceProjectConfig {
					t.Fatalf("connected entry = %+v", entry)
				}
				cfg := config.LoadForEdit(filepath.Join(project, "reasonix.toml"))
				if len(cfg.Plugins) != 1 || cfg.Plugins[0].Type != tc.want || cfg.Plugins[0].URL != tc.source {
					t.Fatalf("persisted plugins = %+v", cfg.Plugins)
				}
			})
		}
	}
}
