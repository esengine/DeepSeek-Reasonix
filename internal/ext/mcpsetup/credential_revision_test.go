package mcpsetup

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRevisionDerivedNameExcludesCredentials(t *testing.T) {
	draft, err := Parse("node --header 'Authorization: Bearer fixturesecret'")
	if err != nil {
		t.Fatal(err)
	}
	if draft.Entries[0].Args[1] != "Authorization: Bearer fixturesecret" {
		t.Fatal("operational argument changed")
	}
	b, _ := json.Marshal(draft.Risks)
	if strings.Contains(draft.Entries[0].Name, "fixturesecret") || strings.Contains(string(b), "fixturesecret") {
		t.Fatalf("derived display leaked: %s %s", draft.Entries[0].Name, b)
	}
}

func TestRevisionDerivedNameExcludesOpaqueCarrierValues(t *testing.T) {
	for _, command := range []string{"node", "npx", "python", "uvx"} {
		if got := NameFromArgv(command, []string{"--header", "X-Custom: fixturesecret"}); got != "mcp-server" {
			t.Errorf("carrier became name: %s", got)
		}
	}
}

func TestRevisionParseErrorMasksHeader(t *testing.T) {
	_, err := Parse("reasonix mcp add neutral --http https://host/mcp --header 'Authorization: Bearer fixturesecret'")
	if err == nil || strings.Contains(err.Error(), "fixturesecret") {
		t.Fatalf("parse error leaked: %v", err)
	}
}
