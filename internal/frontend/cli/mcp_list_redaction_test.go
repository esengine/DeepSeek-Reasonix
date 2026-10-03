package cli

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"

	"reasonix/internal/contract/config"
)

func TestMCPListRedactsRemoteQueryCredentials(t *testing.T) {
	isolateCLIConfigHome(t)
	cfg := config.Default()
	cfg.Plugins = []config.PluginEntry{
		{Name: "global-http", Type: "http", URL: "https://mcp.example.test/Case/MCP?tenant=demo&API_KEY=global%20key&access_token=first-token&access_token=second-token"},
		{Name: "global-sse", Type: "sse", URL: "https://sse.example.test/events?key=sse-key&view=compact", AutoStart: new(false)},
		{Name: "local-process", Command: "demo-mcp", Args: []string{"--path", "docs"}},
	}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatal(err)
	}
	project := []byte(`{"mcpServers":{
		"project-http":{"type":"http","url":"https://project.example.test/mcp?api-key=project%2Bkey&region=west"},
		"plain-http":{"type":"http","url":"https://plain.example.test/Case/MCP?z=last&a=first#anchor"}
	}}`)
	if err := os.WriteFile(".mcp.json", project, 0o600); err != nil {
		t.Fatal(err)
	}
	userBefore, err := os.ReadFile(config.UserConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		name, transport, suffix, url string
	}{
		{"global-http", "http", "", "https://mcp.example.test/Case/MCP?API_KEY=%3Credacted%3E&access_token=%3Credacted%3E&tenant=demo"},
		{"global-sse", "sse", " [auto_start=false]", "https://sse.example.test/events?key=%3Credacted%3E&view=compact"},
		{"project-http", "http", "", "https://project.example.test/mcp?api-key=%3Credacted%3E&region=west"},
		{"plain-http", "http", "", "https://plain.example.test/Case/MCP?z=last&a=first#anchor"},
	}
	t.Run("get", func(t *testing.T) {
		for _, entry := range want {
			out := captureStdout(t, func() {
				if code := Run([]string{"mcp", "get", entry.name}, "test-version"); code != 0 {
					t.Fatalf("mcp get %s exit = %d", entry.name, code)
				}
			})
			if !strings.Contains(out, "url: "+entry.url+"\n") {
				t.Errorf("mcp get %s has incorrect endpoint display:\n%s", entry.name, out)
			}
		}
	})
	for _, verb := range []string{"list", "ls"} {
		t.Run(verb, func(t *testing.T) {
			out := captureStdout(t, func() {
				if code := Run([]string{"mcp", verb}, "test-version"); code != 0 {
					t.Fatalf("mcp %s exit = %d", verb, code)
				}
			})
			for _, entry := range want {
				line := fmt.Sprintf("%-16s (%s)%s  %s\n", entry.name, entry.transport, entry.suffix, entry.url)
				if !strings.Contains(out, line) {
					t.Errorf("mcp %s missing expected row for %s:\n%s", verb, entry.name, out)
				}
			}
			if !strings.Contains(out, fmt.Sprintf("%-16s (stdio)  demo-mcp --path docs\n", "local-process")) || strings.Count(out, "\n") != 5 {
				t.Errorf("mcp %s changed the configured server inventory:\n%s", verb, out)
			}
			for _, secret := range []string{"global%20key", "global key", "first-token", "second-token", "sse-key", "project%2Bkey", "project+key"} {
				if strings.Contains(out, secret) {
					t.Errorf("mcp %s exposed fixture credential %q", verb, secret)
				}
			}
		})
	}
	userAfter, err := os.ReadFile(config.UserConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	projectAfter, err := os.ReadFile(".mcp.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(userAfter, userBefore) || !bytes.Equal(projectAfter, project) {
		t.Fatal("displaying MCP configuration rewrote its source")
	}
}
