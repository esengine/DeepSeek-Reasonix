package responses

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/provider"
)

func boolPtr(value bool) *bool { return &value }

func collect(t *testing.T, p provider.Provider, req provider.Request) []provider.Chunk {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stream, err := p.Stream(ctx, req)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var chunks []provider.Chunk
	for chunk := range stream {
		chunks = append(chunks, chunk)
	}
	return chunks
}

func writeEvents(w http.ResponseWriter, events ...string) {
	w.Header().Set("Content-Type", "text/event-stream")
	for _, event := range events {
		_, _ = w.Write([]byte("event: ignored\n"))
		_, _ = w.Write([]byte("data: " + event + "\n\n"))
	}
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

func TestDetectVendorAndModeDefaults(t *testing.T) {
	tests := []struct{ url, vendor, mode string }{
		{"https://api.deepseek.com", "deepseek", "stateless"},
		{"https://dashscope.aliyuncs.com/compatible-mode/v1", "dashscope", "stateful"},
		{"https://token-plan.cn-beijing.maas.aliyuncs.com/compatible-mode/v1", "dashscope", "stateful"},
		{"https://example.com/v1", "", "stateful"},
	}
	for _, test := range tests {
		if got := DetectVendor(test.url); got != test.vendor {
			t.Errorf("DetectVendor(%q) = %q, want %q", test.url, got, test.vendor)
		}
		if got := (Config{BaseURL: test.url}).mode(); got != test.mode {
			t.Errorf("mode(%q) = %q, want %q", test.url, got, test.mode)
		}
	}
	if got := (Config{BaseURL: "https://api.deepseek.com", Mode: "stateful"}).mode(); got != "stateful" {
		t.Fatalf("explicit mode = %q", got)
	}
	if got := (Config{BaseURL: "https://api.deepseek.com", Mode: "stateful", Stateful: boolPtr(false)}).mode(); got != "stateful" {
		t.Fatalf("mode must win over legacy stateful, got %q", got)
	}
	if got := (Config{BaseURL: "https://example.com", Stateful: boolPtr(false)}).mode(); got != "stateless" {
		t.Fatalf("legacy stateful=false mode = %q", got)
	}
}

func TestDeepSeekEffortUsesResponsesReasoningShape(t *testing.T) {
	tests := []struct{ effort, want string }{
		{"auto", ""}, {"disabled", "none"}, {"minimal", "minimal"}, {"low", "low"}, {"high", "high"}, {"max", "max"},
	}
	for _, test := range tests {
		client := New(Config{Name: "deepseek", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-flash", Effort: test.effort}).(*client)
		body, _, _ := client.buildRequestBody(provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}}})
		reasoning, _ := body["reasoning"].(map[string]any)
		got, _ := reasoning["effort"].(string)
		if got != test.want {
			t.Errorf("effort %q serialized as %q, want %q", test.effort, got, test.want)
		}
	}
}

func TestFactoryPreservesUnsetLegacyStatefulForVendorDetection(t *testing.T) {
	p, err := newFromConfig(provider.Config{
		Name: "deepseek", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-flash",
		Extra: map[string]any{"stateful": (*bool)(nil)},
	})
	if err != nil {
		t.Fatalf("newFromConfig: %v", err)
	}
	if got := p.(*client).mode; got != "stateless" {
		t.Fatalf("unset stateful mode = %q, want DeepSeek vendor default stateless", got)
	}
}

