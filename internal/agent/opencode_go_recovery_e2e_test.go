package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"

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
	fingerprint := provider.MissingToolCallReasoningWarningFingerprint(prov)
	if newMissingReasoningWarnState(stateDir).fallbackActiveAt(fingerprint, time.Now()) {
		t.Fatal("one recovered omission incorrectly opened the disabled-thinking fallback circuit")
	}
}

func TestOpenCodeGoAnthropicRepeatedMissingReasoningUsesDisabledThinkingFallback(t *testing.T) {
	var mu sync.Mutex
	var bodies [][]byte
	responses := []string{missingReasoningToolSSE, missingReasoningToolSSE, missingReasoningToolSSE, finalAnswerSSE}
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
	if len(bodies) != 4 {
		t.Fatalf("HTTP requests = %d, want original, exact replay, fallback, and final turn", len(bodies))
	}
	if !bytes.Equal(bodies[0], bodies[1]) {
		t.Fatal("first recovery changed the frozen request instead of preserving cache bytes")
	}
	if !bytes.Contains(bodies[2], []byte(`"thinking":{"type":"disabled"}`)) || bytes.Contains(bodies[2], []byte(`"output_config"`)) {
		t.Fatalf("fallback request did not use the bounded disabled-thinking shape: %s", bodies[2])
	}
	if !bytes.Contains(bodies[3], []byte(`"thinking":{"type":"disabled"}`)) ||
		!bytes.Contains(bodies[3], []byte(`"type":"tool_use"`)) || !bytes.Contains(bodies[3], []byte(`"type":"tool_result"`)) {
		t.Fatalf("fallback continuation did not preserve the tool loop: %s", bodies[3])
	}
	var normal, fallback map[string]any
	if err := json.Unmarshal(bodies[0], &normal); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(bodies[2], &fallback); err != nil {
		t.Fatal(err)
	}
	delete(normal, "thinking")
	delete(normal, "output_config")
	delete(fallback, "thinking")
	delete(fallback, "output_config")
	if !reflect.DeepEqual(normal, fallback) {
		t.Fatal("fallback changed provider-visible request fields beyond the declared thinking controls")
	}
	if got := len(sink.kinds(event.ToolResult)); got != 1 {
		t.Fatalf("tool results = %d, want exactly one execution from the fallback response", got)
	}
	fingerprint := provider.MissingToolCallReasoningWarningFingerprint(prov)
	if !newMissingReasoningWarnState(stateDir).fallbackActiveAt(fingerprint, time.Now()) {
		t.Fatal("repeated omissions did not persist the fallback circuit")
	}
}

func TestOpenCodeGoAnthropicHalfOpenProbeFailsDirectlyToStableFallback(t *testing.T) {
	var mu sync.Mutex
	var bodies [][]byte
	responses := []string{missingReasoningToolSSE, missingReasoningToolSSE, finalAnswerSSE}
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
	stateDir := t.TempDir()
	state := newMissingReasoningWarnState(stateDir)
	fingerprint := provider.MissingToolCallReasoningWarningFingerprint(prov)
	openedAt := time.Now().Add(-missingReasoningFallbackBackoffs[0] - 2*time.Second)
	if !state.claimAt(fingerprint, openedAt) || !state.openFallbackAt(fingerprint, openedAt.Add(time.Second)) {
		t.Fatal("failed to seed due half-open circuit")
	}
	sink := &recordSink{}
	agent := New(prov, echoRegistry(), NewSession(""), Options{MissingReasoningWarnStateDir: stateDir}, sink)
	if err := agent.Run(withNoClosedLoop(context.Background()), "go"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 3 {
		t.Fatalf("HTTP requests = %d, want half-open probe, direct fallback, and final turn", len(bodies))
	}
	if !bytes.Contains(bodies[0], []byte(`"thinking":{"type":"enabled"}`)) ||
		!bytes.Contains(bodies[0], []byte(`"output_config":{"effort":"high"}`)) {
		t.Fatalf("half-open probe did not restore the normal thinking shape: %s", bodies[0])
	}
	if !bytes.Contains(bodies[1], []byte(`"thinking":{"type":"disabled"}`)) || bytes.Contains(bodies[1], []byte(`"output_config"`)) {
		t.Fatalf("failed probe did not switch directly to disabled thinking: %s", bodies[1])
	}
	if !bytes.Contains(bodies[2], []byte(`"thinking":{"type":"disabled"}`)) ||
		!bytes.Contains(bodies[2], []byte(`"type":"tool_use"`)) || !bytes.Contains(bodies[2], []byte(`"type":"tool_result"`)) {
		t.Fatalf("fallback continuation did not keep a stable tool loop: %s", bodies[2])
	}
	var normal, fallback map[string]any
	if err := json.Unmarshal(bodies[0], &normal); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(bodies[1], &fallback); err != nil {
		t.Fatal(err)
	}
	delete(normal, "thinking")
	delete(normal, "output_config")
	delete(fallback, "thinking")
	delete(fallback, "output_config")
	if !reflect.DeepEqual(normal, fallback) {
		t.Fatal("half-open transition changed request fields beyond declared thinking controls")
	}
	if got := sink.recoveryCount(event.ProtocolRecoveryMissingReasoningRetryAttempted); got != 0 {
		t.Fatalf("half-open exact retries = %d, want 0", got)
	}
	incidents, err := state.load(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if incident := incidents[fingerprint]; incident.FallbackLevel != 2 ||
		time.Unix(0, incident.NextProbeAtUnixNano).Sub(time.Unix(0, incident.FallbackAtUnixNano)) != missingReasoningFallbackBackoffs[1] {
		t.Fatalf("failed half-open incident = %+v, want level 2", incident)
	}
}
