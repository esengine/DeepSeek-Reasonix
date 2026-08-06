package anthropic

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/provider"
)

func TestBuildRequestExcludesToolExecution(t *testing.T) {
	code := 1
	msgs := provider.ModelMessages([]provider.Message{
		{Role: provider.RoleUser, Content: "run tests"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{
			{ID: "call_1", Name: "bash", Arguments: `{"command":"go test ./..."}`},
		}},
		{
			Role: provider.RoleTool, ToolCallID: "call_1", Name: "bash", Content: "FAIL",
			ToolExecution: &provider.ToolExecution{
				Kind: "shell", Shell: "bash", State: "failed", ExitCode: &code,
				FailurePhase: "execution", OutputTail: "中文stderr-marker-must-not-leak",
			},
		},
	})
	req := (&client{model: "claude-test"}).buildRequest(context.Background(), provider.Request{Messages: msgs})
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	for _, banned := range []string{"tool_execution", "outputTail", "中文stderr-marker-must-not-leak", "failurePhase", "mutationRisk"} {
		if strings.Contains(s, banned) {
			t.Fatalf("anthropic wire leaked %q: %s", banned, s)
		}
	}
	if !strings.Contains(s, "tool_result") || !strings.Contains(s, "call_1") {
		t.Fatalf("anthropic tool_result missing: %s", s)
	}
}

func TestBuildRequestStableWhenLocalExecutionAdded(t *testing.T) {
	base := []provider.Message{
		{Role: provider.RoleUser, Content: "run"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "c1", Name: "bash", Arguments: `{"command":"true"}`}}},
		{Role: provider.RoleTool, ToolCallID: "c1", Name: "bash", Content: "ok"},
	}
	code := 0
	withMeta := append([]provider.Message(nil), base...)
	withMeta[2].ToolExecution = &provider.ToolExecution{Kind: "shell", OutputTail: "noise", ExitCode: &code}
	a, err := json.Marshal((&client{model: "claude-test"}).buildRequest(context.Background(), provider.Request{Messages: provider.ModelMessages(base)}))
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal((&client{model: "claude-test"}).buildRequest(context.Background(), provider.Request{Messages: provider.ModelMessages(withMeta)}))
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Fatalf("anthropic request diverged\nbase=%s\nmeta=%s", a, b)
	}
}
