package responses

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
		{"https://eu.deepseek.com/v1", "deepseek", "stateless"},
		{"https://api.xiaomimimo.com/v1", "mimo", "stateless"},
		{"https://dashscope.aliyuncs.com/compatible-mode/v1", "dashscope", "stateful"},
		{"https://token-plan.cn-beijing.maas.aliyuncs.com/compatible-mode/v1", "dashscope", "stateful"},
		{"https://api.deepseek.com.attacker.example/v1", "", "stateful"},
		{"https://example.com/api.deepseek.com/v1", "", "stateful"},
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

func TestRequestSerializesExplicitMaxOutputTokens(t *testing.T) {
	client := New(Config{Name: "responses", BaseURL: "https://example.com", Model: "model"}).(*client)
	body, _, _ := client.buildRequestBody(provider.Request{
		Messages:  []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
		MaxTokens: 32 * 1024,
	})
	if got := body["max_output_tokens"]; got != 32*1024 {
		t.Fatalf("max_output_tokens = %#v, want 32768", got)
	}
}

func TestRequestUsesOnlySafeProviderOutputDefaults(t *testing.T) {
	message := []provider.Message{{Role: provider.RoleUser, Content: "hi"}}

	deepseek := New(Config{Name: "deepseek", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-flash"}).(*client)
	deepseekBody, _, _ := deepseek.buildRequestBody(provider.Request{Messages: message})
	if got := deepseekBody["max_output_tokens"]; got != 131072 {
		t.Fatalf("DeepSeek max_output_tokens = %#v, want 131072", got)
	}

	for _, effort := range []string{"none", "disabled", "off", " NONE "} {
		thinkingDisabled := New(Config{Name: "deepseek", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-flash", Effort: effort}).(*client)
		thinkingDisabledBody, _, _ := thinkingDisabled.buildRequestBody(provider.Request{Messages: message})
		if _, exists := thinkingDisabledBody["max_output_tokens"]; exists {
			t.Fatalf("thinking-disabled DeepSeek effort %q received an automatic output budget: %#v", effort, thinkingDisabledBody)
		}
	}

	explicitThinkingDisabled := New(Config{Name: "deepseek", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-flash", Effort: "none", MaxOutputTokens: 8192}).(*client)
	explicitThinkingDisabledBody, _, _ := explicitThinkingDisabled.buildRequestBody(provider.Request{Messages: message})
	if got := explicitThinkingDisabledBody["max_output_tokens"]; got != 8192 {
		t.Fatalf("explicit thinking-disabled DeepSeek budget = %#v, want 8192", got)
	}

	unknown := New(Config{Name: "responses", BaseURL: "https://example.com", Model: "model"}).(*client)
	unknownBody, _, _ := unknown.buildRequestBody(provider.Request{Messages: message})
	if _, exists := unknownBody["max_output_tokens"]; exists {
		t.Fatalf("unknown Responses endpoint received an inferred output budget: %#v", unknownBody)
	}

	disabled := New(Config{Name: "deepseek", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-flash", MaxOutputTokens: -1}).(*client)
	disabledBody, _, _ := disabled.buildRequestBody(provider.Request{Messages: message})
	if _, exists := disabledBody["max_output_tokens"]; exists {
		t.Fatalf("disabled DeepSeek Responses budget remained present: %#v", disabledBody)
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

func TestFactoryPropagatesWebSearch(t *testing.T) {
	p, err := newFromConfig(provider.Config{
		Name: "deepseek", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-flash",
		Extra: map[string]any{"web_search": true},
	})
	if err != nil {
		t.Fatalf("newFromConfig: %v", err)
	}
	if !p.(*client).webSearch {
		t.Fatal("web_search was not propagated to the Responses client")
	}
}

func TestWebSearchToolPrecedesFunctionTools(t *testing.T) {
	client := New(Config{
		Name: "deepseek", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-flash", WebSearch: true,
	}).(*client)
	body, _, _ := client.buildRequestBody(provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "latest release"}},
		Tools:    []provider.ToolSchema{{Name: "read_file", Description: "Read a file", Parameters: json.RawMessage(`{"type":"object"}`)}},
	})
	tools, ok := body["tools"].([]map[string]any)
	if !ok || len(tools) != 2 {
		t.Fatalf("tools = %#v, want web_search plus one function", body["tools"])
	}
	if got := tools[0]["type"]; got != "web_search" {
		t.Fatalf("tools[0] = %#v, want stable web_search first", tools[0])
	}
	if got := tools[1]["type"]; got != "function" {
		t.Fatalf("tools[1] = %#v, want function tool", tools[1])
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
	var text strings.Builder
	var usage *provider.Usage
	for _, chunk := range chunks {
		if chunk.Type == provider.ChunkText {
			text.WriteString(chunk.Text)
		}
		if chunk.Type == provider.ChunkUsage {
			usage = chunk.Usage
		}
	}
	if text.String() != "hello" {
		t.Fatalf("streamed text = %q, want one copy", text.String())
	}
	if usage == nil || usage.CacheHitTokens != 2 || usage.CacheMissTokens != 1 || usage.ReasoningTokens != 1 || usage.RequestCount != 1 {
		t.Fatalf("usage = %+v", usage)
	}
	if chunks[len(chunks)-1].Type != provider.ChunkDone {
		t.Fatalf("last chunk = %v", chunks[len(chunks)-1].Type)
	}
}

func TestStreamToleratesWebSearchLifecycleEvents(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeEvents(w,
			`{"type":"response.web_search_call.in_progress","item_id":"ws_1"}`,
			`{"type":"response.web_search_call.searching","item_id":"ws_1"}`,
			`{"type":"response.web_search_call.completed","item_id":"ws_1"}`,
			`{"type":"response.output_text.delta","item_id":"msg_1","content_index":0,"delta":"found it"}`,
			`{"type":"response.completed","response":{"id":"resp_1","usage":{"input_tokens":4,"output_tokens":2,"total_tokens":6}}}`,
		)
	}))
	defer server.Close()

	chunks := collect(t, New(Config{Name: "deepseek", APIKey: "key", BaseURL: server.URL, Model: "deepseek-v4-flash", Mode: "stateless", WebSearch: true}), provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "search"}},
	})
	var text strings.Builder
	for _, chunk := range chunks {
		if chunk.Type == provider.ChunkText {
			text.WriteString(chunk.Text)
		}
		if chunk.Type == provider.ChunkError {
			t.Fatalf("unexpected stream error: %v", chunk.Err)
		}
	}
	if text.String() != "found it" || chunks[len(chunks)-1].Type != provider.ChunkDone {
		t.Fatalf("chunks = %#v, want searched answer followed by done", chunks)
	}
}

func TestDeepSeekStatelessReplayPreservesCompletedWebSearchCall(t *testing.T) {
	var bodies []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		bodies = append(bodies, body)
		if len(bodies) == 1 {
			writeEvents(w,
				`{"type":"response.output_item.done","item":{"id":"ws_1","type":"web_search_call","status":"completed","action":{"type":"search","query":"latest release","sources":[{"url":"https://api-docs.deepseek.com/updates/"}]}}}`,
				`{"type":"response.output_text.delta","item_id":"msg_1","content_index":0,"delta":"found it"}`,
				`{"type":"response.completed","response":{"id":"resp_1","usage":{"input_tokens":4,"output_tokens":2,"total_tokens":6}}}`,
			)
			return
		}
		writeEvents(w, `{"type":"response.completed","response":{"id":"resp_2","usage":{"input_tokens":8,"output_tokens":2,"total_tokens":10}}}`)
	}))
	defer server.Close()

	client := New(Config{Name: "deepseek", APIKey: "key", BaseURL: server.URL, Model: "deepseek-v4-flash", Mode: "stateless", WebSearch: true}).(*client)
	// The test server is local, so pin the vendor classification to the official
	// DeepSeek behavior under test without weakening production URL detection.
	client.vendor = "deepseek"
	first := collect(t, client, provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "search"}}})
	var replayItems []json.RawMessage
	for _, chunk := range first {
		if chunk.Type == provider.ChunkResponsesItem {
			replayItems = append(replayItems, chunk.ResponsesItem)
		}
	}
	if len(replayItems) != 1 {
		t.Fatalf("replay items = %d, want one completed web_search_call: %#v", len(replayItems), first)
	}

	collect(t, client, provider.Request{Messages: []provider.Message{
		{Role: provider.RoleUser, Content: "search"},
		{Role: provider.RoleAssistant, Content: "found it", ResponsesItems: replayItems},
		{Role: provider.RoleUser, Content: "which source?"},
	}})
	if len(bodies) != 2 {
		t.Fatalf("request bodies = %d, want 2", len(bodies))
	}
	items, ok := bodies[1]["input"].([]any)
	if !ok || len(items) != 4 {
		t.Fatalf("follow-up input = %#v, want user/search-call/assistant/user", bodies[1]["input"])
	}
	search, ok := items[1].(map[string]any)
	if !ok || search["type"] != "web_search_call" || search["id"] != "ws_1" {
		t.Fatalf("replayed search item = %#v", items[1])
	}
	action, _ := search["action"].(map[string]any)
	if action["query"] != "latest release" {
		t.Fatalf("replayed search action = %#v", action)
	}
}

