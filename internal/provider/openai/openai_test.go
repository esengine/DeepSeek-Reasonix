package openai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"reasonix/internal/provider"
)

// TestIsRetryableStatus covers the boundary: 408/429/5xx retry, other 4xx don't.
func TestIsRetryableStatus(t *testing.T) {
	retry := []int{408, 429, 500, 502, 503, 504, 599}
	noRetry := []int{200, 400, 401, 403, 404, 422}
	for _, s := range retry {
		if !isRetryableStatus(s) {
			t.Errorf("status %d should be retryable", s)
		}
	}
	for _, s := range noRetry {
		if isRetryableStatus(s) {
			t.Errorf("status %d should not be retryable", s)
		}
	}
}

// TestIsTransientErr keeps user-intent errors (ctx cancel / deadline) out of
// the retry path while letting network-level failures through.
func TestIsTransientErr(t *testing.T) {
	if isTransientErr(nil) {
		t.Error("nil error should not be transient")
	}
	if isTransientErr(context.Canceled) {
		t.Error("ctx canceled should not be transient")
	}
	if isTransientErr(context.DeadlineExceeded) {
		t.Error("ctx deadline should not be transient")
	}
	if !isTransientErr(errors.New("connection reset")) {
		t.Error("generic network-ish error should be transient")
	}
}

