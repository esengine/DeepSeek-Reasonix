package openai

import (
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/contract/provider"
)

func relayClient(t *testing.T, extra map[string]any) *client {
	t.Helper()
	p, err := New(provider.Config{
		Name:    "relay",
		BaseURL: "https://relay.example.com/v1",
		Model:   "deepseek-v3.2",
		APIKey:  "test",
		Extra:   extra,
	})
	if err != nil {
		t.Fatalf("New relay: %v", err)
	}
	c, ok := p.(*client)
	if !ok {
		t.Fatalf("New returned %T, want *client", p)
	}
	return c
}

func toolCallTurn(reasoning string) []provider.Message {
	return []provider.Message{
		{Role: provider.RoleUser, Content: "count the go files"},
		{
			Role:             provider.RoleAssistant,
			ReasoningContent: reasoning,
			ToolCalls:        []provider.ToolCall{{ID: "c1", Name: "bash", Arguments: `{"command":"ls"}`}},
		},
		{Role: provider.RoleTool, Content: "14", ToolCallID: "c1", Name: "bash"},
	}
}

// A relay is undeclared by construction: its host is nobody's vendor and its
// model id is a name the operator chose. With no protocol declared the
// thinking round-trip does not happen.
func TestRelayWithoutDeclaredProtocolDropsReasoning(t *testing.T) {
	c := relayClient(t, nil)
	req := c.buildRequest(provider.Request{Messages: toolCallTurn("CHAIN-OF-THOUGHT")})

	body, err := json.Marshal(req.Messages)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(body), "reasoning_content") {
		t.Errorf("an undeclared endpoint must not be sent reasoning_content: %s", body)
	}
}

// Declaring the protocol is the whole fix on the user's side, so the declared
// relay must produce the same bytes the official host gets.
func TestRelayWithDeclaredDeepSeekProtocolReplaysReasoning(t *testing.T) {
	c := relayClient(t, map[string]any{"reasoning_protocol": "deepseek"})
	req := c.buildRequest(provider.Request{Messages: toolCallTurn("CHAIN-OF-THOUGHT")})

	body, err := json.Marshal(req.Messages)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(body), "CHAIN-OF-THOUGHT") {
		t.Errorf("declared DeepSeek relay must round-trip reasoning_content: %s", body)
	}
}
