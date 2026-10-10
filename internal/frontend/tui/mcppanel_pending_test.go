package tui

import (
	"strings"
	"testing"

	"reasonix/internal/base/i18n"
)

// A repository server waiting for the user reads as waiting, not as switched
// off, says when it waits because what it launches changed, and shows the
// command its approval would cover.
func TestMCPPanelShowsWhyAProjectServerWaitsAndWhatItWouldStart(t *testing.T) {
	m, _ := testModel(t)
	m.mcp = &mcpPanel{servers: []MCPServer{
		{Name: "docs", State: "pending", Source: "project_mcp_json", PendingReason: "changed_since_enabled", Launch: "node ./mcp/server.js"},
		{Name: "fresh", State: "pending", Source: "project_mcp_json", PendingReason: "awaiting_user_decision", Launch: "uvx fresh-mcp"},
	}}
	text := strings.Join(m.mcpPanelLines(), "\n")
	for _, want := range []string{i18n.M.McpPanelChanged, i18n.M.McpPanelPending, "node ./mcp/server.js"} {
		if !strings.Contains(text, want) {
			t.Fatalf("panel lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "docs · "+i18n.M.McpPanelOff) {
		t.Fatalf("a pending server reads as switched off:\n%s", text)
	}
}

// The panel's own styling uses escape sequences, so what is checked is that
// the repository's name contributes none: no OSC, no BEL, no NUL, no bidi.
func TestMCPPanelDrawsAHostileServerNameAsText(t *testing.T) {
	m, _ := testModel(t)
	name := "x\x1b]52;c;Y3VybA==\x07\r\nforged\x00‮evil"
	m.mcp = &mcpPanel{servers: []MCPServer{{Name: name, State: "pending", Source: "project_mcp_json", Launch: "npx pkg"}}}
	for _, view := range [][]string{m.mcpPanelLines(), m.mcpDetail(m.mcp.servers[0])} {
		text := strings.Join(view, "\n")
		for _, bad := range []string{"]52;", "\x07", "\x00", "‮", "\r"} {
			if strings.Contains(text, bad) {
				t.Fatalf("panel carries %q from the server name:\n%q", bad, text)
			}
		}
	}
	m.mcp.confirm = name
	if text := strings.Join(m.mcpPanelLines(), "\n"); strings.Contains(text, "]52;") || strings.Contains(text, "\x07") {
		t.Fatalf("confirmation line carries the raw name: %q", text)
	}
}
