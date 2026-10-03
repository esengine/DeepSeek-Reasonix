package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/session/control"
)

func TestMCPParseRedactsUserinfoOnlyInRiskDisplay(t *testing.T) {
	ctrl := control.New(control.Options{})
	t.Cleanup(ctrl.Close)
	srv := httptest.NewServer(operatorHandler(New(ctrl, NewBroadcaster(), config.ServeConfig{})))
	t.Cleanup(srv.Close)
	for _, tc := range []struct{ input, transport, raw, display string }{
		{"https://demo-user:dummy-password@mcp.example.test/Case/MCP?z=last&a=first", "http", "https://demo-user:dummy-password@mcp.example.test/Case/MCP?z=last&a=first", "https://%3Credacted%3E@mcp.example.test/Case/MCP?z=last&a=first"},
		{"reasonix mcp add demo --sse https://demo-user:dummy%3Apassword@mcp.example.test/events?tenant=demo&token=query-secret", "sse", "https://demo-user:dummy%3Apassword@mcp.example.test/events?tenant=demo&token=query-secret", "https://%3Credacted%3E@mcp.example.test/events?tenant=demo&token=%3Credacted%3E"},
		{`{"mcpServers":{"demo":{"type":"http","url":"https://dummy-user-token@mcp.example.test/mcp"}}}`, "http", "https://dummy-user-token@mcp.example.test/mcp", "https://%3Credacted%3E@mcp.example.test/mcp"},
		{"https://mcp.example.test/Case/MCP?z=last&a=first", "http", "https://mcp.example.test/Case/MCP?z=last&a=first", "https://mcp.example.test/Case/MCP?z=last&a=first"},
		{`{"mcpServers":{"demo":{"type":"http","url":"https://demo-user:dummy-password@mcp.example.test/%zz"}}}`, "http", "https://demo-user:dummy-password@mcp.example.test/%zz", "<redacted>"},
		{`{"mcpServers":{"demo":{"type":"sse","url":"https://mcp.example.test/events?key=dummy-key;mode=read"}}}`, "sse", "https://mcp.example.test/events?key=dummy-key;mode=read", "<redacted>"},
	} {
		resp := postJSON(t, srv.URL+"/mcp/parse", map[string]string{"input": tc.input})
		var got struct {
			Servers []draftServer `json:"servers"`
			Risks   []draftRisk   `json:"risks"`
		}
		err := json.NewDecoder(resp.Body).Decode(&got)
		resp.Body.Close()
		if err != nil || resp.StatusCode != http.StatusOK || len(got.Servers) != 1 || len(got.Risks) != 1 {
			t.Fatalf("preview status=%d body=%+v err=%v", resp.StatusCode, got, err)
		}
		if s := got.Servers[0]; s.Transport != tc.transport || s.URL != tc.raw {
			t.Errorf("preview candidate endpoint changed: %+v", s)
		}
		if r := got.Risks[0]; r.Server != got.Servers[0].Name || r.Kind != "unknown-host" || r.Field != "url" || r.Detail != tc.display {
			t.Errorf("preview endpoint disclosure = %+v", r)
		}
	}
	if names := ctrl.ConfiguredMCPNames(); len(names) != 0 {
		t.Errorf("preview installed servers: %v", names)
	}
}