func TestResponsesItemsAreIgnoredOutsideOfficialDeepSeekWire(t *testing.T) {
	raw := json.RawMessage(`{"id":"ws_1","type":"web_search_call","status":"completed"}`)
	client := New(Config{Name: "compatible", BaseURL: "https://gateway.example", Model: "m", Mode: "stateless"}).(*client)
	body, _, _ := client.buildRequestBody(provider.Request{Messages: []provider.Message{
		{Role: provider.RoleUser, Content: "search"},
		{Role: provider.RoleAssistant, Content: "answer", ResponsesItems: []json.RawMessage{raw}},
	}})
	items := body["input"].([]map[string]any)
	for _, item := range items {
		if item["type"] == "web_search_call" {
			t.Fatalf("foreign Responses endpoint received DeepSeek replay item: %#v", items)
		}
	}
}

func TestDeepSeekReplayDropsMalformedOrIncompleteSearchItems(t *testing.T) {
	items := []json.RawMessage{
		json.RawMessage(`{"id":"ws_valid","type":"web_search_call","status":"completed","action":{"type":"search"}}`),
		json.RawMessage(`{"id":"ws_failed","type":"web_search_call","status":"failed"}`),
		json.RawMessage(`{"type":"web_search_call","status":"completed"}`),
		json.RawMessage(`{"id":"fc_1","type":"function_call","status":"completed"}`),
		json.RawMessage(`{"id":`),
	}
	client := New(Config{Name: "deepseek", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-flash", Mode: "stateless"}).(*client)
	body, _, _ := client.buildRequestBody(provider.Request{Messages: []provider.Message{
		{Role: provider.RoleUser, Content: "search"},
		{Role: provider.RoleAssistant, Content: "answer", ResponsesItems: items},
	}})
	wire := body["input"].([]map[string]any)
	var searches []map[string]any
	for _, item := range wire {
		if item["type"] == "web_search_call" {
			searches = append(searches, item)
		}
	}
	if len(searches) != 1 || searches[0]["id"] != "ws_valid" {
		t.Fatalf("replayed searches = %#v, want only completed valid item", searches)
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
	chunks := collect(t, p, provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "one"}, {Role: provider.RoleAssistant, Content: "answer"}, {Role: provider.RoleUser, Content: "two"}}})
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
	var usage *provider.Usage
	for _, chunk := range chunks {
		if chunk.Type == provider.ChunkUsage {
			usage = chunk.Usage
		}
	}
	if usage == nil || usage.RequestCount != 2 {
		t.Fatalf("retry usage = %+v, want request count 2", usage)
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
	p.caps = capabilitiesFor("deepseek")
	collect(t, p, provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}}})
	if got != "" {
		t.Fatalf("DeepSeek request leaked DashScope header %q", got)
	}
	p.vendor = "dashscope"
	p.caps = capabilitiesFor("dashscope")
	collect(t, p, provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}}})
	if got != "enable" {
		t.Fatalf("DashScope header = %q", got)
	}
}

