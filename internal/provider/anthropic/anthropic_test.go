package anthropic

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"reasonix/internal/provider"
)

// TestBuildRequest covers the protocol conversion: system lift, tool_use /
// tool_result blocks, coalescing consecutive tool results into one user turn,
// cache_control placement, and the max_tokens fallback.
func TestBuildRequest(t *testing.T) {
	c := &client{name: "anthropic", model: "claude-opus-4-8"}
	req := provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleSystem, Content: "You are helpful."},
			{Role: provider.RoleUser, Content: "weather in Paris and Berlin?"},
			{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{
				{ID: "t1", Name: "get_weather", Arguments: `{"city":"Paris"}`},
				{ID: "t2", Name: "get_weather", Arguments: `{"city":"Berlin"}`},
			}},
			{Role: provider.RoleTool, ToolCallID: "t1", Content: "sunny"},
			{Role: provider.RoleTool, ToolCallID: "t2", Content: "cloudy"},
		},
		Tools: []provider.ToolSchema{{Name: "get_weather", Description: "w", Parameters: json.RawMessage(`{"type":"object"}`)}},
	}
	r := c.buildRequest(req)

	if r.Model != "claude-opus-4-8" {
		t.Fatalf("model = %q", r.Model)
	}
	if r.MaxTokens != defaultMaxTokens {
		t.Fatalf("max_tokens = %d, want default %d", r.MaxTokens, defaultMaxTokens)
	}
	// System lifted to the top level, with a cache breakpoint on its last block.
	if len(r.System) != 1 || r.System[0].Text != "You are helpful." {
		t.Fatalf("system = %+v", r.System)
	}
	if r.System[0].CacheControl == nil {
		t.Fatal("system block should carry cache_control")
	}
	// System present ⇒ the tool does NOT also get a breakpoint (system caches tools).
	if r.Tools[0].CacheControl != nil {
		t.Fatal("tool should not carry cache_control when system does")
	}
	// user, assistant(tool_use ×2), user(tool_result ×2 coalesced) = 3 messages.
	if len(r.Messages) != 3 {
		t.Fatalf("want 3 messages, got %d: %+v", len(r.Messages), r.Messages)
	}
	if r.Messages[0].Role != "user" || r.Messages[0].Content[0].Text != "weather in Paris and Berlin?" {
		t.Fatalf("msg[0] = %+v", r.Messages[0])
	}
	if r.Messages[1].Role != "assistant" || len(r.Messages[1].Content) != 2 ||
		r.Messages[1].Content[0].Type != "tool_use" || r.Messages[1].Content[0].ID != "t1" ||
		string(r.Messages[1].Content[0].Input) != `{"city":"Paris"}` {
		t.Fatalf("msg[1] = %+v", r.Messages[1])
	}
	last := r.Messages[2]
	if last.Role != "user" || len(last.Content) != 2 {
		t.Fatalf("tool results should coalesce into one user turn: %+v", last)
	}
	if last.Content[0].Type != "tool_result" || last.Content[0].ToolUseID != "t1" || last.Content[0].Content != "sunny" {
		t.Fatalf("tool_result[0] = %+v", last.Content[0])
	}
	if last.Content[1].ToolUseID != "t2" {
		t.Fatalf("tool_result[1] = %+v", last.Content[1])
	}
	// Conversation cache breakpoint on the last block of the last message.
	if last.Content[len(last.Content)-1].CacheControl == nil {
		t.Fatal("last message block should carry cache_control")
	}
}

// TestBuildRequestNoSystem checks the breakpoint falls back to the last tool when
// there is no system message.
func TestBuildRequestNoSystem(t *testing.T) {
	c := &client{model: "claude-opus-4-8"}
	r := c.buildRequest(provider.Request{
		Messages:  []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
		Tools:     []provider.ToolSchema{{Name: "a"}, {Name: "b"}},
		MaxTokens: 1000,
	})
	if r.MaxTokens != 1000 {
		t.Fatalf("explicit max_tokens should win: %d", r.MaxTokens)
	}
	if r.Tools[1].CacheControl == nil {
		t.Fatal("last tool should carry cache_control when there is no system")
	}
	// A tool with no schema gets a minimal valid object schema.
	if string(r.Tools[0].InputSchema) != `{"type":"object","properties":{}}` {
		t.Fatalf("empty schema not defaulted: %s", r.Tools[0].InputSchema)
	}
}