func TestStatelessRequestReplaysReasoningContentAndToolPair(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeEvents(w, `{"type":"response.completed","response":{"id":"resp_1","usage":{"input_tokens":10,"output_tokens":2,"total_tokens":12}}}`)
	}))
	defer server.Close()

	p := New(Config{Name: "deepseek", APIKey: "key", BaseURL: server.URL, Model: "deepseek-v4-flash", Mode: "stateless", Effort: "high"})
	collect(t, p, provider.Request{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "system"},
		{Role: provider.RoleUser, Content: "weather"},
		{Role: provider.RoleAssistant, Content: "checking", ReasoningContent: "need a tool", ToolCalls: []provider.ToolCall{{ID: "call_1", Name: "weather", Arguments: `{"city":"SG"}`}}},
		{Role: provider.RoleTool, ToolCallID: "call_1", Name: "weather", Content: "sunny"},
	}})

	if body["previous_response_id"] != nil {
		t.Fatalf("stateless request has previous_response_id: %#v", body)
	}
	if body["instructions"] != "system" {
		t.Fatalf("instructions = %#v", body["instructions"])
	}
	items, ok := body["input"].([]any)
	if !ok || len(items) != 5 {
		t.Fatalf("input = %#v, want user/reasoning/assistant/call/output", body["input"])
	}
	wantTypes := []string{"", "reasoning", "", "function_call", "function_call_output"}
	for i, want := range wantTypes {
		item := items[i].(map[string]any)
		if got, _ := item["type"].(string); got != want {
			t.Errorf("item[%d].type = %q, want %q: %#v", i, got, want, item)
		}
	}
	assistant := items[2].(map[string]any)
	if assistant["content"] != "checking" {
		t.Fatalf("assistant content lost: %#v", assistant)
	}
	reasoning := items[1].(map[string]any)["content"].([]any)[0].(map[string]any)
	if reasoning["type"] != "reasoning_text" || reasoning["text"] != "need a tool" {
		t.Fatalf("reasoning item = %#v", reasoning)
	}
}

func TestStatelessRequestSanitizesMissingToolOutput(t *testing.T) {
	client := New(Config{Name: "test", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-flash"}).(*client)
	body, _, _ := client.buildRequestBody(provider.Request{Messages: []provider.Message{
		{Role: provider.RoleUser, Content: "run"},
		{Role: provider.RoleAssistant, ReasoningContent: "call", ToolCalls: []provider.ToolCall{{ID: "call_1", Name: "bash", Arguments: `{"command":"pwd"}`}}},
	}})
	items := body["input"].([]map[string]any)
	if got := items[len(items)-1]["type"]; got != "function_call_output" {
		t.Fatalf("last input item = %#v, want repaired function_call_output", items[len(items)-1])
	}
}

func TestStreamDoesNotDuplicateDoneText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeEvents(w,
			`{"type":"response.output_text.delta","item_id":"msg_1","content_index":0,"delta":"hello"}`,
			`{"type":"response.output_text.done","item_id":"msg_1","content_index":0,"text":"hello"}`,
			`{"type":"response.completed","response":{"id":"resp_1","usage":{"input_tokens":3,"input_tokens_details":{"cached_tokens":2},"output_tokens":1,"output_tokens_details":{"reasoning_tokens":1},"total_tokens":4}}}`,
		)
	}))
	defer server.Close()

	chunks := collect(t, New(Config{Name: "test", APIKey: "key", BaseURL: server.URL, Model: "m", Mode: "stateless"}), provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}}})
	var text string
	var usage *provider.Usage
	for _, chunk := range chunks {
		if chunk.Type == provider.ChunkText {
			text += chunk.Text
		}
		if chunk.Type == provider.ChunkUsage {
			usage = chunk.Usage
		}
	}
	if text != "hello" {
		t.Fatalf("streamed text = %q, want one copy", text)
	}
	if usage == nil || usage.CacheHitTokens != 2 || usage.CacheMissTokens != 1 || usage.ReasoningTokens != 1 {
		t.Fatalf("usage = %+v", usage)
	}
	if chunks[len(chunks)-1].Type != provider.ChunkDone {
		t.Fatalf("last chunk = %v", chunks[len(chunks)-1].Type)
	}
}

