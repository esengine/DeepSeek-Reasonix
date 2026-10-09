package pluginpkg

import (
	"strings"
	"testing"
)

func TestRevisionExportCommandCredentials(t *testing.T) {
	for _, command := range []string{"node --header 'Authorization: Bearer fixturesecret'", "node --env 'KEY=fixturesecret'"} {
		out, _, err := StripCredentials([]byte(`{"mcpServers":{"neutral":{"command":"` + command + `"}}}`))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(out), "fixturesecret") {
			t.Fatalf("command leaked: %s", out)
		}
	}
}