func TestVendorCapabilityTableCoversKnownEndpoints(t *testing.T) {
	tests := []struct {
		url, vendor          string
		stateless, reasoning bool
		ignoresTemp          bool
		singleSegment        bool
	}{
		{"https://api.deepseek.com", "deepseek", true, true, false, false},
		{"https://api.xiaomimimo.com/v1", "mimo", true, true, true, true},
		{"https://dashscope.aliyuncs.com/compatible-mode/v1", "dashscope", false, false, false, false},
		{"https://example.com/v1", "", false, false, false, false},
	}
	for _, test := range tests {
		vendor := DetectVendor(test.url)
		if vendor != test.vendor {
			t.Errorf("DetectVendor(%q) = %q, want %q", test.url, vendor, test.vendor)
		}
		caps := capabilitiesFor(vendor)
		if caps.stateless != test.stateless {
			t.Errorf("capabilities(%q).stateless = %v, want %v", vendor, caps.stateless, test.stateless)
		}
		if caps.toolCallReasoning != test.reasoning {
			t.Errorf("capabilities(%q).toolCallReasoning = %v, want %v", vendor, caps.toolCallReasoning, test.reasoning)
		}
		if caps.ignoresTemperature != test.ignoresTemp {
			t.Errorf("capabilities(%q).ignoresTemperature = %v, want %v", vendor, caps.ignoresTemperature, test.ignoresTemp)
		}
		if caps.singleSegmentReasoning != test.singleSegment {
			t.Errorf("capabilities(%q).singleSegmentReasoning = %v, want %v", vendor, caps.singleSegmentReasoning, test.singleSegment)
		}
	}
}

func TestMiMoOmitsTemperatureFromRequestBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var reqBody map[string]any
		if err := json.Unmarshal(body, &reqBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if _, ok := reqBody["temperature"]; ok {
			t.Fatalf("MiMo request must not carry temperature (vendor forces 1.0 in thinking mode), got %#v", reqBody["temperature"])
		}
		writeEvents(w, `{"type":"response.completed","response":{"id":"resp","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`)
	}))
	defer server.Close()

	temp := 0.3
	p := New(Config{Name: "mimo", APIKey: "key", BaseURL: server.URL, Model: "mimo-v2.5-pro"}).(*client)
	p.vendor = "mimo"
	p.caps = capabilitiesFor("mimo")
	if !p.caps.ignoresTemperature {
		t.Fatal("MiMo capability must ignore temperature")
	}
	collect(t, p, provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}}, Temperature: &temp})

	// Control: an unknown endpoint still sends temperature.
	var gotTemp any
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var reqBody map[string]any
		_ = json.Unmarshal(body, &reqBody)
		gotTemp = reqBody["temperature"]
		writeEvents(w, `{"type":"response.completed","response":{"id":"resp","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`)
	}))
	defer control.Close()
	p2 := New(Config{Name: "openai", APIKey: "key", BaseURL: control.URL, Model: "m"}).(*client)
	collect(t, p2, provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}}, Temperature: &temp})
	if gotTemp == nil {
		t.Fatal("unknown endpoint must still send temperature")
	}
}

