package agent

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/provider/anthropic"
)

const missingReasoningToolSSE = `data: {"type":"message_start","message":{"usage":{"input_tokens":10}}}

data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_1","name":"echo"}}

data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"text\":\"hi\"}"}}

data: {"type":"content_block_stop","index":0}

data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":8}}

data: {"type":"message_stop"}

`

const recoveredReasoningToolSSE = `data: {"type":"message_start","message":{"usage":{"input_tokens":10}}}

data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking"}}

data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"call echo safely"}}

data: {"type":"content_block_stop","index":0}

data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_1","name":"echo"}}

data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"text\":\"hi\"}"}}

data: {"type":"content_block_stop","index":1}

data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":12}}

data: {"type":"message_stop"}

`

const finalAnswerSSE = `data: {"type":"message_start","message":{"usage":{"input_tokens":20}}}

data: {"type":"content_block_start","index":0,"content_block":{"type":"text"}}

data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"done"}}

data: {"type":"content_block_stop","index":0}

data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}

data: {"type":"message_stop"}

`

func TestOpenCodeGoAnthropicMissingReasoningRecoversBeforeToolExecution(t *testing.T) {
	var mu sync.Mutex
	var bodies [][]byte
	responses := []string{missingReasoningToolSSE, recoveredReasoningToolSSE, finalAnswerSSE}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		mu.Lock()
		bodies = append(bodies, body)
		i := len(bodies) - 1
		mu.Unlock()
		if i >= len(responses) {
			t.Errorf("unexpected request %d", i+1)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, responses[i])
	}))
	defer srv.Close()

	prov, err := anthropic.New(provider.Config{
		Name: "opencode-go-deepseek", BaseURL: srv.URL, Model: "deepseek-v4-flash", APIKey: "test-key",
		Extra: map[string]any{"reasoning_protocol": "deepseek", "thinking": "adaptive", "effort": "high", "web_search": true},
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	sink := &recordSink{}
	stateDir := t.TempDir()
	agent := New(prov, echoRegistry(), NewSession(""), Options{MissingReasoningWarnStateDir: stateDir}, sink)
	if err := agent.Run(withNoClosedLoop(context.Background()), "go"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 3 {
		t.Fatalf("HTTP requests = %d, want malformed turn, exact retry, and final turn", len(bodies))
	}
	if !bytes.Equal(bodies[0], bodies[1]) {
		t.Fatal("missing-reasoning recovery did not retry the exact frozen request")
	}
	for _, wire := range [][]byte{
		[]byte(`"thinking":{"type":"enabled"}`),
		[]byte(`"output_config":{"effort":"high"}`),
		[]byte(`{"type":"web_search_20250305","name":"web_search"}`),
	} {
		if !bytes.Contains(bodies[0], wire) {
			t.Fatalf("OpenCode Go preset request is missing %s", wire)
		}
	}
	if got := sink.recoveryCount(event.ProtocolRecoveryMissingReasoningRetryAttempted); got != 1 {
		t.Fatalf("missing-reasoning retries = %d, want 1", got)
	}
	if got := len(sink.kinds(event.ToolResult)); got != 1 {
		t.Fatalf("tool results = %d, want exactly one execution after recovery", got)
	}
}

func TestOpenCodeGoAnthropicRepeatedMissingReasoningStopsAfterOneExactRetry(t *testing.T) {
	var mu sync.Mutex
	var bodies [][]byte
	responses := []string{missingReasoningToolSSE, missingReasoningToolSSE}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		mu.Lock()
		bodies = append(bodies, body)
		i := len(bodies) - 1
		mu.Unlock()
		if i >= len(responses) {
			t.Errorf("unexpected request %d", i+1)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, responses[i])
	}))
	defer srv.Close()

	prov, err := anthropic.New(provider.Config{
		Name: "opencode-go-deepseek", BaseURL: srv.URL, Model: "deepseek-v4-flash", APIKey: "test-key",
		Extra: map[string]any{"reasoning_protocol": "deepseek", "thinking": "adaptive", "effort": "high", "web_search": true},
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	sink := &recordSink{}
	agent := New(prov, echoRegistry(), NewSession(""), Options{}, sink)
	var replayErr *ReasoningReplayError
	if err := agent.Run(withNoClosedLoop(context.Background()), "go"); !errors.As(err, &replayErr) {
		t.Fatalf("Run error = %v, want ReasoningReplayError", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 2 {
		t.Fatalf("HTTP requests = %d, want one original request and one exact retry", len(bodies))
	}
	if !bytes.Equal(bodies[0], bodies[1]) {
		t.Fatal("protocol retry changed the frozen request")
	}
	if bytes.Contains(bodies[1], []byte(`"thinking":{"type":"disabled"}`)) {
		t.Fatal("repeated missing reasoning entered a disabled-thinking fallback")
	}
	if got := sink.recoveryCount(event.ProtocolRecoveryMissingReasoningRetryAttempted); got != 1 {
		t.Fatalf("missing-reasoning retries = %d, want one", got)
	}
}
