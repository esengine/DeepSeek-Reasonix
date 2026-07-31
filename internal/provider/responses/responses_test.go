package responses

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"reasonix/internal/provider"
)

// mockResponsesServer serves canned Responses API SSE streams.
func mockResponsesServer(t *testing.T, events []string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			http.Error(w, "wrong path", http.StatusNotFound)
			return
		}
		if got := r.Header.Get("Authorization"); !strings.HasPrefix(got, "Bearer ") {
			http.Error(w, "missing auth", http.StatusUnauthorized)
			return
		}
		if got := r.Header.Get("x-dashscope-session-cache"); got != "enable" {
			http.Error(w, "missing session cache header", http.StatusBadRequest)
			return
		}
		fl := w.(http.Flusher)
		w.Header().Set("Content-Type", "text/event-stream")
		for _, e := range events {
			if _, err := w.Write([]byte("data:" + e + "\n\n")); err != nil {
				return
			}
			fl.Flush()
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func collect(t *testing.T, p provider.Provider, req provider.Request) []provider.Chunk {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ch, err := p.Stream(ctx, req)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var out []provider.Chunk
	for c := range ch {
		out = append(out, c)
	}
	return out
}

func TestResponsesStreamBasicText(t *testing.T) {
	srv := mockResponsesServer(t, []string{
		`{"type":"response.created","response":{"id":"resp_1"}}`,
		`{"type":"response.output_text.delta","delta":"Hello "}`,
		`{"type":"response.output_text.delta","delta":"world"}`,
		`{"type":"response.completed","response":{"id":"resp_1","usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15,"input_tokens_details":{"cached_tokens":8},"output_tokens_details":{"reasoning_tokens":2}}}}`,
	})

	p := New(Config{Name: "test", APIKey: "k", BaseURL: srv.URL, Model: "qwen3.7-plus", Stateful: boolPtr(true)})
	chunks := collect(t, p, provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleSystem, Content: "sys"},
			{Role: provider.RoleUser, Content: "hi"},
		},
	})

	// text deltas
	if len(chunks) < 2 || chunks[0].Type != provider.ChunkText || chunks[0].Text != "Hello " || chunks[1].Text != "world" {
		t.Fatalf("text chunks wrong: %+v", chunks)
	}
	// usage
	var usage *provider.Usage
	for _, c := range chunks {
		if c.Type == provider.ChunkUsage {
			usage = c.Usage
		}
	}
	if usage == nil {
		t.Fatal("no usage chunk")
	}
	if usage.PromptTokens != 10 || usage.CacheHitTokens != 8 || usage.CacheMissTokens != 2 || usage.ReasoningTokens != 2 {
		t.Fatalf("usage wrong: %+v", usage)
	}
	// done
	last := chunks[len(chunks)-1]
	if last.Type != provider.ChunkDone {
		t.Fatalf("last chunk = %v, want Done", last.Type)
	}
}

func TestResponsesStatefulUsesPreviousResponseID(t *testing.T) {
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		bodies = append(bodies, body)
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		// Echo the received previous_response_id back as the new response id.
		prev, _ := body["previous_response_id"].(string)
		w.Write([]byte(`data:{"type":"response.output_text.delta","delta":"ok"}` + "\n\n"))
		if prev != "" {
			w.Write([]byte(`data:{"type":"response.completed","response":{"id":"resp_next","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}` + "\n\n"))
		} else {
			w.Write([]byte(`data:{"type":"response.completed","response":{"id":"resp_first","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}` + "\n\n"))
		}
		fl.Flush()
	}))
	t.Cleanup(srv.Close)

	p := New(Config{Name: "test", APIKey: "k", BaseURL: srv.URL, Model: "qwen3.7-plus", Stateful: boolPtr(true)})

	// Turn 1: full input, no previous_response_id.
	collect(t, p, provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleSystem, Content: "sys"},
			{Role: provider.RoleUser, Content: "first"},
		},
	})
	if _, has := bodies[0]["previous_response_id"]; has {
		t.Fatal("first turn must not carry previous_response_id")
	}

	// Turn 2: only new user message + previous_response_id.
	collect(t, p, provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleSystem, Content: "sys"},
			{Role: provider.RoleUser, Content: "first"},
			{Role: provider.RoleAssistant, Content: "ok"},
			{Role: provider.RoleUser, Content: "second"},
		},
	})
	prev, has := bodies[1]["previous_response_id"].(string)
	if !has || prev != "resp_first" {
		t.Fatalf("turn 2 previous_response_id = %q (has=%v), want resp_first", prev, has)
	}
	input, _ := bodies[1]["input"].(string)
	if input != "second" {
		t.Fatalf("turn 2 input = %q, want only the new user message", input)
	}
}

func TestResponsesStatelessSendsFullInput(t *testing.T) {
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		bodies = append(bodies, body)
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		w.Write([]byte(`data:{"type":"response.output_text.delta","delta":"ok"}` + "\n\n"))
		w.Write([]byte(`data:{"type":"response.completed","response":{"id":"resp_1","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}` + "\n\n"))
		fl.Flush()
	}))
	t.Cleanup(srv.Close)

	p := New(Config{Name: "test", APIKey: "k", BaseURL: srv.URL, Model: "deepseek-v4-flash", Stateful: boolPtr(false)})

	collect(t, p, provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleSystem, Content: "sys"},
			{Role: provider.RoleUser, Content: "first"},
		},
	})
	collect(t, p, provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleSystem, Content: "sys"},
			{Role: provider.RoleUser, Content: "first"},
			{Role: provider.RoleAssistant, Content: "ok"},
			{Role: provider.RoleUser, Content: "second"},
		},
	})

	if _, has := bodies[1]["previous_response_id"]; has {
		t.Fatal("stateless provider must never send previous_response_id")
	}
	input, ok := bodies[1]["input"].([]any)
	if !ok || len(input) != 4 {
		t.Fatalf("stateless turn 2 input = %#v, want full 4-item array", bodies[1]["input"])
	}
}

