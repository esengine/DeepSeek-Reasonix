package provider

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestToolExecutionDoesNotAffectProviderVisibleBytes proves that attaching
// local shell execution metadata to a tool result leaves ModelMessages and
// therefore provider-visible conversation bytes unchanged.
func TestToolExecutionDoesNotAffectProviderVisibleBytes(t *testing.T) {
	base := []Message{
		{Role: RoleUser, Content: "run tests"},
		{Role: RoleAssistant, Content: "", ToolCalls: []ToolCall{{ID: "c1", Name: "bash", Arguments: `{"command":"go test ./..."}`}}},
		{Role: RoleTool, ToolCallID: "c1", Name: "bash", Content: "ok"},
	}
	code := 0
	withMeta := append([]Message(nil), base...)
	withMeta[2].ToolExecution = &ToolExecution{
		Kind: "shell", Shell: "bash", State: "completed", ExitCode: &code,
		MutationRisk: "none", Verification: "passed", OutputTail: "noise",
	}

	visBase, err := json.Marshal(ModelMessages(base))
	if err != nil {
		t.Fatal(err)
	}
	visMeta, err := json.Marshal(ModelMessages(withMeta))
	if err != nil {
		t.Fatal(err)
	}
	if string(visBase) != string(visMeta) {
		t.Fatalf("provider-visible messages diverged after local execution metadata\nbase: %s\nmeta: %s", visBase, visMeta)
	}
	if strings.Contains(string(visMeta), "tool_execution") || strings.Contains(string(visMeta), "outputTail") {
		t.Fatalf("local execution field leaked into provider-visible JSON: %s", visMeta)
	}
}