func TestFunctionArgumentEventsUseOutputItemMappingAndCumulativeProgress(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeEvents(w,
			`{"type":"response.output_item.added","item":{"id":"fc_1","type":"function_call","call_id":"call_1","name":"bash"}}`,
			`{"type":"response.function_call_arguments.delta","item_id":"fc_1","delta":"{\"command\":"}`,
			`{"type":"response.function_call_arguments.delta","item_id":"fc_1","delta":"\"pwd\"}"}`,
			`{"type":"response.function_call_arguments.done","item_id":"fc_1","arguments":"{\"command\":\"pwd\"}"}`,
			`{"type":"response.output_item.done","item":{"id":"fc_1","type":"function_call","call_id":"call_1","name":"bash","arguments":"{\"command\":\"pwd\"}"}}`,
			`{"type":"response.completed","response":{"id":"resp_1","usage":{"input_tokens":2,"output_tokens":2,"total_tokens":4}}}`,
		)
	}))
	defer server.Close()

	chunks := collect(t, New(Config{Name: "test", APIKey: "key", BaseURL: server.URL, Model: "m", Mode: "stateless"}), provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "pwd"}}})
	var starts, completed int
	var progress []int
	for _, chunk := range chunks {
		switch chunk.Type {
		case provider.ChunkToolCallStart:
			starts++
			if chunk.ToolCall.ID != "call_1" || chunk.ToolCall.Name != "bash" {
				t.Errorf("start = %+v", chunk.ToolCall)
			}
		case provider.ChunkToolCallArgsDelta:
			progress = append(progress, chunk.ArgChars)
			if chunk.ToolCall.ID != "call_1" {
				t.Errorf("delta ID = %q", chunk.ToolCall.ID)
			}
		case provider.ChunkToolCall:
			completed++
			if chunk.ToolCall.ID != "call_1" || chunk.ToolCall.Name != "bash" || chunk.ToolCall.Arguments != `{"command":"pwd"}` {
				t.Errorf("complete = %+v", chunk.ToolCall)
			}
		}
	}
	if starts != 1 || completed != 1 {
		t.Fatalf("starts=%d completed=%d", starts, completed)
	}
	if len(progress) != 2 || progress[1] <= progress[0] {
		t.Fatalf("argument progress = %v, want cumulative", progress)
	}
}

func TestIncompleteResponseSurfacesFinishReason(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeEvents(w, `{"type":"response.incomplete","response":{"id":"resp_1","incomplete_details":{"reason":"max_output_tokens"},"usage":{"input_tokens":2,"output_tokens":3,"total_tokens":5}}}`)
	}))
	defer server.Close()
	p := New(Config{Name: "test", APIKey: "key", BaseURL: server.URL, Model: "m", Mode: "stateful"}).(*client)
	p.lastResponseID = "stale"
	p.expectedPrefixDigest = "stale"
	chunks := collect(t, p, provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}}})
	for _, chunk := range chunks {
		if chunk.Type == provider.ChunkUsage {
			if chunk.Usage.FinishReason != "length" {
				t.Fatalf("finish reason = %q", chunk.Usage.FinishReason)
			}
			if p.lastResponseID != "" || p.expectedPrefixDigest != "" {
				t.Fatalf("incomplete response retained stateful context: id=%q digest=%q", p.lastResponseID, p.expectedPrefixDigest)
			}
			return
		}
	}
	t.Fatal("missing usage chunk")
}

func TestStatefulContinuationValidatesConversationPrefix(t *testing.T) {
	var mu sync.Mutex
	var bodies []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		bodies = append(bodies, body)
		id := len(bodies)
		mu.Unlock()
		writeEvents(w,
			`{"type":"response.output_text.delta","item_id":"msg","delta":"answer"}`,
			`{"type":"response.completed","response":{"id":"resp_`+string(rune('0'+id))+`","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`,
		)
	}))
	defer server.Close()
	p := New(Config{Name: "stateful", APIKey: "key", BaseURL: server.URL, Model: "m", Mode: "stateful"})

	collect(t, p, provider.Request{Messages: []provider.Message{{Role: provider.RoleSystem, Content: "sys"}, {Role: provider.RoleUser, Content: "one"}}})
	collect(t, p, provider.Request{Messages: []provider.Message{{Role: provider.RoleSystem, Content: "sys"}, {Role: provider.RoleUser, Content: "one"}, {Role: provider.RoleAssistant, Content: "answer"}, {Role: provider.RoleUser, Content: "two"}}})
	collect(t, p, provider.Request{Messages: []provider.Message{{Role: provider.RoleSystem, Content: "different"}, {Role: provider.RoleUser, Content: "new session"}}})

	if bodies[1]["previous_response_id"] != "resp_1" || bodies[1]["input"] != "two" {
		t.Fatalf("valid continuation = %#v", bodies[1])
	}
	if bodies[1]["instructions"] != "sys" {
		t.Fatalf("valid continuation instructions = %#v, want %q", bodies[1]["instructions"], "sys")
	}
	if _, ok := bodies[2]["previous_response_id"]; ok {
		t.Fatalf("session switch reused previous response: %#v", bodies[2])
	}
	if _, ok := bodies[2]["input"].([]any); !ok {
		t.Fatalf("session switch did not send full input: %#v", bodies[2]["input"])
	}
}