func TestMapStopReason(t *testing.T) {
	cases := map[string]string{
		"end_turn":      "stop",
		"stop_sequence": "stop",
		"tool_use":      "tool_calls",
		"max_tokens":    "length",
		"refusal":       "refusal",
		"":              "",
	}
	for in, want := range cases {
		if got := mapStopReason(in); got != want {
			t.Errorf("mapStopReason(%q) = %q, want %q", in, got, want)
		}
	}
}

const sseFixture = `event: message_start
data: {"type":"message_start","message":{"usage":{"input_tokens":100,"cache_creation_input_tokens":0,"cache_read_input_tokens":50,"output_tokens":1}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" world"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: content_block_start
data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_1","name":"get_weather"}}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"city\":"}}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"\"Paris\"}"}}

event: content_block_stop
data: {"type":"content_block_stop","index":1}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":25}}

event: message_stop
data: {"type":"message_stop"}
`

// TestReadStream feeds a canned Messages API SSE stream through readStream and
// asserts the emitted chunk sequence: text deltas, a tool-call start + complete,
// a usage record, then done.
func TestReadStream(t *testing.T) {
	c := &client{name: "anthropic"}
	resp := &http.Response{Body: io.NopCloser(strings.NewReader(sseFixture))}
	ch := make(chan provider.Chunk)
	go c.readStream(context.Background(), resp, ch)

	var text strings.Builder
	var started, full *provider.ToolCall
	var usage *provider.Usage
	done := false
	for ck := range ch {
		switch ck.Type {
		case provider.ChunkText:
			text.WriteString(ck.Text)
		case provider.ChunkToolCallStart:
			started = ck.ToolCall
		case provider.ChunkToolCall:
			full = ck.ToolCall
		case provider.ChunkUsage:
			usage = ck.Usage
		case provider.ChunkDone:
			done = true
		case provider.ChunkError:
			t.Fatalf("unexpected error chunk: %v", ck.Err)
		}
	}

	if text.String() != "Hello world" {
		t.Fatalf("text = %q", text.String())
	}
	if started == nil || started.ID != "toolu_1" || started.Name != "get_weather" {
		t.Fatalf("tool start = %+v", started)
	}
	if full == nil || full.Arguments != `{"city":"Paris"}` {
		t.Fatalf("tool full = %+v", full)
	}
	switch {
	case usage == nil:
		t.Fatal("expected a usage chunk")
	case usage.PromptTokens != 150 || usage.CompletionTokens != 25 || usage.TotalTokens != 175:
		t.Fatalf("usage tokens = %+v", usage)
	case usage.CacheHitTokens != 50 || usage.CacheMissTokens != 100:
		t.Fatalf("usage cache = hit %d miss %d", usage.CacheHitTokens, usage.CacheMissTokens)
	case usage.FinishReason != "tool_calls":
		t.Fatalf("finish reason = %q", usage.FinishReason)
	}
	if !done {
		t.Fatal("expected a done chunk")
	}
}

// TestReadStreamError surfaces a mid-stream error event as a ChunkError.
func TestReadStreamError(t *testing.T) {
	sse := "event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"overloaded\"}}\n\n"
	c := &client{name: "anthropic"}
	resp := &http.Response{Body: io.NopCloser(strings.NewReader(sse))}
	ch := make(chan provider.Chunk)
	go c.readStream(context.Background(), resp, ch)

	var gotErr error
	for ck := range ch {
		if ck.Type == provider.ChunkError {
			gotErr = ck.Err
		}
	}
	if gotErr == nil || !strings.Contains(gotErr.Error(), "overloaded") {
		t.Fatalf("expected an error chunk mentioning overloaded, got %v", gotErr)
	}
}

