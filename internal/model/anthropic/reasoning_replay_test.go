package anthropic

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"reasonix/internal/contract/provider"
)

func relayClient(t *testing.T, base string, extra map[string]any) *client {
	t.Helper()
	p, err := New(provider.Config{
		Name:    "relay",
		BaseURL: base,
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

func replayTurn() provider.Request {
	return provider.Request{Messages: []provider.Message{
		{Role: provider.RoleUser, Content: "weather?"},
		{
			Role:             provider.RoleAssistant,
			ReasoningContent: "I should call the tool.",
			ToolCalls:        []provider.ToolCall{{ID: "t1", Name: "get_weather", Arguments: `{"city":"Paris"}`}},
		},
		{Role: provider.RoleTool, ToolCallID: "t1", Content: "sunny"},
	}}
}

func assistantBlocks(t *testing.T, r anthRequest) []contentBlock {
	t.Helper()
	if len(r.Messages) < 2 {
		t.Fatalf("messages = %+v, want a replayed assistant turn", r.Messages)
	}
	return r.Messages[1].Content
}

// The contract this endpoint speaks was readable only off the host, so a relay
// carrying the same models had no way to ask for the unsigned thinking block it
// requires — and no setting could rescue it.
func TestDeclaredDeepSeekRelayReplaysUnsignedThinking(t *testing.T) {
	c := relayClient(t, "https://relay.example.com", map[string]any{"reasoning_protocol": "deepseek"})
	blocks := assistantBlocks(t, c.buildRequest(context.Background(), replayTurn()))

	if len(blocks) != 2 || blocks[0].Type != "thinking" || blocks[0].Thinking != "I should call the tool." {
		t.Fatalf("assistant blocks = %+v, want thinking before tool_use", blocks)
	}
	if blocks[0].Signature != "" {
		t.Errorf("DeepSeek issues no signature; sending one invents proof: %+v", blocks[0])
	}
}

func TestUndeclaredRelayDropsThinkingAndNamesIt(t *testing.T) {
	c := relayClient(t, "https://relay.example.com", nil)
	r := c.buildRequest(context.Background(), replayTurn())

	for _, b := range assistantBlocks(t, r) {
		if b.Type == "thinking" {
			t.Fatalf("an undeclared endpoint must not be sent thinking: %+v", b)
		}
	}
	if r.reasoningHint != provider.HintDroppedToolCallReasoning {
		t.Errorf("hint = %q, want %q", r.reasoningHint, provider.HintDroppedToolCallReasoning)
	}
}

// On Anthropic proper unsigned reasoning is unsendable rather than withheld:
// the endpoint requires a signature it alone issues. Reporting it as a
// declaration the user forgot would send them to a setting that cannot help.
func TestNativeAnthropicUnsignedReasoningIsNotReported(t *testing.T) {
	c := relayClient(t, "https://api.anthropic.com", map[string]any{"thinking": "adaptive"})
	if hint := c.buildRequest(context.Background(), replayTurn()).reasoningHint; hint != "" {
		t.Errorf("hint = %q, want none", hint)
	}
}

// deepSeekReplayServer answers like DeepSeek's Anthropic endpoint in thinking
// mode: an assistant tool_use turn without a thinking block carrying the
// "thinking" field is refused; anything else completes.
func deepSeekReplayServer(t *testing.T, bodies *[]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		*bodies = append(*bodies, string(raw))
		var req struct {
			Messages []struct {
				Role    string                       `json:"role"`
				Content []map[string]json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		if err := json.Unmarshal(raw, &req); err != nil {
			t.Errorf("request body: %v", err)
		}
		for _, m := range req.Messages {
			if m.Role != "assistant" {
				continue
			}
			hasToolUse, hasThinking := false, false
			for _, b := range m.Content {
				var typ string
				_ = json.Unmarshal(b["type"], &typ)
				_, field := b["thinking"]
				hasToolUse = hasToolUse || typ == "tool_use"
				hasThinking = hasThinking || (typ == "thinking" && field)
			}
			if hasToolUse && !hasThinking {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, `{"error":{"message":"The `+"`content[].thinking`"+` in the thinking mode must be passed back to the API.","type":"invalid_request_error"}}`)
				return
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":2,\"output_tokens\":0}}}\n\n"+
			"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\n"+
			"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	}))
}

// A tool-call turn whose reasoning never arrived (the missing-reasoning
// fallback commits it that way) is still history DeepSeek will be sent on
// every later request of the session.
func TestDeepSeekToolCallTurnWithoutReasoningStillReplaysThinking(t *testing.T) {
	var bodies []string
	srv := deepSeekReplayServer(t, &bodies)
	defer srv.Close()

	c := relayClient(t, srv.URL, map[string]any{"reasoning_protocol": "deepseek"})
	req := replayTurn()
	req.Messages[1].ReasoningContent = ""
	req.Tools = []provider.ToolSchema{{Name: "get_weather"}}

	ch, err := c.Stream(context.Background(), req)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("stream error: %v", chunk.Err)
		}
	}
	if len(bodies) != 1 || !strings.Contains(bodies[0], `{"type":"thinking","thinking":""}`) {
		t.Fatalf("request bodies = %q, want an empty thinking block before tool_use", bodies)
	}
}

func TestDeepSeekThinkingOffSendsNoEmptyThinking(t *testing.T) {
	c := relayClient(t, "https://relay.example.com", map[string]any{"reasoning_protocol": "deepseek", "effort": "disabled"})
	req := replayTurn()
	req.Messages[1].ReasoningContent = ""
	for _, b := range assistantBlocks(t, c.buildRequest(context.Background(), req)) {
		if b.Type == "thinking" {
			t.Fatalf("thinking off must keep the turn as sent: %+v", b)
		}
	}
}