func TestExpiredPreviousResponseRetriesOnceWithFullHistory(t *testing.T) {
	var mu sync.Mutex
	var bodies []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		bodies = append(bodies, body)
		attempt := len(bodies)
		mu.Unlock()
		if attempt == 2 {
			http.Error(w, `previous_response_id expired`, http.StatusBadRequest)
			return
		}
		id := "resp_1"
		if attempt == 3 {
			id = "resp_2"
		}
		writeEvents(w,
			`{"type":"response.output_text.delta","item_id":"msg","delta":"answer"}`,
			`{"type":"response.completed","response":{"id":"`+id+`","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`,
		)
	}))
	defer server.Close()
	p := New(Config{Name: "stateful", APIKey: "key", BaseURL: server.URL, Model: "m", Mode: "stateful"})
	collect(t, p, provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "one"}}})
	collect(t, p, provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "one"}, {Role: provider.RoleAssistant, Content: "answer"}, {Role: provider.RoleUser, Content: "two"}}})
	if len(bodies) != 3 {
		t.Fatalf("request count = %d, want initial + stale + retry", len(bodies))
	}
	if bodies[1]["previous_response_id"] != "resp_1" {
		t.Fatalf("stale attempt = %#v", bodies[1])
	}
	if _, ok := bodies[2]["previous_response_id"]; ok {
		t.Fatalf("retry still has previous response: %#v", bodies[2])
	}
	if _, ok := bodies[2]["input"].([]any); !ok {
		t.Fatalf("retry input = %#v, want full array", bodies[2]["input"])
	}
}

func TestDashScopeCacheHeaderIsVendorScoped(t *testing.T) {
	var got string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("x-dashscope-session-cache")
		writeEvents(w, `{"type":"response.completed","response":{"id":"resp","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`)
	}))
	defer server.Close()
	p := New(Config{Name: "test", APIKey: "key", BaseURL: server.URL, Model: "m", Mode: "stateless", SessionCache: boolPtr(true)}).(*client)
	p.vendor = "deepseek"
	collect(t, p, provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}}})
	if got != "" {
		t.Fatalf("DeepSeek request leaked DashScope header %q", got)
	}
	p.vendor = "dashscope"
	collect(t, p, provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}}})
	if got != "enable" {
		t.Fatalf("DashScope header = %q", got)
	}
}

func TestRequiresToolCallReasoningOnlyForDeepSeek(t *testing.T) {
	deepseek := New(Config{Name: "deepseek", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-flash"})
	if !provider.RequiresToolCallReasoning(deepseek) {
		t.Fatal("DeepSeek Responses provider must preserve tool-call reasoning")
	}
	other := New(Config{Name: "other", BaseURL: "https://example.com", Model: "m"})
	if provider.RequiresToolCallReasoning(other) {
		t.Fatal("unknown Responses endpoint unexpectedly requires DeepSeek reasoning")
	}
}

func TestFailedEventSurfacesAuthenticationError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeEvents(w, `{"type":"response.failed","response":{"id":"resp","error":{"code":"invalid_api_key","message":"bad API key"}}}`)
	}))
	defer server.Close()
	chunks := collect(t, New(Config{Name: "test", APIKey: "key", KeyEnv: "TEST_API_KEY", BaseURL: server.URL, Model: "m"}), provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}}})
	for _, chunk := range chunks {
		if chunk.Type == provider.ChunkError {
			if _, ok := chunk.Err.(*provider.AuthError); !ok || !strings.Contains(chunk.Err.Error(), "TEST_API_KEY") {
				t.Fatalf("error = %T %v", chunk.Err, chunk.Err)
			}
			return
		}
	}
	t.Fatal("missing error chunk")
}