// TestBuildRequestThinking checks that, with thinking enabled, the request carries
// the adaptive thinking + effort config and the prior assistant turn's signed
// thinking block is replayed first (before its tool_use).
func TestBuildRequestThinking(t *testing.T) {
	c := &client{model: "claude-opus-4-8", thinking: "adaptive", effort: "high"}
	r := c.buildRequest(provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: "weather?"},
			{Role: provider.RoleAssistant, ReasoningContent: "Let me check.", ReasoningSignature: "sig-abc",
				ToolCalls: []provider.ToolCall{{ID: "t1", Name: "get_weather", Arguments: `{"city":"Paris"}`}}},
			{Role: provider.RoleTool, ToolCallID: "t1", Content: "sunny"},
		},
	})
	if r.Thinking == nil || r.Thinking.Type != "adaptive" || r.Thinking.Display != "summarized" {
		t.Fatalf("thinking config = %+v", r.Thinking)
	}
	if r.OutputConfig == nil || r.OutputConfig.Effort != "high" {
		t.Fatalf("output config = %+v", r.OutputConfig)
	}
	asst := r.Messages[1]
	if asst.Role != "assistant" || len(asst.Content) != 2 {
		t.Fatalf("assistant msg = %+v", asst)
	}
	if asst.Content[0].Type != "thinking" || asst.Content[0].Thinking != "Let me check." || asst.Content[0].Signature != "sig-abc" {
		t.Fatalf("first block should be the signed thinking block: %+v", asst.Content[0])
	}
	if asst.Content[1].Type != "tool_use" {
		t.Fatalf("tool_use should follow the thinking block: %+v", asst.Content[1])
	}
}

func TestBuildRequestThinkingEnabledGateway(t *testing.T) {
	c := &client{model: "LongCat-2.0", thinking: "enabled", effort: "disabled"}
	r := c.buildRequest(provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: "hi"},
			{Role: provider.RoleAssistant, Content: "ok", ReasoningContent: "signed reasoning", ReasoningSignature: "sig"},
		},
	})
	if r.Thinking == nil || r.Thinking.Type != "disabled" || r.Thinking.Display != "" {
		t.Fatalf("thinking config = %+v, want disabled without display", r.Thinking)
	}
	if r.OutputConfig != nil {
		t.Fatalf("enabled/disabled gateway thinking must omit output_config: %+v", r.OutputConfig)
	}
	for _, block := range r.Messages[1].Content {
		if block.Type == "thinking" {
			t.Fatalf("enabled/disabled gateway must not replay Anthropic signed thinking blocks: %+v", r.Messages[1])
		}
	}
}

// TestBuildRequestThinkingOff is the default: no thinking field, and reasoning is
// NOT replayed (even with a signature present) since the model wasn't asked to think.
func TestBuildRequestThinkingOff(t *testing.T) {
	c := &client{model: "claude-opus-4-8"}
	r := c.buildRequest(provider.Request{Messages: []provider.Message{
		{Role: provider.RoleUser, Content: "hi"},
		{Role: provider.RoleAssistant, Content: "ok", ReasoningContent: "x", ReasoningSignature: "sig"},
	}})
	if r.Thinking != nil || r.OutputConfig != nil {
		t.Fatalf("thinking should be off by default: %+v / %+v", r.Thinking, r.OutputConfig)
	}
	for _, b := range r.Messages[1].Content {
		if b.Type == "thinking" {
			t.Fatal("thinking block must not be replayed when thinking is off")
		}
	}
}

func TestBuildRequestDropsMemoryCitations(t *testing.T) {
	c := &client{model: "claude-opus-4-8"}
	r := c.buildRequest(provider.Request{Messages: []provider.Message{
		{Role: provider.RoleUser, Content: "continue"},
		{Role: provider.RoleUser, Content: "edited prompt", Edited: true, Original: "original prompt"},
		{Role: provider.RoleAssistant, Content: "done", MemoryCitations: []provider.MemoryCitation{{
			ID: "mem-1", Source: "MEMORY.md", LineStart: 116, LineEnd: 123, Note: "workflow",
		}}},
	}})
	b, err := json.Marshal(r.Messages)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(b), "memoryCitations") || strings.Contains(string(b), "MEMORY.md") {
		t.Fatalf("local memory citations leaked into Anthropic request: %s", b)
	}
	if strings.Contains(string(b), "original prompt") || strings.Contains(string(b), `"edited"`) || strings.Contains(string(b), `"original"`) {
		t.Fatalf("local edit metadata leaked into Anthropic request: %s", b)
	}
	if !strings.Contains(string(b), "done") {
		t.Fatalf("assistant content was dropped with local metadata: %s", b)
	}
}

const sseThinking = `event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"Let me "}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"think."}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"SIG123"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"Hi"}}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":10}}

event: message_stop
data: {"type":"message_stop"}
`

