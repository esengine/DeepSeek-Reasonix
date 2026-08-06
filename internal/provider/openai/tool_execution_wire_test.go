package openai

import (
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/provider"
)

func TestBuildRequestExcludesToolExecution(t *testing.T) {
	code := 1
	// Agent applies ModelMessages before Stream; mirror that order.
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
				MutationRisk: "may_be_partial", Verification: "failed",
			},
		},
	})
	// Also prove Sanitize-only path (without ModelMessages) still cannot emit
	// local fields because chatMessage is a closed wire struct.
	withMeta := []provider.Message{
		{Role: provider.RoleUser, Content: "hi"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "c2", Name: "bash", Arguments: `{"command":"false"}`}}},
		{Role: provider.RoleTool, ToolCallID: "c2", Name: "bash", Content: "x",
			ToolExecution: &provider.ToolExecution{Kind: "shell", OutputTail: "中文stderr-marker-must-not-leak"}},
	}

	for _, tc := range []struct {
		name string
		msgs []provider.Message
	}{
		{name: "after ModelMessages", msgs: msgs},
		{name: "raw with metadata", msgs: withMeta},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := (&client{model: "deepseek-v4"}).buildRequest(provider.Request{Messages: tc.msgs})
			body, err := json.Marshal(req)
			if err != nil {
				t.Fatal(err)
			}
			s := string(body)
			for _, banned := range []string{"tool_execution", "outputTail", "中文stderr-marker-must-not-leak", "failurePhase", "mutationRisk"} {
				if strings.Contains(s, banned) {
					t.Fatalf("openai wire leaked %q: %s", banned, s)
				}
			}
			if !strings.Contains(s, `"role":"tool"`) {
				t.Fatalf("missing tool role: %s", s)
			}
		})
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
	withMeta[2].ToolExecution = &provider.ToolExecution{
		Kind: "shell", Shell: "bash", State: "completed", ExitCode: &code, OutputTail: "noise",
	}
	// Same ModelMessages strip the agent uses.
	a, err := json.Marshal((&client{model: "deepseek-v4"}).buildRequest(provider.Request{Messages: provider.ModelMessages(base)}))
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal((&client{model: "deepseek-v4"}).buildRequest(provider.Request{Messages: provider.ModelMessages(withMeta)}))
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Fatalf("openai request diverged after local execution metadata\nbase=%s\nmeta=%s", a, b)
	}
}