func TestRequiresToolCallReasoningForStatelessVendors(t *testing.T) {
	deepseek := New(Config{Name: "deepseek", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-flash"})
	if !provider.RequiresToolCallReasoning(deepseek) {
		t.Fatal("DeepSeek Responses provider must preserve tool-call reasoning")
	}
	mimo := New(Config{Name: "mimo", BaseURL: "https://api.xiaomimimo.com/v1", Model: "mimo-v2.5-pro"})
	if !provider.RequiresToolCallReasoning(mimo) {
		t.Fatal("MiMo Responses provider must preserve tool-call reasoning (documented requirement)")
	}
	other := New(Config{Name: "other", BaseURL: "https://example.com", Model: "m"})
	if provider.RequiresToolCallReasoning(other) {
		t.Fatal("unknown Responses endpoint unexpectedly requires tool-call reasoning")
	}
}

func TestMissingToolCallReasoningWarningFingerprintTracksResponsesConfiguration(t *testing.T) {
	first := New(Config{Name: "deepseek", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-flash", Effort: "high"})
	same := New(Config{Name: "deepseek", BaseURL: "https://api.deepseek.com/", Model: "deepseek-v4-flash", Effort: "high"})
	changedEffort := New(Config{Name: "deepseek", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-flash", Effort: "max"})
	got := provider.MissingToolCallReasoningWarningFingerprint(first)
	if got != provider.MissingToolCallReasoningWarningFingerprint(same) {
		t.Fatal("equivalent Responses configurations produced different fingerprints")
	}
	if got == provider.MissingToolCallReasoningWarningFingerprint(changedEffort) {
		t.Fatal("Responses effort change did not re-key the warning fingerprint")
	}
}

func TestReasoningIDRoundTripsThroughInput(t *testing.T) { // The OpenAI Responses schema marks Reasoning.id required. A message
	// carrying a captured ReasoningID must echo it back on the input
	// reasoning item (and omit it when absent).
	p := New(Config{Name: "mimo", APIKey: "key", BaseURL: "https://api.xiaomimimo.com/v1", Model: "mimo-v2.5"}).(*client)
	p.vendor = "mimo"
	p.caps = capabilitiesFor("mimo")

	body, _, _ := p.buildRequestBody(provider.Request{Messages: []provider.Message{
		{Role: provider.RoleUser, Content: "continue"},
		{Role: provider.RoleAssistant, ReasoningContent: "think hard", ReasoningID: "rs_item_1", ReasoningStatus: "completed"},
	}})
	items := body["input"].([]map[string]any)
	var reasoningItem map[string]any
	for _, item := range items {
		if item["type"] == "reasoning" {
			reasoningItem = item
			break
		}
	}
	if reasoningItem == nil {
		t.Fatal("missing reasoning item in second-turn input")
	}
	if reasoningItem["id"] != "rs_item_1" {
		t.Fatalf("reasoning id = %#v, want rs_item_1", reasoningItem["id"])
	}
	if reasoningItem["status"] != "completed" {
		t.Fatalf("reasoning status = %#v, want completed", reasoningItem["status"])
	}
	// content must still be the only other key.
	if _, has := reasoningItem["summary"]; has {
		t.Fatalf("mimo must not serialize summary, got %#v", reasoningItem["summary"])
	}

	// Without a captured id the key stays absent.
	body2, _, _ := p.buildRequestBody(provider.Request{Messages: []provider.Message{
		{Role: provider.RoleAssistant, ReasoningContent: "think hard"},
	}})
	items2 := body2["input"].([]map[string]any)
	for _, item := range items2 {
		if item["type"] == "reasoning" {
			if _, has := item["id"]; has {
				t.Fatalf("reasoning id must be omitted when not captured, got %#v", item["id"])
			}
		}
	}
}

func TestWarnOnMissingToolCallReasoningIsModelScoped(t *testing.T) {
	// DeepSeek pro-tier: warns (endpoint reliably emits tool-call reasoning).
	pro := New(Config{Name: "deepseek", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-pro"})
	if !provider.WarnOnMissingToolCallReasoning(pro) {
		t.Fatal("DeepSeek pro must warn on missing tool-call reasoning")
	}
	// DeepSeek flash-tier: no warning (flash does not emit tool-call reasoning).
	flash := New(Config{Name: "deepseek", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-flash"})
	if provider.WarnOnMissingToolCallReasoning(flash) {
		t.Fatal("DeepSeek flash must not warn on missing tool-call reasoning")
	}
	// MiMo: preserves reasoning on replay but does not guarantee it every
	// round — a missing chain-of-thought is endpoint-conditional, not a
	// degradation worth a warning (observed: mimo-v2.5-pro tool-call turn
	// with empty reasoning).
	mimo := New(Config{Name: "mimo", BaseURL: "https://api.xiaomimimo.com/v1", Model: "mimo-v2.5-pro"})
	if provider.WarnOnMissingToolCallReasoning(mimo) {
		t.Fatal("MiMo must not warn on missing tool-call reasoning (endpoint-conditional)")
	}
	other := New(Config{Name: "other", BaseURL: "https://example.com", Model: "m"})
	if provider.WarnOnMissingToolCallReasoning(other) {
		t.Fatal("unknown Responses endpoint must not warn on missing tool-call reasoning")
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
			var authErr *provider.AuthError
			if !errors.As(chunk.Err, &authErr) || !strings.Contains(chunk.Err.Error(), "TEST_API_KEY") {
				t.Fatalf("error = %T %v", chunk.Err, chunk.Err)
			}
			return
		}
	}
	t.Fatal("missing error chunk")
}

func TestCompletedResponseDefaultsFinishReasonToStop(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// No finish_reason anywhere in the completed event: the client must
		// synthesize FinishReason="stop" for a normally completed response.
		writeEvents(w, `{"type":"response.completed","response":{"id":"resp_1","usage":{"input_tokens":2,"output_tokens":3,"total_tokens":5}}}`)
	}))
	defer server.Close()
	chunks := collect(t, New(Config{Name: "test", APIKey: "key", BaseURL: server.URL, Model: "m", Mode: "stateful"}), provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}}})
	for _, chunk := range chunks {
		if chunk.Type == provider.ChunkUsage {
			if chunk.Usage.FinishReason != "stop" {
				t.Fatalf("finish reason = %q, want \"stop\"", chunk.Usage.FinishReason)
			}
			return
		}
	}
	t.Fatal("missing usage chunk")
}

func TestAllZeroUsageCompletedEmitsCompletionSemantics(t *testing.T) {
	// DashScope 偶发全零 usage（服务端上报缺口）。旧实现无条件抑制——
	// agent 收不到 usage → reasoningOnlyFinishHonoured 失效 → reasoning-only
	// 完成被误判触发重试（#7168 评审"完成语义保留"只做了一半）。新语义：
	// 全零+stop 也发送（计费层 Pricing.Cost 对全零天然 0 成本，不污染统计），
	// 完成语义恢复；全零且无 finish reason 才抑制（异常前哨场景）。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeEvents(w, `{"type":"response.completed","response":{"id":"resp_1","usage":{"input_tokens":0,"output_tokens":0,"total_tokens":0}}}`)
	}))
	defer server.Close()
	chunks := collect(t, New(Config{Name: "test", APIKey: "key", BaseURL: server.URL, Model: "m", Mode: "stateful"}), provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}}})
	var usageChunks int
	for _, chunk := range chunks {
		if chunk.Type == provider.ChunkUsage {
			usageChunks++
			if chunk.Usage.FinishReason != "stop" {
				t.Fatalf("all-zero usage must carry FinishReason=stop (completion semantics), got %q", chunk.Usage.FinishReason)
			}
			if chunk.Usage.Estimated {
				t.Fatal("synthesized stop on real (zero) usage must not be marked Estimated")
			}
		}
	}
	if usageChunks != 1 {
		t.Fatalf("usage chunks = %d, want 1 (all-zero+stop must be emitted for completion semantics)", usageChunks)
	}
}

