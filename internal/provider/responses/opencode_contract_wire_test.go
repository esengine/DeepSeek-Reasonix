package responses

import (
	"bytes"
	"encoding/json"
	"reasonix/internal/provider"
	"testing"
)

func TestOpenCodeGoDeepSeekWireKeepsReasoningAndStableHistory(t *testing.T) {
	for _, model := range []string{"deepseek-v4-flash", "deepseek-v4-pro", "deepseek-v4-flash-vision-exp"} {
		c := New(Config{BaseURL: "https://opencode.ai/zen/go/v1", RequestURL: "https://opencode.ai/zen/go/v1/responses", Model: model, Effort: "max", Mode: "stateless"}).(*client)
		req := provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "fixture"}, {Role: provider.RoleAssistant, ReasoningContent: "provider-reasoning", ToolCalls: []provider.ToolCall{{ID: "call-1", Name: "read_file", Arguments: "{}"}}}, {Role: provider.RoleTool, ToolCallID: "call-1", Content: "tool-result"}}, MaxTokens: 128}
		a, _, _ := c.buildRequestBody(req)
		b, _, _ := c.buildRequestBody(req)
		first, _ := json.Marshal(a)
		second, _ := json.Marshal(b)
		if !bytes.Equal(first, second) || !bytes.Contains(first, []byte(`"effort":"max"`)) || !bytes.Contains(first, []byte(`provider-reasoning`)) {
			t.Fatalf("%s lost effort/replay/cache stability: %s", model, first)
		}
	}
}
