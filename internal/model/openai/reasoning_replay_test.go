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
// thinking round-trip cannot happen, and the request must say which one it is
// so a refusal is not read as an unexplained bug.
func TestRelayWithoutDeclaredProtocolDropsReasoningAndNamesIt(t *testing.T) {
	c := relayClient(t, nil)
	req := c.buildRequest(provider.Request{Messages: toolCallTurn("CHAIN-OF-THOUGHT")})

	body, err := json.Marshal(req.Messages)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(body), "reasoning_content") {
		t.Errorf("an undeclared endpoint must not be sent reasoning_content: %s", body)
	}
	if req.reasoningHint != provider.HintDroppedToolCallReasoning {
		t.Errorf("hint = %q, want %q", req.reasoningHint, provider.HintDroppedToolCallReasoning)
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
	if req.reasoningHint != "" {
		t.Errorf("a request that sent its reasoning left nothing out: %q", req.reasoningHint)
	}
}

// The hint answers "we had it and did not send it". Anything else is a guess
// about someone else's 400, and would send a reader after the wrong field.
func TestNoHintWhenThereWasNoReasoningToDrop(t *testing.T) {
	c := relayClient(t, nil)
	for _, tc := range []struct {
		name string
		msgs []provider.Message
	}{
		{name: "tool call without reasoning", msgs: toolCallTurn("")},
		{name: "reasoning without a tool call", msgs: []provider.Message{
			{Role: provider.RoleUser, Content: "hi"},
			{Role: provider.RoleAssistant, Content: "hello", ReasoningContent: "private scratchpad"},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if hint := c.buildRequest(provider.Request{Messages: tc.msgs}).reasoningHint; hint != "" {
				t.Errorf("hint = %q, want none", hint)
			}
		})
	}
}