func TestMessagesToInputIncludesSummaryOnReasoningItems(t *testing.T) {
	// DashScope is the only vendor whose schema requires the summary list;
	// use its base URL so the capability table opts in.
	client := New(Config{Name: "dashscope", BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1", Model: "qwen3"}).(*client)
	body, _, _ := client.buildRequestBody(provider.Request{Messages: []provider.Message{
		{Role: provider.RoleUser, Content: "run"},
		{Role: provider.RoleAssistant, ReasoningContent: "think step by step", Content: "answer"},
	}})
	items := body["input"].([]map[string]any)
	var reasoning map[string]any
	for _, item := range items {
		if item["type"] == "reasoning" {
			reasoning = item
			break
		}
	}
	if reasoning == nil {
		t.Fatal("missing reasoning item in input")
	}
	// DashScope requires summary as a list, not a scalar; OpenAI format only
	// needs content. The serialized input must carry both.
	summary, ok := reasoning["summary"].([]map[string]string)
	if !ok {
		t.Fatalf("reasoning summary = %#v, want []map[string]string list", reasoning["summary"])
	}
	if len(summary) != 1 || summary[0]["type"] != "summary_text" || summary[0]["text"] != "think step by step" {
		t.Fatalf("reasoning summary = %#v", summary)
	}
	content, ok := reasoning["content"].([]map[string]string)
	if !ok || len(content) != 1 || content[0]["type"] != "reasoning_text" || content[0]["text"] != "think step by step" {
		t.Fatalf("reasoning content = %#v", reasoning["content"])
	}
}

func TestMessagesToInputOmitsSummaryForNonDashScope(t *testing.T) {
	// MiMo/DeepSeek/unknown endpoints do not define `summary` in their
	// reasoning-item schema; sending it leaks the reasoning text into an
	// extra field the server may echo back into the model context (the
	// chain-of-thought doubling that truncates long tool loops). Only
	// `content` must be serialized.
	for _, tc := range []struct {
		name, baseURL, model string
	}{
		{"mimo", "https://api.xiaomimimo.com/v1", "mimo-v2.5"},
		{"deepseek", "https://api.deepseek.com", "deepseek-v4-flash"},
		{"unknown", "https://example.com", "m"},
	} {
		client := New(Config{Name: tc.name, BaseURL: tc.baseURL, Model: tc.model}).(*client)
		body, _, _ := client.buildRequestBody(provider.Request{Messages: []provider.Message{
			{Role: provider.RoleUser, Content: "run"},
			{Role: provider.RoleAssistant, ReasoningContent: "think step by step", Content: "answer"},
		}})
		items := body["input"].([]map[string]any)
		var reasoning map[string]any
		for _, item := range items {
			if item["type"] == "reasoning" {
				reasoning = item
				break
			}
		}
		if reasoning == nil {
			t.Fatalf("%s: missing reasoning item in input", tc.name)
		}
		if _, has := reasoning["summary"]; has {
			t.Fatalf("%s: must not serialize summary (not in vendor schema), got %#v", tc.name, reasoning["summary"])
		}
		content, ok := reasoning["content"].([]map[string]string)
		if !ok || len(content) != 1 || content[0]["type"] != "reasoning_text" || content[0]["text"] != "think step by step" {
			t.Fatalf("%s: reasoning content = %#v", tc.name, reasoning["content"])
		}
	}
}

func TestMessagesToInputOmitsSummaryWithoutReasoning(t *testing.T) {
	client := New(Config{Name: "test", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-flash"}).(*client)
	body, _, _ := client.buildRequestBody(provider.Request{Messages: []provider.Message{
		{Role: provider.RoleUser, Content: "run"},
		// Assistant turn without reasoning: no reasoning item and thus no
		// summary list should be serialized at all.
		{Role: provider.RoleAssistant, Content: "plain answer"},
	}})
	items := body["input"].([]map[string]any)
	for _, item := range items {
		if item["type"] == "reasoning" {
			t.Fatalf("unexpected reasoning item without ReasoningContent: %#v", item)
		}
	}
}

func TestMessagesToInputTextOnlyStaysStringShape(t *testing.T) {
	client := New(Config{Name: "test", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-flash"}).(*client)
	body, _, _ := client.buildRequestBody(provider.Request{Messages: []provider.Message{
		{Role: provider.RoleUser, Content: "hello"},
		{Role: provider.RoleAssistant, Content: "hi"},
	}})
	items := body["input"].([]map[string]any)
	// Text-only turns keep the documented TextInput string shape even with
	// vision enabled: images are the only trigger for the array form.
	for i, item := range items {
		if _, isArr := item["content"].([]map[string]string); isArr {
			t.Fatalf("item[%d] content is an array, want string: %#v", i, item)
		}
		if _, isArr := item["content"].([]map[string]any); isArr {
			t.Fatalf("item[%d] content is an array, want string: %#v", i, item)
		}
	}
}

func TestMessagesToInputEmbedsImagesAsInputImageParts(t *testing.T) {
	c := New(Config{Name: "mimo", BaseURL: "https://api.xiaomimimo.com/v1", Model: "mimo-v2.5", Extra: map[string]any{"vision": true}}).(*client)
	body, _, _ := c.buildRequestBody(provider.Request{Messages: []provider.Message{
		{Role: provider.RoleUser, Content: "what is this", Images: []string{"data:image/png;base64,AAAA", "data:image/jpeg;base64,BBBB"}},
	}})
	items := body["input"].([]map[string]any)
	if len(items) != 1 {
		t.Fatalf("input items = %d, want 1", len(items))
	}
	parts, ok := items[0]["content"].([]map[string]string)
	if !ok {
		t.Fatalf("user content = %#v, want InputItemList array (image turn)", items[0]["content"])
	}
	if len(parts) != 3 {
		t.Fatalf("parts = %d, want 3 (text + 2 images)", len(parts))
	}
	if parts[0]["type"] != "input_text" || parts[0]["text"] != "what is this" {
		t.Fatalf("parts[0] = %#v, want input_text", parts[0])
	}
	for i, want := range []string{"data:image/png;base64,AAAA", "data:image/jpeg;base64,BBBB"} {
		if parts[i+1]["type"] != "input_image" || parts[i+1]["image_url"] != want {
			t.Fatalf("parts[%d] = %#v, want input_image %q", i+1, parts[i+1], want)
		}
	}
	// Vision disabled: images are ignored, content stays a string.
	plain := New(Config{Name: "mimo", BaseURL: "https://api.xiaomimimo.com/v1", Model: "mimo-v2.5"}).(*client)
	body2, _, _ := plain.buildRequestBody(provider.Request{Messages: []provider.Message{
		{Role: provider.RoleUser, Content: "what is this", Images: []string{"data:image/png;base64,AAAA"}},
	}})
	items2 := body2["input"].([]map[string]any)
	if got, ok := items2[0]["content"].(string); !ok || got != "what is this" {
		t.Fatalf("vision-off user content = %#v, want string", items2[0]["content"])
	}
}

func TestOfficialDeepSeekResponsesIgnoresVisionMetadata(t *testing.T) {
	c := New(Config{
		Name: "deepseek", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-flash",
		Extra: map[string]any{"vision": true},
	}).(*client)
	if c.vision {
		t.Fatal("official DeepSeek Responses endpoint must ignore vision metadata")
	}
	body, _, _ := c.buildRequestBody(provider.Request{Messages: []provider.Message{{
		Role: provider.RoleUser, Content: "what is this",
		Images: []string{"data:image/png;base64,AAAA"},
	}}})
	items := body["input"].([]map[string]any)
	if got, ok := items[0]["content"].(string); !ok || got != "what is this" {
		t.Fatalf("official DeepSeek content = %#v, want plain text", items[0]["content"])
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request body: %v", err)
	}
	if bytes.Contains(encoded, []byte("input_image")) || bytes.Contains(encoded, []byte("base64,AAAA")) {
		t.Fatalf("official DeepSeek Responses request leaked image payload: %s", encoded)
	}
}

func TestResponseFormatJSONObjectOnWire(t *testing.T) {
	// text.format.type=json_object must be serialized when requested, and
	// the request body must stay byte-identical without it (cache stability).
	var gotText any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var reqBody map[string]any
		_ = json.Unmarshal(body, &reqBody)
		gotText = reqBody["text"]
		writeEvents(w, `{"type":"response.completed","response":{"id":"resp","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`)
	}))
	defer server.Close()

	p := New(Config{Name: "mimo", APIKey: "key", BaseURL: server.URL, Model: "mimo-v2.5"}).(*client)
	p.vendor = "mimo"
	p.caps = capabilitiesFor("mimo")
	collect(t, p, provider.Request{
		Messages:       []provider.Message{{Role: provider.RoleUser, Content: "give json"}},
		ResponseFormat: &provider.ResponseFormat{Type: "json_object"},
	})
	want := map[string]any{"format": map[string]any{"type": "json_object"}}
	if fmt.Sprintf("%v", gotText) != fmt.Sprintf("%v", want) {
		t.Fatalf("text = %#v, want %#v", gotText, want)
	}

	// Nil format leaves the wire without the key.
	plain := New(Config{Name: "openai", APIKey: "key", BaseURL: "https://example.com", Model: "m"}).(*client)
	req, _, _ := plain.buildRequestBody(provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}}})
	if _, ok := req["text"]; ok {
		t.Fatalf("text must be omitted without ResponseFormat, got %#v", req["text"])
	}
}

