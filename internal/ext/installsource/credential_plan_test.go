package installsource

import (
	"strings"
	"testing"

	"reasonix/internal/contract/config"
)

func TestMCPPlanKeepsCredentialsOnlyInOperationalEntry(t *testing.T) {
	raw := "https://user:fixture-secret@host/mcp?%74oken=fixture-secret"
	task := &Tool{}
	a := task.mcpEntryAction(request{}, config.PluginEntry{Name: "neutral", URL: raw, Headers: map[string]string{"Authorization": "fixture-secret"}, Env: map[string]string{"PASSWORD": "fixture-secret"}, Args: []string{"--password", "fixture-secret"}}, raw)
	if a.entry.URL != raw || a.entry.Headers["Authorization"] != "fixture-secret" {
		t.Fatal("operational entry changed")
	}
	if got := marshalJSON(publicActions([]action{a})); strings.Contains(got, "fixture-secret") {
		t.Fatalf("plan leaked: %s", got)
	}
}