func TestResponsesToolCallFunctionCallArguments(t *testing.T) {
	srv := mockResponsesServer(t, []string{
		`{"type":"response.output_item.added","item":{"id":"fc_1","type":"function_call","call_id":"call_1","name":"bash"}}`,
		`{"type":"response.function_call_arguments.delta","item_id":"call_1","delta":"{\"command\":"}`,
		`{"type":"response.function_call_arguments.delta","item_id":"call_1","delta":"\"ls\"}"}`,
		`{"type":"response.output_item.done","item":{"id":"fc_1","type":"function_call","call_id":"call_1","name":"bash","arguments":"{\"command\":\"ls\"}"}}`,
		`{"type":"response.completed","response":{"id":"resp_1","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`,
	})

	p := New(Config{Name: "test", APIKey: "k", BaseURL: srv.URL, Model: "deepseek-v4-flash", Stateful: boolPtr(false)})
	chunks := collect(t, p, provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "list files"}},
	})

	var start, delta, done *provider.Chunk
	for i := range chunks {
		switch chunks[i].Type {
		case provider.ChunkToolCallStart:
			start = &chunks[i]
		case provider.ChunkToolCallArgsDelta:
			delta = &chunks[i]
		case provider.ChunkToolCall:
			done = &chunks[i]
		}
	}
	if start == nil || start.ToolCall == nil || start.ToolCall.Name != "bash" || start.ToolCall.ID != "call_1" {
		t.Fatalf("tool start wrong: %+v", start)
	}
	if delta == nil || delta.ToolCall == nil || delta.ToolCall.ID != "call_1" || delta.ArgChars <= 0 {
		t.Fatalf("tool args delta wrong: %+v", delta)
	}
	if done == nil || done.ToolCall == nil || done.ToolCall.Arguments != `{"command":"ls"}` {
		t.Fatalf("tool done wrong: %+v", done)
	}
}

func TestResponsesFailedEventSurfacesError(t *testing.T) {
	srv := mockResponsesServer(t, []string{
		`{"type":"response.failed","response":{"id":"resp_1","error":{"message":"model exploded"}}}`,
	})

	p := New(Config{Name: "test", APIKey: "k", BaseURL: srv.URL, Model: "qwen3.7-plus", Stateful: boolPtr(true)})
	chunks := collect(t, p, provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
	})

	var errChunk *provider.Chunk
	for i := range chunks {
		if chunks[i].Type == provider.ChunkError {
			errChunk = &chunks[i]
		}
	}
	if errChunk == nil || !strings.Contains(errChunk.Err.Error(), "model exploded") {
		t.Fatalf("failed event error wrong: %+v", errChunk)
	}
}

func TestResponsesReasoningDialects(t *testing.T) {
	// Both dialects must map to ChunkReasoning.
	for _, ev := range []string{
		`{"type":"response.reasoning_summary_text.delta","delta":"thinking..."}`,
		`{"type":"response.reasoning_text.delta","delta":"thinking..."}`,
	} {
		srv := mockResponsesServer(t, []string{
			ev,
			`{"type":"response.completed","response":{"id":"resp_1","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`,
		})
		p := New(Config{Name: "test", APIKey: "k", BaseURL: srv.URL, Model: "m", Stateful: boolPtr(true)})
		chunks := collect(t, p, provider.Request{
			Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
		})
		found := false
		for _, c := range chunks {
			if c.Type == provider.ChunkReasoning && c.Text == "thinking..." {
				found = true
			}
		}
		if !found {
			t.Fatalf("event %q did not produce ChunkReasoning: %+v", ev, chunks)
		}
	}
}

func boolPtr(v bool) *bool { return &v }


func TestDetectVendorAndModeDefaults(t *testing.T) {
	cases := []struct {
		baseURL string
		vendor  string
		mode    string // default mode when nothing explicit
	}{
		{"https://token-plan.cn-beijing.maas.aliyuncs.com/compatible-mode/v1", "dashscope", "stateful"},
		{"https://dashscope.aliyuncs.com/compatible-mode/v1", "dashscope", "stateful"},
		{"https://api.deepseek.com", "deepseek", "stateless"},
		{"https://api.minimaxi.com/v1", "minimax", "stateful"},
		{"https://ark.cn-beijing.volces.com/api/v3", "volcano", "stateful"},
		{"https://unknown.example.com/v1", "", "stateful"},
	}
	for _, c := range cases {
		if got := DetectVendor(c.baseURL); got != c.vendor {
			t.Errorf("DetectVendor(%q) = %q, want %q", c.baseURL, got, c.vendor)
		}
		if got := (Config{BaseURL: c.baseURL}).mode(); got != c.mode {
			t.Errorf("default mode for %q = %q, want %q", c.baseURL, got, c.mode)
		}
	}
	// Explicit config wins over vendor detection.
	if got := (Config{BaseURL: "https://api.deepseek.com", Mode: "stateful"}).mode(); got != "stateful" {
		t.Errorf("explicit Mode should win, got %q", got)
	}
	if got := (Config{BaseURL: "https://token-plan.cn-beijing.maas.aliyuncs.com/compatible-mode/v1", Stateful: boolPtr(false)}).mode(); got != "stateless" {
		t.Errorf("explicit Stateful=false should win, got %q", got)
	}
}