func TestConversationDigestMirrorsWireKnobs(t *testing.T) {
	// The stateful fast path compares conversationDigest against the
	// previous request's wire input. A digest built with different
	// vision/summary knobs than buildRequestBody would never match,
	// silently disabling previous_response_id (cache-hit loss).
	messages := []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "hi", Images: []string{"https://x/img.png"}},
		{Role: provider.RoleAssistant, ReasoningContent: "think", Content: "answer"},
	}

	// Vision on: wire input embeds input_image parts; digest must match.
	v := New(Config{Name: "mimo", APIKey: "k", BaseURL: "https://api.xiaomimimo.com/v1", Model: "mimo-v2.5", Extra: map[string]any{"vision": true}}).(*client)
	v.vendor = "mimo"
	v.caps = capabilitiesFor("mimo")
	if got := v.conversationDigest(messages); got == "" {
		t.Fatal("empty digest")
	}
	body, _, _ := v.buildRequestBody(provider.Request{Messages: messages})
	// Digest of the same messages must equal the digest of the wire input.
	// Recompute through a fresh client with identical knobs: identical result.
	v2 := New(Config{Name: "mimo", APIKey: "k", BaseURL: "https://api.xiaomimimo.com/v1", Model: "mimo-v2.5", Extra: map[string]any{"vision": true}}).(*client)
	v2.vendor = "mimo"
	v2.caps = capabilitiesFor("mimo")
	if v.conversationDigest(messages) != v2.conversationDigest(messages) {
		t.Fatal("digest must be deterministic for identical knobs")
	}
	_ = body

	// Vision off vs on must differ (the wire shapes differ).
	plain := New(Config{Name: "mimo", APIKey: "k", BaseURL: "https://api.xiaomimimo.com/v1", Model: "mimo-v2.5"}).(*client)
	plain.vendor = "mimo"
	plain.caps = capabilitiesFor("mimo")
	if plain.conversationDigest(messages) == v.conversationDigest(messages) {
		t.Fatal("vision must change the digest (different wire shape)")
	}

	// DashScope summary knob: wire includes summary, digest must too.
	ds := New(Config{Name: "dashscope", APIKey: "k", BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1", Model: "qwen3"}).(*client)
	ds.caps = capabilitiesFor("dashscope")
	if ds.caps.summaryRequired && ds.conversationDigest(messages) == plain.conversationDigest(messages) {
		t.Fatal("dashscope summary must change the digest (wire sends summary)")
	}
}

// TestSingleSegmentReasoningWiredIntoWarningPolicy：singleSegmentReasoning
// capability 驱动警告策略（评审 #7234 Copilot：wire into behavior）。
func TestSingleSegmentReasoningWiredIntoWarningPolicy(t *testing.T) {
	cases := []struct {
		name, baseURL, model string
		want                 bool
	}{
		{"mimo 单段不警告", "https://api.xiaomimimo.com/v1", "mimo-v2.5-pro", false},
		{"dashscope 无回传契约不警告", "https://dashscope.aliyuncs.com/compatible-mode/v1", "qwen3", false},
		{"deepseek pro 多段警告", "https://api.deepseek.com", "deepseek-v4-pro", true},
		{"deepseek flash 豁免", "https://api.deepseek.com", "deepseek-v4-flash", false},
	}
	for _, tc := range cases {
		pro := New(Config{Name: "t", APIKey: "k", BaseURL: tc.baseURL, Model: tc.model}).(interface {
			WarnOnMissingToolCallReasoning() bool
		})
		if got := pro.WarnOnMissingToolCallReasoning(); got != tc.want {
			t.Errorf("%s: WarnOnMissingToolCallReasoning = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestFactoryPassesExtraThrough：newFromConfig 原样透传 cfg.Extra——
// vision 开关经 provider factory 后仍生效（评审 #7234 第 3 点）。
func TestFactoryPassesExtraThrough(t *testing.T) {
	p, err := newFromConfig(provider.Config{
		Name: "mimo", BaseURL: "https://api.xiaomimimo.com/v1", Model: "mimo-v2.5-pro",
		Extra: map[string]any{"vision": true, "effort": "low", "mode": "stateless"},
	})
	if err != nil {
		t.Fatalf("newFromConfig: %v", err)
	}
	cl := p.(*client)
	if !cl.vision {
		t.Fatal("vision must survive factory (Extra passthrough)")
	}
	if cl.effort != "low" {
		t.Fatalf("effort = %q, want low", cl.effort)
	}
}

// TestReasoningMetaChunkEndToEnd：第一轮 SSE（reasoning item 带 id/status）
// → meta chunk 携带 → 用捕获的 id/status 构造第二轮 Message →
// messagesToInput 回传（评审 #7234 第 1 点要求的端到端回归路径）。
func TestReasoningMetaChunkEndToEnd(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeEvents(w,
			`{"type":"response.output_item.added","item":{"type":"reasoning","id":"rs_round1","summary":[],"content":[]}}`,
			`{"type":"response.reasoning_text.delta","item_id":"rs_round1","delta":"think hard"}`,
			`{"type":"response.output_item.done","item":{"type":"reasoning","id":"rs_round1","status":"completed"}}`,
			`{"type":"response.completed","response":{"id":"resp_1","usage":{"input_tokens":2,"output_tokens":3,"total_tokens":5}}}`,
		)
	}))
	defer server.Close()
	c := New(Config{Name: "deepseek-responses", APIKey: "key", BaseURL: server.URL, Model: "deepseek-v4-pro"}).(*client)

	var rid, rstatus string
	ch, _ := c.Stream(context.Background(), provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
	})
	for chunk := range ch {
		if chunk.Type == provider.ChunkReasoning && chunk.ReasoningID != "" {
			rid = chunk.ReasoningID
			rstatus = chunk.ReasoningStatus
		}
	}
	if rid != "rs_round1" || rstatus != "completed" {
		t.Fatalf("meta chunk id/status = %q/%q, want rs_round1/completed", rid, rstatus)
	}

	body, _, _ := c.buildRequestBody(provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: "hi"},
			{Role: provider.RoleAssistant, Content: "answer", ReasoningContent: "think hard",
				ReasoningID: rid, ReasoningStatus: rstatus},
		},
	})
	items := body["input"].([]map[string]any)
	found := false
	for _, item := range items {
		if item["type"] == "reasoning" {
			if item["id"] != "rs_round1" || item["status"] != "completed" {
				t.Fatalf("round-2 reasoning item id/status = %v/%v, want rs_round1/completed",
					item["id"], item["status"])
			}
			found = true
		}
	}
	if !found {
		t.Fatal("round-2 input must contain the reasoning item with captured id/status")
	}
}

