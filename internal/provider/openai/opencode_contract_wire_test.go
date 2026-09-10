package openai

import (
	"bytes"
	"encoding/json"
	"reasonix/internal/provider"
	"testing"
)

func TestOpenCodeGoDeepSeekWireKeepsReasoningAndStableHistory(t *testing.T) {
	for _, model := range []string{"deepseek-v4-flash", "deepseek-v4-pro", "deepseek-v4-flash-vision-exp"} {
		p, err := New(provider.Config{BaseURL: "https://opencode.ai/zen/go/v1", Model: model, Extra: map[string]any{"request_url": "https://opencode.ai/zen/go/v1/chat/completions", "thinking": "enabled", "effort": "max"}})
		if err != nil {
			t.Fatal(err)
		}
		c := p.(*client)
		req := provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "fixture"}, {Role: provider.RoleAssistant, ReasoningContent: "provider-reasoning", ToolCalls: []provider.ToolCall{{ID: "call-1", Name: "read_file", Arguments: "{}"}}}, {Role: provider.RoleTool, ToolCallID: "call-1", Content: "tool-result"}}, MaxTokens: 128}
		body := c.buildRequest(req)
		first, _ := json.Marshal(body)
		second, _ := json.Marshal(c.buildRequest(req))
		if !bytes.Equal(first, second) || !bytes.Contains(first, []byte(`"reasoning_effort":"max"`)) || !bytes.Contains(first, []byte(`"reasoning_content":"provider-reasoning"`)) {
			t.Fatalf("%s lost effort/replay/cache stability: %s", model, first)
		}
	}
}
