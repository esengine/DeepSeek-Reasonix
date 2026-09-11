package responses

import (
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/provider"
)

func TestBuildRequestBodyExcludesToolExecution(t *testing.T) {
	code := 1
	msgs := []provider.Message{
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
	}
	// Production Responses path applies ModelMessages inside buildRequestBody.
	bodyMap, _, _ := (&client{model: "gpt-test"}).buildRequestBody(provider.Request{Messages: msgs})
	body, err := json.Marshal(bodyMap)
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	for _, banned := range []string{"tool_execution", "outputTail", "中文stderr-marker-must-not-leak", "failurePhase", "mutationRisk"} {
		if strings.Contains(s, banned) {
			t.Fatalf("responses wire leaked %q: %s", banned, s)
		}
	}
}

func TestBuildRequestBodyStableWhenLocalExecutionAdded(t *testing.T) {
	base := []provider.Message{
		{Role: provider.RoleUser, Content: "run"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "c1", Name: "bash", Arguments: `{"command":"true"}`}}},
		{Role: provider.RoleTool, ToolCallID: "c1", Name: "bash", Content: "ok"},
	}
	code := 0
	withMeta := append([]provider.Message(nil), base...)
	withMeta[2].ToolExecution = &provider.ToolExecution{Kind: "shell", OutputTail: "noise", ExitCode: &code}

	aMap, _, _ := (&client{model: "gpt-test"}).buildRequestBody(provider.Request{Messages: base})
	bMap, _, _ := (&client{model: "gpt-test"}).buildRequestBody(provider.Request{Messages: withMeta})
	a, err := json.Marshal(aMap)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(bMap)
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Fatalf("responses request diverged\nbase=%s\nmeta=%s", a, b)
	}
}

func TestBuildRequestExcludesMCPAppPresentation(t *testing.T) {
	msgs := provider.ModelMessages([]provider.Message{
		{Role: provider.RoleUser, Content: "run app tool"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{
			{ID: "call_app", Name: "mcp__srv__render", Arguments: `{"q":"x"}`},
		}},
		{
			Role: provider.RoleTool, ToolCallID: "call_app", Name: "mcp__srv__render", Content: "rendered",
			MCPApp: &provider.MCPAppPresentation{
				Server: "srv", Tool: "render", Generation: 7,
				ResourceURI: "ui://app/must-not-leak.html",
				RawResult:   json.RawMessage(`{"content":[{"type":"text","text":"mcp-app-marker-must-not-leak"}]}`),
				Structured:  json.RawMessage(`{"secret":"mcp-structured-marker-must-not-leak"}`),
			},
		},
	})
	bodyMap, _, _ := (&client{model: "gpt-x"}).buildRequestBody(provider.Request{Messages: msgs})
	body, err := json.Marshal(bodyMap)
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	for _, banned := range []string{"mcp_app", "resourceUri", "ui://app/must-not-leak", "mcp-app-marker-must-not-leak", "mcp-structured-marker-must-not-leak"} {
		if strings.Contains(s, banned) {
			t.Fatalf("responses wire leaked %q: %s", banned, s)
		}
	}
}
