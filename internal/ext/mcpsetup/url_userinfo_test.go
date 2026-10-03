package mcpsetup

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMCPURLDisplayRedactsUserinfo(t *testing.T) {
	for _, tc := range []struct{ raw, display string }{
		{"https://demo-user:dummy-password@mcp.example.test/Case/MCP?z=last&a=first#anchor", "https://%3Credacted%3E@mcp.example.test/Case/MCP?z=last&a=first#anchor"},
		{"https://demo%40user:dummy%3Apassword@mcp.example.test/mcp", "https://%3Credacted%3E@mcp.example.test/mcp"},
		{"https://dummy-user-token@mcp.example.test/mcp", "https://%3Credacted%3E@mcp.example.test/mcp"},
		{"https://:dummy-password@mcp.example.test/mcp", "https://%3Credacted%3E@mcp.example.test/mcp"},
		{"https://demo-user:dummy-password@mcp.example.test/mcp?tenant=demo&token=query-secret&token=second-secret", "https://%3Credacted%3E@mcp.example.test/mcp?tenant=demo&token=%3Credacted%3E"},
		{"https://mcp.example.test/Case/MCP?z=last&a=first#anchor", "https://mcp.example.test/Case/MCP?z=last&a=first#anchor"},
		{" https://mcp.example.test/mcp?mode=Keep%20Case ", " https://mcp.example.test/mcp?mode=Keep%20Case "},
		{"https://mcp.example.test/mcp?token=query-secret&tenant=demo", "https://mcp.example.test/mcp?tenant=demo&token=%3Credacted%3E"},
		{"https://demo-user:dummy-password@mcp.example.test/%zz", "<redacted>"},
		{"https://demo-user:dummy-password@mcp.example.test/mcp?tenant=demo&key=dummy-key;mode=read", "<redacted>"},
		{"https://mcp.example.test/mcp?key=dummy-key;mode=read", "<redacted>"},
		{"https://mcp.example.test/mcp?%zz=dummy-key", "<redacted>"},
		{"https://mcp.example.test/mcp?key=dummy%zz", "<redacted>"},
		{"https://mcp.example.test/mcp?token=masked&key=dummy-key;mode=read", "<redacted>"},
		{"https://mcp.example.test/mcp?note=alpha%3Bbeta&mode=read", "https://mcp.example.test/mcp?note=alpha%3Bbeta&mode=read"},
		{"https://mcp.example.test/mcp?key=dummy%3Bkey&mode=read", "https://mcp.example.test/mcp?key=%3Credacted%3E&mode=read"},
		{"https://mcp.example.test/mcp?password=dummy-password&passwd=dummy-passwd&tenant=demo", "https://mcp.example.test/mcp?passwd=%3Credacted%3E&password=%3Credacted%3E&tenant=demo"},
		{"https://mcp.example.test/mcp?PASSWORD=first-secret&PASSWORD=second-secret", "https://mcp.example.test/mcp?PASSWORD=%3Credacted%3E"},
		{"https://mcp.example.test/mcp#access_token=fragment-secret&tenant=demo", "https://mcp.example.test/mcp#%3Credacted%3E"},
		{"https://demo-user:dummy-password@mcp.example.test/mcp?mode=read#passwd=fragment-secret", "https://%3Credacted%3E@mcp.example.test/mcp?mode=read#%3Credacted%3E"},
		{"https://mcp.example.test/mcp#token=fragment-secret;mode=read", "<redacted>"},
		{"https://mcp.example.test/mcp#token=fragment%25zz", "<redacted>"},
		{"https://mcp.example.test/mcp#%25zz=fragment-secret", "<redacted>"},
		{"https://demo-user:dummy-password@mcp%zz.example.test/mcp", "<redacted>"},
		{"https://demo%zz:dummy-password@mcp.example.test/mcp", "<redacted>"},
		{"https://demo-user:dummy-password@mcp.example.test/mcp#fragment%zz", "<redacted>"},
		{"", ""},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			if got := RedactURL(tc.raw); got != tc.display {
				t.Errorf("endpoint display = %q, want %q", got, tc.display)
			}
		})
	}
}

func TestMCPPreviewPreservesEndpointAndRedactsUserinfoRisk(t *testing.T) {
	raw := "https://demo-user:dummy-password@mcp.example.test/Case/MCP?z=last&a=first#anchor"
	want := "https://%3Credacted%3E@mcp.example.test/Case/MCP?z=last&a=first#anchor"
	for _, transport := range []string{"http", "sse"} {
		block, err := json.Marshal(map[string]any{"mcpServers": map[string]any{"demo": map[string]string{"type": transport, "url": raw}}})
		if err != nil {
			t.Fatal(err)
		}
		for _, input := range []string{string(block), "reasonix mcp add demo --" + transport + " " + raw} {
			t.Run(transport+input, func(t *testing.T) {
				draft, err := Parse(input)
				if err != nil || len(draft.Entries) != 1 || len(draft.Risks) != 1 {
					t.Fatalf("draft=%+v err=%v", draft, err)
				}
				if e := draft.Entries[0]; e.Name != "demo" || e.Type != transport || e.URL != raw {
					t.Errorf("candidate endpoint changed: %+v", e)
				}
				if r := draft.Risks[0]; r.Server != "demo" || r.Kind != "unknown-host" || r.Field != "url" || r.Detail != want {
					t.Errorf("endpoint risk = %+v", r)
				}
				if strings.Contains(draft.Risks[0].Detail, "dummy-password") {
					t.Error("risk display contains a fixture password")
				}
			})
		}
	}
}
