package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"reasonix/internal/contract/config"
)

func TestMCPGetRedactsEndpointUserinfoWithoutChangingConfig(t *testing.T) {
	isolateCLIConfigHome(t)
	cfg := config.Default()
	cfg.Plugins = []config.PluginEntry{{Name: "global-http", Type: "http", URL: "https://demo-user:dummy-password@mcp.example.test/Case/MCP?z=last&a=first"}}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatal(err)
	}
	project := []byte(`{"mcpServers":{"password-query":{"type":"http","url":"https://mcp.example.test/mcp?password=dummy-password&passwd=dummy-passwd"},"fragment-token":{"type":"sse","url":"https://mcp.example.test/events#access_token=fragment-secret"},"invalid-path":{"type":"http","url":"https://demo-user:dummy-password@mcp.example.test/%zz"},"invalid-query":{"type":"sse","url":"https://mcp.example.test/events?key=dummy-key;mode=read"},"project-sse":{"type":"sse","url":"https://demo-user:dummy%3Apassword@mcp.example.test/events?tenant=demo&token=query-secret"}}}`)
	if err := os.WriteFile(".mcp.json", project, 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(config.UserConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, display string }{
		{"global-http", "https://%3Credacted%3E@mcp.example.test/Case/MCP?z=last&a=first"},
		{"project-sse", "https://%3Credacted%3E@mcp.example.test/events?tenant=demo&token=%3Credacted%3E"},
		{"password-query", "https://mcp.example.test/mcp?passwd=%3Credacted%3E&password=%3Credacted%3E"},
		{"fragment-token", "https://mcp.example.test/events#%3Credacted%3E"},
		{"invalid-path", "<redacted>"},
		{"invalid-query", "<redacted>"},
	} {
		out := captureStdout(t, func() {
			if code := Run([]string{"mcp", "get", tc.name}, "test-version"); code != 0 {
				t.Fatalf("mcp get exit=%d", code)
			}
		})
		if !strings.Contains(out, "url: "+tc.display+"\n") {
			t.Errorf("incorrect mcp get endpoint display: %s", out)
		}
	}
	for _, verb := range []string{"list", "ls"} {
		out := captureStdout(t, func() {
			if code := Run([]string{"mcp", verb}, "test-version"); code != 0 {
				t.Fatalf("mcp %s exit=%d", verb, code)
			}
		})
		for _, secret := range []string{"demo-user", "dummy-password", "dummy%3Apassword", "dummy-key", "dummy-passwd", "query-secret", "fragment-secret"} {
			if strings.Contains(out, secret) {
				t.Errorf("mcp %s exposes fixture credential %q: %s", verb, secret, out)
			}
		}
		if !strings.Contains(out, "https://%3Credacted%3E@mcp.example.test/Case/MCP?z=last&a=first") || !strings.Contains(out, "https://mcp.example.test/events#%3Credacted%3E") {
			t.Errorf("mcp %s endpoint display missing: %s", verb, out)
		}
	}
	after, err := os.ReadFile(config.UserConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	projectAfter, err := os.ReadFile(".mcp.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) || !bytes.Equal(project, projectAfter) {
		t.Fatal("mcp get rewrote the stored endpoint")
	}
}