// TestReadStreamThinking checks thinking_delta streams as reasoning text and
// signature_delta carries the signature back on a ChunkReasoning.
func TestReadStreamThinking(t *testing.T) {
	c := &client{name: "anthropic"}
	resp := &http.Response{Body: io.NopCloser(strings.NewReader(sseThinking))}
	ch := make(chan provider.Chunk)
	go c.readStream(context.Background(), resp, ch)

	var reasoning, text strings.Builder
	var sig string
	for ck := range ch {
		switch ck.Type {
		case provider.ChunkReasoning:
			reasoning.WriteString(ck.Text)
			if ck.Signature != "" {
				sig = ck.Signature
			}
		case provider.ChunkText:
			text.WriteString(ck.Text)
		}
	}
	if reasoning.String() != "Let me think." {
		t.Fatalf("reasoning = %q", reasoning.String())
	}
	if sig != "SIG123" {
		t.Fatalf("signature = %q", sig)
	}
	if text.String() != "Hi" {
		t.Fatalf("text = %q", text.String())
	}
}

// TestBaseURLNormalizedForV1Messages checks the URL-rewriting step in New().
// Anthropic's Messages endpoint is {root}/v1/messages, but the setup wizard
// accepts OpenAI-style URLs (e.g. "https://proxy.example.com/v1") because
// /models probes expect that shape. Without the strip, the chat client would
// concatenate /v1/messages onto an already-versioned root and the request
// would go to https://proxy.example.com/v1/v1/messages — failing 404.
func TestBaseURLNormalizedForV1Messages(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain root (no /v1)", "https://api.anthropic.com", "https://api.anthropic.com"},
		{"versioned v1 (OpenAI shape)", "https://proxy.example.com/v1", "https://proxy.example.com"},
		{"versioned v1 with trailing slash", "https://proxy.example.com/v1/", "https://proxy.example.com"},
		{"versioned v1 with path prefix", "https://gateway.example.com/api/v1", "https://gateway.example.com/api"},
		{"trailing slash only", "https://api.anthropic.com/", "https://api.anthropic.com"},
		{"empty falls back to default", "", "https://api.anthropic.com"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, err := New(provider.Config{
				Name:    "test",
				Model:   "claude-opus-4-8",
				BaseURL: tc.in,
			})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			c, ok := p.(*client)
			if !ok {
				t.Fatalf("provider type = %T, want *client", p)
			}
			if c.baseURL != tc.want {
				t.Errorf("baseURL = %q, want %q", c.baseURL, tc.want)
			}
		})
	}
}

func TestStreamSupportsBearerAuthHeaderAndCustomHeaders(t *testing.T) {
	var gotAuth, gotAPIKey, gotVersion, gotUserAgent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("path = %q, want /v1/messages", r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		gotAPIKey = r.Header.Get("x-api-key")
		gotVersion = r.Header.Get("anthropic-version")
		gotUserAgent = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	}))
	defer srv.Close()

	p, err := New(provider.Config{
		Name:    "gateway",
		BaseURL: srv.URL,
		Model:   "claude-sonnet-4-6",
		APIKey:  "sk-test",
		Extra: map[string]any{
			"auth_header": true,
			"headers": map[string]string{
				"User-Agent":        "Reasonix",
				"Authorization":     "Bearer wrong",
				"x-api-key":         "wrong",
				"anthropic-version": "bad",
			},
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ch, err := p.Stream(context.Background(), provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	for range ch {
	}

	if gotAuth != "Bearer sk-test" {
		t.Fatalf("Authorization = %q, want Bearer sk-test", gotAuth)
	}
	if gotAPIKey != "" {
		t.Fatalf("x-api-key = %q, want omitted", gotAPIKey)
	}
	if gotVersion != anthropicVersion {
		t.Fatalf("anthropic-version = %q, want %q", gotVersion, anthropicVersion)
	}
	if gotUserAgent != "Reasonix" {
		t.Fatalf("User-Agent = %q, want Reasonix", gotUserAgent)
	}
}

// Ensure the package wires into the registry under the expected kind.
func TestRegistered(t *testing.T) {
	p, err := provider.New("anthropic", provider.Config{Model: "claude-opus-4-8", Name: "claude"})
	if err != nil {
		t.Fatalf("provider.New: %v", err)
	}
	if p.Name() != "claude" {
		t.Fatalf("name = %q", p.Name())
	}
	// Missing model is rejected.
	if _, err := provider.New("anthropic", provider.Config{}); err == nil {
		t.Fatal("expected error for missing model")
	}
	_ = context.Background()
}
