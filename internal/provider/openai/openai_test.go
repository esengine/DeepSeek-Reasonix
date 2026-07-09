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

// TestStreamRetriesThenSucceeds drives the real retry path end-to-end: the
// server returns 503 twice, then a valid SSE stream. The provider must back off,
// fire the retry-notify callback for each attempt, and ultimately stream the answer.
func TestStreamRetriesThenSucceeds(t *testing.T) {
	var reqs int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqs++
		if reqs <= 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":"overloaded"}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hi there\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()

	p, err := New(provider.Config{Name: "deepseek", BaseURL: srv.URL, Model: "deepseek-v4", APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	var attempts []int
	ctx := provider.WithRetryNotify(context.Background(), func(i provider.RetryInfo) {
		attempts = append(attempts, i.Attempt)
		if i.Max != provider.MaxRetries {
			t.Errorf("RetryInfo.Max = %d, want %d", i.Max, provider.MaxRetries)
		}
	})

	ch, err := p.Stream(ctx, provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("Stream after retries: %v", err)
	}
	var got strings.Builder
	for chunk := range ch {
		if chunk.Type == provider.ChunkError {
			t.Fatalf("unexpected stream error: %v", chunk.Err)
		}
		if chunk.Type == provider.ChunkText {
			got.WriteString(chunk.Text)
		}
	}
	if got.String() != "hi there" {
		t.Errorf("streamed text = %q, want %q", got.String(), "hi there")
	}
	if reqs != 3 {
		t.Errorf("server saw %d requests, want 3 (2 failures + 1 success)", reqs)
	}
	if len(attempts) != 2 || attempts[0] != 1 || attempts[1] != 2 {
		t.Errorf("retry-notify attempts = %v, want [1 2]", attempts)
	}
}

// TestStreamInsufficientBalance verifies a 402 fails fast (no retry) as a typed
// *provider.APIError carrying the status, so the display layer can explain it.
func TestStreamInsufficientBalance(t *testing.T) {
	var reqs int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqs++
		w.WriteHeader(http.StatusPaymentRequired)
		_, _ = w.Write([]byte(`{"error":"Insufficient Balance"}`))
	}))
	defer srv.Close()

	p, _ := New(provider.Config{Name: "deepseek", BaseURL: srv.URL, Model: "deepseek-v4", APIKey: "k"})
	_, err := p.Stream(context.Background(), provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}}})
	var apiErr *provider.APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 402 {
		t.Fatalf("want *provider.APIError{Status:402}, got %T: %v", err, err)
	}
	if reqs != 1 {
		t.Errorf("402 should not retry, server saw %d requests", reqs)
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

func TestStreamUsesConfiguredChatURL(t *testing.T) {
	var sawRequest bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawRequest = true
		if r.URL.Path != "/proxy/v1/chat/completions" {
			t.Errorf("path = %s, want /proxy/v1/chat/completions", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer k" {
			http.Error(w, "bad key", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()

	p, err := New(provider.Config{
		Name:    "custom",
		BaseURL: srv.URL + "/base",
		Model:   "model-a",
		APIKey:  "k",
		Extra:   map[string]any{"chat_url": srv.URL + "/proxy/v1/chat/completions"},
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
	var got strings.Builder
	for chunk := range ch {
		if chunk.Type == provider.ChunkError {
			t.Fatalf("stream error: %v", chunk.Err)
		}
		if chunk.Type == provider.ChunkText {
			got.WriteString(chunk.Text)
		}
	}
	if !sawRequest {
		t.Fatal("server did not receive request")
	}
	if got.String() != "ok" {
		t.Fatalf("streamed text = %q, want ok", got.String())
	}
}

func TestStreamSendsCustomHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer real-key" {
			http.Error(w, "authorization was not preserved", http.StatusUnauthorized)
			return
		}
		if r.Header.Get("HTTP-Referer") != "https://app.example" || r.Header.Get("X-Title") != "Reasonix" {
			http.Error(w, "custom headers missing", http.StatusForbidden)
			return
		}
		if r.Header.Get("Accept") != "text/event-stream" {
			http.Error(w, "reserved Accept header was overwritten", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()

	p, err := New(provider.Config{
		Name:    "custom",
		BaseURL: srv.URL,
		Model:   "model-a",
		APIKey:  "real-key",
		Extra: map[string]any{"headers": map[string]string{
			"Authorization": "Bearer wrong",
			"Accept":        "application/json",
			"HTTP-Referer":  "https://app.example",
			"X-Title":       "Reasonix",
		}},
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
	for chunk := range ch {
		if chunk.Type == provider.ChunkError {
			t.Fatalf("stream error: %v", chunk.Err)
		}
	}
}

func TestStreamUsesMiMoAPIKeyHeader(t *testing.T) {
	var gotAuth, gotAPIKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotAPIKey = r.Header.Get("api-key")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()

	p, err := New(provider.Config{
		Name:    "mimo",
		BaseURL: "https://api.xiaomimimo.com/v1",
		Model:   "mimo-v2.5-pro",
		APIKey:  "mimo-key",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c := p.(*client)
	c.chatURL = srv.URL

	ch, err := p.Stream(context.Background(), provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	for chunk := range ch {
		if chunk.Type == provider.ChunkError {
			t.Fatalf("stream error: %v", chunk.Err)
		}
	}
	if gotAPIKey != "mimo-key" {
		t.Fatalf("api-key = %q, want mimo-key", gotAPIKey)
	}
	if gotAuth != "" {
		t.Fatalf("Authorization = %q, want omitted for MiMo", gotAuth)
	}
}

func TestStreamSendsExtraBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read body", http.StatusBadRequest)
			return
		}
		var req map[string]any
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		if req["enable_thinking"] != true {
			http.Error(w, "extra enable_thinking missing", http.StatusBadRequest)
			return
		}
		if got, ok := req["top_p"].(float64); !ok || got != 0.7 {
			http.Error(w, "extra top_p missing", http.StatusBadRequest)
			return
		}
		if req["model"] != "model-a" || req["stream"] != true {
			http.Error(w, "reserved fields were overwritten", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()

	p, err := New(provider.Config{
		Name:    "custom",
		BaseURL: srv.URL,
		Model:   "model-a",
		APIKey:  "real-key",
		Extra: map[string]any{"extra_body": map[string]any{
			"enable_thinking": true,
			"top_p":           0.7,
			"model":           "wrong",
			"stream":          false,
		}},
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
	for chunk := range ch {
		if chunk.Type == provider.ChunkError {
			t.Fatalf("stream error: %v", chunk.Err)
		}
	}
}

// TestBuildRequestAlwaysSerializesContent guards the DeepSeek 400 regression:
// DeepSeek rejects a message missing the `content` field, so every message must
// serialize one. A pure tool_calls assistant turn carries null (OpenAI-spec,
// and accepted by DeepSeek — verified against a live multi-tool session); other
// roles serialize a string. The field must never be absent.
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
	// The tool-call-only assistant message must carry content:null and its tool_calls.
	if got := string(raw[1]["content"]); got != `null` {
		t.Errorf("assistant content = %s, want null", got)
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
func TestBuildRequestDropsReasoningOnPlainAssistantTurn(t *testing.T) {
	c := &client{model: "deepseek-reasoner", deepseek: true}
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
		t.Errorf("a no-tool-calls assistant turn must not carry reasoning_content: %s", b)
	}
	if strings.Contains(string(b), "SECRET-CHAIN-OF-THOUGHT") {
		t.Errorf("the assistant chain-of-thought leaked into the request: %s", b)
	}
	if !strings.Contains(string(b), "the answer") {
		t.Errorf("assistant content was dropped along with reasoning: %s", b)
	}
}

func TestBuildRequestDropsMemoryCitations(t *testing.T) {
	c := &client{model: "deepseek-chat", deepseek: true}
	req := c.buildRequest(provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: "continue"},
			{Role: provider.RoleUser, Content: "edited prompt", Edited: true, Original: "original prompt"},
			{Role: provider.RoleAssistant, Content: "done", MemoryCitations: []provider.MemoryCitation{{
				ID: "mem-1", Source: "MEMORY.md", LineStart: 116, LineEnd: 123, Note: "workflow",
			}}},
		},
	})
	b, err := json.Marshal(req.Messages)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(b), "memoryCitations") || strings.Contains(string(b), "MEMORY.md") {
		t.Fatalf("local memory citations leaked into OpenAI-compatible request: %s", b)
	}
	if strings.Contains(string(b), "original prompt") || strings.Contains(string(b), `"edited"`) || strings.Contains(string(b), `"original"`) {
		t.Fatalf("local edit metadata leaked into OpenAI-compatible request: %s", b)
	}
	if !strings.Contains(string(b), "done") {
		t.Fatalf("assistant content was dropped with local metadata: %s", b)
	}
}

// DeepSeek thinking mode 400s a tool_calls turn whose reasoning_content was
// dropped on a cache-miss replay, so it must be round-tripped — but only on the
// turn that carries tool calls, and only for the DeepSeek protocol.
func TestBuildRequestRoundTripsReasoningOnDeepSeekToolCalls(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleUser, Content: "count the go files"},
		{
			Role:             provider.RoleAssistant,
			ReasoningContent: "CHAIN-OF-THOUGHT",
			ToolCalls:        []provider.ToolCall{{ID: "c1", Name: "bash", Arguments: `{"command":"ls"}`}},
		},
		{Role: provider.RoleTool, Content: "14", ToolCallID: "c1", Name: "bash"},
	}
	deepseek, _ := json.Marshal((&client{model: "deepseek-v4", deepseek: true}).buildRequest(provider.Request{Messages: msgs}).Messages)
	if !strings.Contains(string(deepseek), "reasoning_content") || !strings.Contains(string(deepseek), "CHAIN-OF-THOUGHT") {
		t.Errorf("DeepSeek tool_calls turn must round-trip reasoning_content: %s", deepseek)
	}

	other, _ := json.Marshal((&client{model: "mimo-v2"}).buildRequest(provider.Request{Messages: msgs}).Messages)
	if strings.Contains(string(other), "CHAIN-OF-THOUGHT") {
		t.Errorf("non-DeepSeek backends must not re-upload reasoning_content: %s", other)
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

func TestBuildRequestTemperatureSerialization(t *testing.T) {
	c := &client{model: "m"}

	omitted := c.buildRequest(provider.Request{})
	if omitted.Temperature != nil {
		t.Fatalf("unset request temperature = %v, want nil", omitted.Temperature)
	}
	b, err := json.Marshal(omitted)
	if err != nil {
		t.Fatalf("marshal omitted: %v", err)
	}
	if strings.Contains(string(b), "temperature") {
		t.Fatalf("unset temperature must be omitted from payload: %s", b)
	}

	zero := c.buildRequest(provider.Request{Temperature: provider.TemperaturePtr(0)})
	if zero.Temperature == nil || *zero.Temperature != 0 {
		t.Fatalf("zero request temperature = %v, want ptr(0)", zero.Temperature)
	}
	b, err = json.Marshal(zero)
	if err != nil {
		t.Fatalf("marshal zero: %v", err)
	}
	if !strings.Contains(string(b), `"temperature":0`) {
		t.Fatalf("explicit zero temperature must be serialized: %s", b)
	}

	nonzero := c.buildRequest(provider.Request{Temperature: provider.TemperaturePtr(0.25)})
	if nonzero.Temperature == nil || *nonzero.Temperature != 0.25 {
		t.Fatalf("nonzero request temperature = %v, want ptr(0.25)", nonzero.Temperature)
	}
}

func TestBuildRequestDeepSeekThinking(t *testing.T) {
	for _, tc := range []struct {
		name          string
		effort        string
		wantThinking  string
		wantReasoning string
	}{
		{name: "high", effort: "high", wantThinking: "enabled", wantReasoning: "high"},
		{name: "max", effort: "max", wantThinking: "enabled", wantReasoning: "max"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := (&client{model: "deepseek-v4", deepseek: true, effort: tc.effort}).buildRequest(provider.Request{})
			if req.Thinking == nil || req.Thinking.Type != tc.wantThinking {
				t.Fatalf("Thinking = %+v, want %q", req.Thinking, tc.wantThinking)
			}
			if req.ReasoningEffort != tc.wantReasoning {
				t.Fatalf("ReasoningEffort = %q, want %q", req.ReasoningEffort, tc.wantReasoning)
			}
		})
	}
}

func TestBuildRequestDeepSeekPreservesCallerTemperature(t *testing.T) {
	c := &client{model: "deepseek-v4", deepseek: true, effort: "high"}

	omitted := c.buildRequest(provider.Request{})
	if omitted.Temperature != nil {
		t.Fatalf("DeepSeek default temperature = %v, want omitted", omitted.Temperature)
	}

	zero := c.buildRequest(provider.Request{Temperature: provider.TemperaturePtr(0)})
	if zero.Temperature == nil || *zero.Temperature != 0 {
		t.Fatalf("DeepSeek explicit zero temperature = %v, want ptr(0)", zero.Temperature)
	}
	if zero.Thinking == nil || zero.Thinking.Type != "enabled" {
		t.Fatalf("DeepSeek thinking = %+v, want enabled", zero.Thinking)
	}
}

// TestBuildRequestMiniMaxThinking covers the M3 wire shape: thinking.type is
// the only knob (no reasoning_effort), and the empty-effort / auto case still
// emits an explicit "adaptive" because that's what the M3 model default means
// (M3 has no implicit "no thinking" mode at the wire level).
func TestBuildRequestMiniMaxThinking(t *testing.T) {
	for _, tc := range []struct {
		name         string
		effort       string
		wantThinking string
	}{
		{name: "auto-defaults-to-adaptive", effort: "", wantThinking: "adaptive"},
		{name: "adaptive", effort: "adaptive", wantThinking: "adaptive"},
		{name: "disabled", effort: "disabled", wantThinking: "disabled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := (&client{model: "MiniMax-M3", minimax: true, effort: tc.effort}).buildRequest(provider.Request{})
			if req.Thinking == nil || req.Thinking.Type != tc.wantThinking {
				t.Fatalf("Thinking = %+v, want %q", req.Thinking, tc.wantThinking)
			}
			if req.ReasoningEffort != "" {
				t.Fatalf("MiniMax must not send reasoning_effort, got %q", req.ReasoningEffort)
			}
		})
	}
}

// TestNewMiniMaxEffortValidation locks in the boot-time validation for the
// MiniMax path. The config effort layer remaps legacy level names, so by the
// time effort reaches this factory it must be one of: "", "adaptive",
// "disabled". Anything else is a config bug, surfaced now (not at request
// time) for an actionable error.
func TestNewMiniMaxEffortValidation(t *testing.T) {
	base := provider.Config{Name: "m3", BaseURL: "https://api.minimaxi.com/v1", Model: "MiniMax-M3", APIKey: "k"}
	// happy path: auto (empty effort) and both explicit values are accepted
	for _, ok := range []string{"", "adaptive", "disabled"} {
		if _, err := New(withEffort(base, ok)); err != nil {
			t.Errorf("effort=%q should be accepted: %v", ok, err)
		}
	}
	// unhappy: anything else is rejected up front
	for _, bad := range []string{"high", "low", "max", "turbo"} {
		if _, err := New(withEffort(base, bad)); err == nil {
			t.Errorf("effort=%q should be rejected", bad)
		}
	}
}

// TestNewMiniMaxSetsFlag is a smoke test for base-URL detection: the factory
// must set the `minimax` flag when the base URL points at api.minimaxi.com
// (with or without the /v1 suffix) so buildRequest picks the right wire shape.
func TestNewMiniMaxSetsFlag(t *testing.T) {
	for _, baseURL := range []string{
		"https://api.minimaxi.com/v1",
		"https://api.minimaxi.com",
	} {
		p, err := New(provider.Config{Name: "m3", BaseURL: baseURL, Model: "MiniMax-M3", APIKey: "k"})
		if err != nil {
			t.Fatalf("New(%q): %v", baseURL, err)
		}
		c := p.(*client)
		if !c.minimax {
			t.Errorf("minimax flag not set for baseURL=%q", baseURL)
		}
	}
}

// TestBuildRequestZhipuThinking covers the Zhipu GLM wire shape: thinking.type
// is enabled|disabled and reasoning_effort is never sent (the endpoint ignores
// it). Auto (empty effort) defaults to "enabled" — the GLM model default.
func TestBuildRequestZhipuThinking(t *testing.T) {
	for _, tc := range []struct {
		name         string
		effort       string
		wantThinking string
	}{
		{name: "auto-defaults-to-enabled", effort: "", wantThinking: "enabled"},
		{name: "enabled", effort: "enabled", wantThinking: "enabled"},
		{name: "disabled", effort: "disabled", wantThinking: "disabled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := (&client{model: "glm-4.5-air", zhipu: true, effort: tc.effort}).buildRequest(provider.Request{})
			if req.Thinking == nil || req.Thinking.Type != tc.wantThinking {
				t.Fatalf("Thinking = %+v, want %q", req.Thinking, tc.wantThinking)
			}
			if req.ReasoningEffort != "" {
				t.Fatalf("Zhipu must not send reasoning_effort, got %q", req.ReasoningEffort)
			}
		})
	}
}

// TestNewZhipuEffortValidation locks in boot-time validation for the Zhipu path.
// The config effort layer remaps depth levels, so by the time effort reaches the
// factory it must be one of: "", "enabled", "disabled".
func TestNewZhipuEffortValidation(t *testing.T) {
	base := provider.Config{Name: "glm", BaseURL: "https://open.bigmodel.cn/api/paas/v4", Model: "glm-4.5-air", APIKey: "k"}
	for _, ok := range []string{"", "enabled", "disabled"} {
		if _, err := New(withEffort(base, ok)); err != nil {
			t.Errorf("effort=%q should be accepted: %v", ok, err)
		}
	}
	for _, bad := range []string{"high", "low", "max", "adaptive"} {
		if _, err := New(withEffort(base, bad)); err == nil {
			t.Errorf("effort=%q should be rejected", bad)
		}
	}
}

// TestNewZhipuSetsFlag is a smoke test for base-URL detection across both the
// China (bigmodel.cn) and international (z.ai) GLM endpoints.
func TestNewZhipuSetsFlag(t *testing.T) {
	for _, baseURL := range []string{
		"https://open.bigmodel.cn/api/paas/v4",
		"https://api.z.ai/api/paas/v4",
	} {
		p, err := New(provider.Config{Name: "glm", BaseURL: baseURL, Model: "glm-4.5-air", APIKey: "k"})
		if err != nil {
			t.Fatalf("New(%q): %v", baseURL, err)
		}
		if c := p.(*client); !c.zhipu {
			t.Errorf("zhipu flag not set for baseURL=%q", baseURL)
		}
	}
}

// TestBuildRequestGenericThinking covers the vendor-agnostic `thinking` config
// field on a provider we don't auto-detect: thinking.type is emitted as set, and
// an empty/unset field leaves thinking off the wire entirely.
func TestBuildRequestGenericThinking(t *testing.T) {
	for _, tc := range []struct {
		name     string
		thinking string
		wantType string // "" means no thinking field
	}{
		{name: "enabled", thinking: "enabled", wantType: "enabled"},
		{name: "disabled", thinking: "disabled", wantType: "disabled"},
		{name: "unset-omits", thinking: "", wantType: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := (&client{model: "some-model", thinkingType: tc.thinking}).buildRequest(provider.Request{})
			if tc.wantType == "" {
				if req.Thinking != nil {
					t.Fatalf("expected no thinking, got %+v", req.Thinking)
				}
				return
			}
			if req.Thinking == nil || req.Thinking.Type != tc.wantType {
				t.Fatalf("Thinking = %+v, want %q", req.Thinking, tc.wantType)
			}
		})
	}
}

// TestNewThinkingConfigParsing pins how the `thinking` config field is read:
// enabled|disabled are kept (case-insensitively), everything else is ignored so
// an unknown value can never break a request.
func TestNewThinkingConfigParsing(t *testing.T) {
	base := provider.Config{Name: "gen", BaseURL: "https://api.example.com/v1", Model: "x", APIKey: "k"}
	for in, want := range map[string]string{"enabled": "enabled", "DISABLED": "disabled", "adaptive": "", "garbage": "", "": ""} {
		cfg := base
		cfg.Extra = map[string]any{"thinking": in}
		p, err := New(cfg)
		if err != nil {
			t.Fatalf("New(thinking=%q): %v", in, err)
		}
		if got := p.(*client).thinkingType; got != want {
			t.Errorf("thinking=%q → thinkingType=%q, want %q", in, got, want)
		}
	}
}

// TestBuildRequestDeepSeekDisabled covers both user-facing ways to turn
// DeepSeek thinking off. Either input must route to thinking.type=disabled,
// drop reasoning_effort, and keep the pre-fix tool-call history bytes: a
// tool_calls turn with no reasoning omits the reasoning_content key entirely
// (only thinking mode requires it), while reasoning left over from a
// thinking-mode round still round-trips so the prompt-cache prefix of a mixed
// thinking-on→off session stays stable.
func TestBuildRequestDeepSeekDisabled(t *testing.T) {
	base := provider.Config{Name: "ds", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4", APIKey: "k"}
	for _, tc := range []struct {
		name  string
		extra map[string]any
	}{
		{name: "effort-disabled", extra: map[string]any{"effort": "disabled"}},
		{name: "thinking-disabled", extra: map[string]any{"thinking": "disabled"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base
			cfg.Extra = tc.extra
			p, err := New(cfg)
			if err != nil {
				t.Fatalf("New(%v): %v", tc.extra, err)
			}
			req := p.(*client).buildRequest(provider.Request{
				Messages: []provider.Message{
					{Role: provider.RoleUser, Content: "inspect"},
					{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
						ID: "call_1", Name: "read_file", Arguments: `{"path":"main.go"}`,
					}}},
					{Role: provider.RoleTool, ToolCallID: "call_1", Name: "read_file", Content: "package main"},
					{Role: provider.RoleAssistant, ReasoningContent: "from a thinking round", ToolCalls: []provider.ToolCall{{
						ID: "call_2", Name: "read_file", Arguments: `{"path":"go.mod"}`,
					}}},
					{Role: provider.RoleTool, ToolCallID: "call_2", Name: "read_file", Content: "module demo"},
				},
			})
			if req.Thinking == nil || req.Thinking.Type != "disabled" {
				t.Fatalf("Thinking = %+v, want disabled", req.Thinking)
			}
			if req.ReasoningEffort != "" {
				t.Fatalf("disabled DeepSeek must not send reasoning_effort, got %q", req.ReasoningEffort)
			}
			if rc := req.Messages[1].ReasoningContent; rc != nil {
				t.Fatalf("disabled mode must omit reasoning_content on a reasoning-less tool_calls turn, got %q", *rc)
			}
			if rc := req.Messages[3].ReasoningContent; rc == nil || *rc != "from a thinking round" {
				t.Fatalf("disabled mode must keep round-tripping thinking-round reasoning, got %v", rc)
			}
		})
	}
}

func withEffort(c provider.Config, effort string) provider.Config {
	extra := c.Extra
	if extra == nil {
		extra = map[string]any{}
	} else {
		cp := make(map[string]any, len(extra)+1)
		for k, v := range extra {
			cp[k] = v
		}
		extra = cp
	}
	extra["effort"] = effort
	c.Extra = extra
	return c
}

func TestBuildRequestNonDeepSeekOmitsThinking(t *testing.T) {
	req := (&client{model: "mimo-v2", effort: "high"}).buildRequest(provider.Request{})
	if req.Thinking != nil {
		t.Fatalf("non-DeepSeek request must not include thinking, got %+v", req.Thinking)
	}
	if req.ReasoningEffort != "high" {
		t.Fatalf("ReasoningEffort = %q, want high", req.ReasoningEffort)
	}
}

func TestNewOllamaCloudReasoningEffort(t *testing.T) {
	p, err := New(provider.Config{Name: "ollama-cloud", BaseURL: "https://ollama.com/v1", Model: "nemotron-3-nano:30b", Extra: map[string]any{"effort": "max"}})
	if err != nil {
		t.Fatalf("New max: %v", err)
	}
	c := p.(*client)
	if got := c.buildRequest(provider.Request{}).ReasoningEffort; got != "max" {
		t.Fatalf("Ollama Cloud reasoning_effort = %q, want max", got)
	}

	p, err = New(provider.Config{Name: "ollama-cloud", BaseURL: "https://ollama.com/v1", Model: "nemotron-3-nano:30b", Extra: map[string]any{"effort": "none"}})
	if err != nil {
		t.Fatalf("New none: %v", err)
	}
	c = p.(*client)
	b, err := json.Marshal(c.buildRequest(provider.Request{}))
	if err != nil {
		t.Fatalf("marshal none: %v", err)
	}
	if strings.Contains(string(b), "reasoning_effort") {
		t.Fatalf("Ollama Cloud effort none must omit reasoning_effort: %s", b)
	}

	if _, err := New(provider.Config{Name: "ollama-cloud", BaseURL: "https://ollama.com/v1", Model: "nemotron-3-nano:30b", Extra: map[string]any{"effort": "ultra"}}); err == nil {
		t.Fatal("New invalid effort succeeded, want error")
	}
}

func TestNewDeepSeekThinkingDefaultsAndValidation(t *testing.T) {
	p, err := New(provider.Config{Name: "deepseek", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c := p.(*client)
	if !c.deepseek || c.effort != "high" {
		t.Fatalf("deepseek=%v effort=%q, want true/high", c.deepseek, c.effort)
	}

	p, err = New(provider.Config{Name: "deepseek", BaseURL: "https://api.deepseek.com/v1", Model: "deepseek-v4", Extra: map[string]any{"effort": "max"}})
	if err != nil {
		t.Fatalf("New max: %v", err)
	}
	if got := p.(*client).effort; got != "max" {
		t.Fatalf("effort = %q, want max", got)
	}

	if _, err := New(provider.Config{Name: "deepseek", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4", Extra: map[string]any{"effort": "medium"}}); err == nil {
		t.Fatal("New should reject invalid DeepSeek effort")
	}
	p, err = New(provider.Config{Name: "deepseek", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4", Extra: map[string]any{"effort": "off"}})
	if err != nil {
		t.Fatalf("New should migrate retired effort=off, not reject it: %v", err)
	}
	if got := p.(*client).effort; got != "high" {
		t.Fatalf("retired effort=off should fall back to high, got %q", got)
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

func TestStreamReadsReasoningFallbackField(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `data: {"choices":[{"delta":{"reasoning":"vllm thinking","content":"answer"}}]}`+"\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	p, err := New(provider.Config{Name: "vllm", BaseURL: srv.URL, Model: "qwen", APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ch, err := p.Stream(context.Background(), provider.Request{})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var reasoning, text strings.Builder
	for chunk := range ch {
		switch chunk.Type {
		case provider.ChunkReasoning:
			reasoning.WriteString(chunk.Text)
		case provider.ChunkText:
			text.WriteString(chunk.Text)
		case provider.ChunkError:
			t.Fatalf("stream error: %v", chunk.Err)
		}
	}
	if reasoning.String() != "vllm thinking" {
		t.Fatalf("reasoning = %q, want vLLM fallback field", reasoning.String())
	}
	if text.String() != "answer" {
		t.Fatalf("text = %q, want answer", text.String())
	}
}

func TestStreamReasoningContentTakesPrecedenceOverFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `data: {"choices":[{"delta":{"reasoning_content":"standard","reasoning":"fallback"}}]}`+"\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	p, err := New(provider.Config{Name: "vllm", BaseURL: srv.URL, Model: "qwen", APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ch, err := p.Stream(context.Background(), provider.Request{})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var reasoning strings.Builder
	for chunk := range ch {
		switch chunk.Type {
		case provider.ChunkReasoning:
			reasoning.WriteString(chunk.Text)
		case provider.ChunkError:
			t.Fatalf("stream error: %v", chunk.Err)
		}
	}
	if reasoning.String() != "standard" {
		t.Fatalf("reasoning = %q, want reasoning_content precedence", reasoning.String())
	}
}

// TestBuildRequestAlwaysSendsReasoningKeyOnDeepSeekToolCalls proves the wire
// contract verified against the live API: DeepSeek thinking mode 400s an
// assistant tool_calls turn whose reasoning_content KEY is missing from the
// request JSON, but accepts an empty string. A turn whose reasoning was lost
// upstream (gateway renamed/dropped the field, legacy session, model switch)
// must therefore still serialize the key — while plain assistant text turns
// keep omitting it.
func TestBuildRequestAlwaysSendsReasoningKeyOnDeepSeekToolCalls(t *testing.T) {
	p, err := New(provider.Config{
		Name:    "deepseek-proxy",
		BaseURL: "https://api.deepseek.com",
		Model:   "deepseek-v4-pro",
		APIKey:  "k",
		Extra:   map[string]any{"reasoning_protocol": "deepseek"},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	body, err := json.Marshal(p.(*client).buildRequest(provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: "inspect"},
			{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
				ID: "call_1", Name: "read_file", Arguments: `{"path":"main.go"}`,
			}}},
			{Role: provider.RoleTool, ToolCallID: "call_1", Name: "read_file", Content: "package main"},
			{Role: provider.RoleAssistant, Content: "plain text turn"},
		},
	}))
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	var req struct {
		Messages []map[string]json.RawMessage `json:"messages"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	if len(req.Messages) != 4 {
		t.Fatalf("messages = %d, want 4", len(req.Messages))
	}
	rc, ok := req.Messages[1]["reasoning_content"]
	if !ok {
		t.Fatal("tool_calls turn with lost reasoning must still serialize the reasoning_content key")
	}
	if string(rc) != `""` {
		t.Fatalf("reasoning_content = %s, want empty string", rc)
	}
	if _, ok := req.Messages[3]["reasoning_content"]; ok {
		t.Fatal("plain assistant text turn must keep omitting reasoning_content")
	}
}

// TestBuildRequestRoundTripsDeepSeekToolCallReasoning keeps the healthy-path
// bytes intact: when the session has the provider-issued reasoning, it is
// replayed verbatim on the tool_calls turn.
func TestBuildRequestRoundTripsDeepSeekToolCallReasoning(t *testing.T) {
	p, err := New(provider.Config{
		Name:    "deepseek-proxy",
		BaseURL: "https://api.deepseek.com",
		Model:   "deepseek-v4-pro",
		APIKey:  "k",
		Extra:   map[string]any{"reasoning_protocol": "deepseek"},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	out := p.(*client).buildRequest(provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: "inspect"},
			{Role: provider.RoleAssistant, ReasoningContent: "read main.go first", ToolCalls: []provider.ToolCall{{
				ID: "call_1", Name: "read_file", Arguments: `{"path":"main.go"}`,
			}}},
			{Role: provider.RoleTool, ToolCallID: "call_1", Name: "read_file", Content: "package main"},
		},
	})
	got := out.Messages[1].ReasoningContent
	if got == nil || *got != "read main.go first" {
		t.Fatalf("reasoning_content = %v, want provider-issued reasoning round-tripped", got)
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
			if s, ok := m.Content.(string); ok {
				toolContents = append(toolContents, s)
			}
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

func TestBuildRequestContentNullForAssistantToolCalls(t *testing.T) {
	c := &client{name: "x", model: "m", baseURL: "https://api.example.com/v1"}
	req := provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleAssistant, Content: "", ToolCalls: []provider.ToolCall{{ID: "c1", Name: "ls", Arguments: `{}`}}},
			{Role: provider.RoleTool, Content: "", ToolCallID: "c1", Name: "ls"},
			{Role: provider.RoleAssistant, Content: "all done"},
		},
		Tools: []provider.ToolSchema{{Name: "noargs", Parameters: provider.CanonicalizeSchema(nil)}},
	}
	body, err := json.Marshal(c.buildRequest(req))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !json.Valid(body) {
		t.Fatalf("invalid JSON body: %s", body)
	}
	s := string(body)
	if !strings.Contains(s, `"tool_calls"`) || !strings.Contains(s, `"content":null`) {
		t.Errorf("assistant tool_calls turn should carry null content: %s", s)
	}
	if !strings.Contains(s, `{"role":"tool","content":""`) {
		t.Errorf("tool message should keep empty-string content, not null: %s", s)
	}
	if !strings.Contains(s, `"content":"all done"`) {
		t.Errorf("text assistant turn should keep its string content: %s", s)
	}
	if !strings.Contains(s, `"parameters":{"properties":{},"type":"object"}`) {
		t.Errorf("no-param tool should serialize a strict empty-object schema: %s", s)
	}
}

func TestBuildRequestOmitsResponseOnlyToolCallIndex(t *testing.T) {
	c := &client{name: "x", model: "m", baseURL: "https://api.example.com/v1"}
	req := provider.Request{
		Messages: []provider.Message{{
			Role: provider.RoleAssistant,
			ToolCalls: []provider.ToolCall{{
				ID:        "call_1",
				Name:      "bash",
				Arguments: `{"cmd":"ls"}`,
			}},
		}},
	}
	body, err := json.Marshal(c.buildRequest(req))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(body)
	if !strings.Contains(s, `"tool_calls"`) {
		t.Fatalf("request body missing tool call: %s", s)
	}
	if strings.Contains(s, `"index"`) {
		t.Fatalf("request body contains response-only tool_call index: %s", s)
	}
}

func TestBuildRequestDefaultsEmptyToolParameters(t *testing.T) {
	c := &client{name: "x", model: "m", baseURL: "https://api.example.com/v1"}
	req := provider.Request{
		Tools: []provider.ToolSchema{{Name: "noargs"}},
	}
	body, err := json.Marshal(c.buildRequest(req))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var wire struct {
		Tools []struct {
			Function map[string]json.RawMessage `json:"function"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		t.Fatalf("unmarshal request: %v\n%s", err, body)
	}
	if len(wire.Tools) != 1 {
		t.Fatalf("tools = %d, want 1: %s", len(wire.Tools), body)
	}
	fn := wire.Tools[0].Function
	if string(fn["name"]) != `"noargs"` {
		t.Fatalf("function name = %s, want noargs", fn["name"])
	}
	if _, ok := fn["description"]; ok {
		t.Fatalf("empty description should be omitted: %s", body)
	}
	if got, want := string(fn["parameters"]), `{"properties":{},"type":"object"}`; got != want {
		t.Fatalf("nil parameters should default to %s, got %s in %s", want, got, body)
	}
}
