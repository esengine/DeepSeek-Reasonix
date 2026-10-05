package pluginpkg

import (
	"strings"
	"testing"
)

func TestExportMasksEndpointAndArgumentCredentials(t *testing.T) {
	for _, endpoint := range []string{
		"HTTPS://user:fixture-secret@[::1]:8443/mcp",
		"https://host/mcp?%74oken=fixture-secret",
		"https://host/mcp#password=fixture-secret",
		"https://host/mcp?token=fixture-secret;y=1",
		"https://host/mcp?token=fixture-secret%zz",
	} {
		raw := `{"mcpServers":{"neutral":{"url":"` + endpoint + `","command":"node","args":["--passwd","fixture-secret","--token=fixture-secret","` + endpoint + `","--author=neutral"],"env":{"PASSWORD":"fixture-secret"},"headers":{"Authorization":"fixture-secret"}}}}`
		out, _, err := StripCredentials([]byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(out), "fixture-secret") {
			t.Fatalf("export leaked: %s", out)
		}
		if !strings.Contains(string(out), "--author=neutral") {
			t.Fatal("ordinary argument changed")
		}
	}
}