// TestStreamAuthError verifies a 401 surfaces as an actionable *provider.AuthError
// (naming the provider and its key env var) rather than a raw status body.
func TestStreamAuthError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"Authentication Fails, Your api key: ****ae54 is invalid"}}`))
	}))
	defer srv.Close()

	p, err := New(provider.Config{
		Name:    "deepseek",
		BaseURL: srv.URL,
		Model:   "deepseek-v4",
		APIKey:  "bad",
		Extra:   map[string]any{"api_key_env": "DEEPSEEK_API_KEY"},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = p.Stream(context.Background(), provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
	})
	var authErr *provider.AuthError
	if !errors.As(err, &authErr) {
		t.Fatalf("want *provider.AuthError, got %T: %v", err, err)
	}
	if authErr.Provider != "deepseek" || authErr.KeyEnv != "DEEPSEEK_API_KEY" || authErr.Status != 401 {
		t.Errorf("AuthError fields wrong: %+v", authErr)
	}
	if msg := authErr.Error(); !strings.Contains(msg, "DEEPSEEK_API_KEY") || strings.Contains(msg, "ae54") {
		t.Errorf("message should name the env var and not dump the raw body: %q", msg)
	}
}

// TestBuildRequestAlwaysSerializesContent guards the DeepSeek 400 regression:
// an assistant turn that is pure tool_calls (no preamble text) has empty
// content, and DeepSeek rejects a message missing the `content` field. Every
// message — including that one — must serialize a content field.
func TestBuildRequestAlwaysSerializesContent(t *testing.T) {
	c := &client{model: "deepseek-v4"}
	req := c.buildRequest(provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: "list the files"},
			// Assistant turn with no text, only a tool call — the offending shape.
			{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{
				{ID: "call_1", Name: "ls", Arguments: `{"path":"."}`},
			}},
			{Role: provider.RoleTool, Content: "main.go", ToolCallID: "call_1", Name: "ls"},
		},
	})

	b, err := json.Marshal(req.Messages)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// Decode generically so we can assert the key's presence (not just its value).
	var raw []map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for i, m := range raw {
		if _, ok := m["content"]; !ok {
			t.Errorf("messages[%d] is missing the content field: %s", i, b)
		}
	}
	// The tool-call-only assistant message must carry content:"" and its tool_calls.
	if got := string(raw[1]["content"]); got != `""` {
		t.Errorf("assistant content = %s, want \"\"", got)
	}
	if _, ok := raw[1]["tool_calls"]; !ok {
		t.Errorf("assistant message lost its tool_calls: %s", b)
	}
}

// TestStreamRepairsDanglingToolCalls reproduces and guards the DeepSeek 400
// "An assistant message with 'tool_calls' must be followed by tool messages
// responding to each 'tool_call_id'". A resumed/interrupted session can carry an
// assistant tool_calls turn whose tool results never landed; the server here
// mimics DeepSeek and rejects any unpaired tool_call with that exact 400, so the
// request must be repaired before it is sent.
func TestStreamRepairsDanglingToolCalls(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []struct {
				Role      string `json:"role"`
				ToolCalls []struct {
					ID string `json:"id"`
				} `json:"tool_calls"`
				ToolCallID string `json:"tool_call_id"`
			} `json:"messages"`
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &req)
		answered := map[string]bool{}
		for _, m := range req.Messages {
			if m.Role == "tool" {
				answered[m.ToolCallID] = true
			}
		}
		for _, m := range req.Messages {
			if m.Role != "assistant" {
				continue
			}
			for _, tc := range m.ToolCalls {
				if !answered[tc.ID] {
					w.WriteHeader(http.StatusBadRequest)
					_, _ = w.Write([]byte(`{"error":{"message":"An assistant message with 'tool_calls' must be followed by tool messages responding to each 'tool_call_id'. (insufficient tool messages following tool_calls message)","type":"invalid_request_error","param":null,"code":"invalid_request_error"}}`))
					return
				}
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"done\"}}]}\n\n")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":1,\"total_tokens\":6}}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	p, err := New(provider.Config{Name: "deepseek-flash", BaseURL: srv.URL, Model: "deepseek-v4", APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// An assistant tool_calls turn whose tool result never landed (an interrupted
	// turn), followed by a fresh user message — the exact shape that 400s.
	ch, err := p.Stream(context.Background(), provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: "list the files"},
			{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{
				{ID: "call_1", Name: "ls", Arguments: `{"path":"."}`},
			}},
			{Role: provider.RoleUser, Content: "never mind, what time is it?"},
		},
	})
	if err != nil {
		t.Fatalf("Stream sent a dangling tool_calls to the API: %v", err)
	}
	var streamErr error
	var text strings.Builder
	for chunk := range ch {
		switch chunk.Type {
		case provider.ChunkText:
			text.WriteString(chunk.Text)
		case provider.ChunkError:
			streamErr = chunk.Err
		}
	}
	if streamErr != nil {
		t.Fatalf("stream errored: %v", streamErr)
	}
	if text.String() != "done" {
		t.Fatalf("completion text = %q, want \"done\"", text.String())
	}
}

// TestNormaliseUsageDeepSeekShape covers DeepSeek's top-level cache fields.
func TestNormaliseUsageDeepSeekShape(t *testing.T) {
	u := normaliseUsage(&wireUsage{
		PromptTokens:          1000,
		CompletionTokens:      200,
		TotalTokens:           1200,
		PromptCacheHitTokens:  900,
		PromptCacheMissTokens: 100,
	})
	if u.CacheHitTokens != 900 || u.CacheMissTokens != 100 {
		t.Errorf("DeepSeek-shape cache fields lost: hit=%d miss=%d", u.CacheHitTokens, u.CacheMissTokens)
	}
}

// TestNormaliseUsageMiMoShape covers the nested prompt_tokens_details /
// completion_tokens_details path used by OpenAI and MiMo. Miss is derived
// from prompt - hit when only hit is provided.
func TestNormaliseUsageMiMoShape(t *testing.T) {
	u := normaliseUsage(&wireUsage{
		PromptTokens:     1000,
		CompletionTokens: 500,
		TotalTokens:      1500,
		PromptTokensDetails: &struct {
			CachedTokens int `json:"cached_tokens"`
		}{CachedTokens: 600},
		CompletionTokensDetails: &struct {
			ReasoningTokens int `json:"reasoning_tokens"`
		}{ReasoningTokens: 180},
	})
	if u.CacheHitTokens != 600 || u.CacheMissTokens != 400 {
		t.Errorf("nested cache normalisation wrong: hit=%d miss=%d (want 600 / 400)", u.CacheHitTokens, u.CacheMissTokens)
	}
	if u.ReasoningTokens != 180 {
		t.Errorf("reasoning tokens lost: %d", u.ReasoningTokens)
	}
}

// TestBuildRequestDropsReasoningContent guards the cache/cost fix: an assistant
// turn's reasoning_content is a response-only signal and must never be echoed
// back in the outgoing request. DeepSeek otherwise counts it as paid prompt
// input (~500 tok/turn on a reasoner chain). The session keeps it for
// display/archive; the wire request must not carry it.
func TestBuildRequestDropsReasoningContent(t *testing.T) {
	c := &client{model: "deepseek-reasoner"}
	req := c.buildRequest(provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: "explain"},
			{Role: provider.RoleAssistant, Content: "the answer", ReasoningContent: "SECRET-CHAIN-OF-THOUGHT"},
			{Role: provider.RoleUser, Content: "thanks"},
		},
	})
	b, err := json.Marshal(req.Messages)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(b), "reasoning_content") {
		t.Errorf("outgoing request must not carry a reasoning_content field: %s", b)
	}
	if strings.Contains(string(b), "SECRET-CHAIN-OF-THOUGHT") {
		t.Errorf("the assistant chain-of-thought leaked into the request: %s", b)
	}
	// The visible answer must survive — we only drop reasoning, not content.
	if !strings.Contains(string(b), "the answer") {
		t.Errorf("assistant content was dropped along with reasoning: %s", b)
	}
}

func TestBuildRequestForwardsReasoningEffort(t *testing.T) {
	c := &client{model: "mimo-v2", effort: "high"}
	if got := c.buildRequest(provider.Request{}).ReasoningEffort; got != "high" {
		t.Errorf("ReasoningEffort = %q, want high", got)
	}

	b, err := json.Marshal((&client{model: "deepseek-v4"}).buildRequest(provider.Request{}))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(b), "reasoning_effort") {
		t.Errorf("empty effort must be omitted from the payload: %s", b)
	}
}

func TestNewReadsEffortFromConfig(t *testing.T) {
	p, err := New(provider.Config{
		Name:    "mimo",
		BaseURL: "https://api.example.com",
		Model:   "mimo-v2",
		Extra:   map[string]any{"effort": "medium"},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := p.(*client).effort; got != "medium" {
		t.Errorf("effort = %q, want medium", got)
	}
}

// TestBuildRequestPreservesEmptyIDToolResults proves a multi-tool turn whose
// calls carry no id (some OpenAI-compatible gateways omit it, sending only the
// index) keeps every tool result through buildRequest. SanitizeToolPairing keys
// on tool_call_id, so empty ids collapse and all but the last result is dropped.
func TestBuildRequestPreservesEmptyIDToolResults(t *testing.T) {
	c := &client{model: "deepseek-v4"}
	req := c.buildRequest(provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: "scan"},
			{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{
				{ID: "", Name: "read_file", Arguments: `{"p":"a"}`},
				{ID: "", Name: "read_file", Arguments: `{"p":"b"}`},
			}},
			{Role: provider.RoleTool, ToolCallID: "", Name: "read_file", Content: "RESULT-A"},
			{Role: provider.RoleTool, ToolCallID: "", Name: "read_file", Content: "RESULT-B"},
		},
	})
	var toolContents []string
	for _, m := range req.Messages {
		if m.Role == string(provider.RoleTool) {
			toolContents = append(toolContents, m.Content)
		}
	}
	if len(toolContents) != 2 {
		t.Fatalf("want 2 tool results in request, got %d: %v", len(toolContents), toolContents)
	}
	if toolContents[0] == toolContents[1] {
		t.Errorf("tool results collapsed to %q — a result was dropped from the model's context", toolContents[0])
	}
}

// TestStreamSynthesizesMissingToolCallIDs covers a gateway that streams tool
// calls by index with no id (vLLM / llama.cpp do this). Each completed call must
// come back with a stable, distinct synthetic id so its result can pair back.
func TestStreamSynthesizesMissingToolCallIDs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"name":"read_file","arguments":"{\"p\":\"a\"}"}}]}}]}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":1,"function":{"name":"read_file","arguments":"{\"p\":\"b\"}"}}]}}]}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":5,"completion_tokens":2,"total_tokens":7}}`+"\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	p, err := New(provider.Config{Name: "local", BaseURL: srv.URL, Model: "qwen", APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ch, err := p.Stream(context.Background(), provider.Request{})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var ids []string
	for chunk := range ch {
		if chunk.Type == provider.ChunkToolCall && chunk.ToolCall != nil {
			ids = append(ids, chunk.ToolCall.ID)
		}
	}
	if len(ids) != 2 {
		t.Fatalf("want 2 tool calls, got %d: %v", len(ids), ids)
	}
	if ids[0] == "" || ids[1] == "" {
		t.Errorf("a tool call came back with an empty id: %v", ids)
	}
	if ids[0] == ids[1] {
		t.Errorf("synthesized ids must be distinct, got %v", ids)
	}
}