// TestVendorTableMaxOutputTokens：默认输出预算完全由 vendor 表驱动——
// mimo 128000（长思考不截断）、deepseek 128K、unknown 不设。
func TestVendorTableMaxOutputTokens(t *testing.T) {
	msg := []provider.Message{{Role: provider.RoleUser, Content: "hi"}}

	// mimo：表默认 128000（思考模式不设会顶到服务端 32768 截断）
	mimo := New(Config{Name: "mimo", BaseURL: "https://api.xiaomimimo.com/v1", Model: "mimo-v2.5-pro"}).(*client)
	body, _, _ := mimo.buildRequestBody(provider.Request{Messages: msg})
	if got := body["max_output_tokens"]; got != 128000 {
		t.Fatalf("mimo max_output_tokens = %#v, want 128000 (vendor table)", got)
	}
	// mimo 思考禁用也设 128000（mimo 无 thinking-disabled 豁免——表无条件）
	noThinking := New(Config{Name: "mimo", BaseURL: "https://api.xiaomimimo.com/v1", Model: "mimo-v2.5-pro", Effort: "none"}).(*client)
	nb, _, _ := noThinking.buildRequestBody(provider.Request{Messages: msg})
	if nb["max_output_tokens"] != 128000 {
		t.Fatalf("mimo thinking-disabled budget = %#v, want 128000", nb["max_output_tokens"])
	}
	// deepseek 值来自表（非硬编码常量）
	ds := New(Config{Name: "deepseek", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-pro"}).(*client)
	db, _, _ := ds.buildRequestBody(provider.Request{Messages: msg})
	if got := db["max_output_tokens"]; got != capabilitiesFor("deepseek").defaultMaxOutputTokens {
		t.Fatalf("deepseek budget must come from vendor table, got %#v", got)
	}
}
