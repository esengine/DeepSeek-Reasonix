package agent

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/provider/openai"
	"reasonix/internal/tool"
)

type recordSink struct {
	mu       sync.Mutex
	evs      []event.Event
	recovery []event.ProtocolRecoveryAudit
}

type textSignalSink struct {
	*recordSink
	textSeen chan struct{}
	once     sync.Once
}

func (s *textSignalSink) Emit(e event.Event) {
	s.recordSink.Emit(e)
	if e.Kind == event.Text {
		s.once.Do(func() { close(s.textSeen) })
	}
}

func (s *recordSink) Emit(e event.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.evs = append(s.evs, e)
}

func (s *recordSink) kinds(k event.Kind) []event.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []event.Event
	for _, e := range s.evs {
		if e.Kind == k {
			out = append(out, e)
		}
	}
	return out
}

func (s *recordSink) RecordProtocolRecovery(a event.ProtocolRecoveryAudit) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recovery = append(s.recovery, a)
}

func (s *recordSink) recoveryCount(kind event.ProtocolRecoveryKind) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	var count int
	for _, audit := range s.recovery {
		if audit.Kind == kind {
			count++
		}
	}
	return count
}

// TestAgentEmitsRetryingThenStreams drives the whole chain end-to-end: a real
// OpenAI-compatible provider hits an httptest server that returns 503 twice then
// a valid SSE stream. The agent must emit a Retrying event per backoff (so the
// composer can show "retrying n/m") and still deliver the streamed answer.
func TestAgentEmitsRetryingThenStreams(t *testing.T) {
	var reqs int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqs++
		if reqs <= 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":"overloaded"}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hi there\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()

	prov, err := openai.New(provider.Config{Name: "deepseek", BaseURL: srv.URL, Model: "deepseek-v4", APIKey: "k"})
	if err != nil {
		t.Fatalf("New provider: %v", err)
	}

	sink := &recordSink{}
	a := New(prov, tool.NewRegistry(), NewSession(""), Options{}, sink)
	if err := a.Run(context.Background(), "hi"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	retries := sink.kinds(event.Retrying)
	if len(retries) != 2 || retries[0].RetryAttempt != 1 || retries[1].RetryAttempt != 2 {
		t.Fatalf("want two Retrying events (1,2), got %+v", retries)
	}
	if retries[0].RetryMax != provider.MaxRetries {
		t.Errorf("RetryMax = %d, want %d", retries[0].RetryMax, provider.MaxRetries)
	}

	var answer strings.Builder
	for _, e := range sink.kinds(event.Text) {
		answer.WriteString(e.Text)
	}
	if !strings.Contains(answer.String(), "hi there") {
		t.Errorf("streamed answer = %q, want it to contain %q", answer.String(), "hi there")
	}
}

// TestDeepSeekFlashMissingReasoningRecoveryWithRealSSE exercises the actual
// OpenAI-compatible decoder shape used by the official Flash endpoint. The
// first response emits a tool call without reasoning_content; the second exact
// request includes it; only the adopted call reaches the session and UI.
func TestDeepSeekFlashMissingReasoningRecoveryWithRealSSE(t *testing.T) {
	var mu sync.Mutex
	var bodies [][]byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, append([]byte(nil), body...))
		requestNo := len(bodies)
		mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		if requestNo == 2 {
			_, _ = io.WriteString(w, `data: {"choices":[{"delta":{"reasoning_content":"call echo safely"}}]}`+"\n\n")
		}
		if requestNo <= 2 {
			_, _ = io.WriteString(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"echo","arguments":"{\"text\":\"hi\"}"}}]},"finish_reason":"tool_calls"}]}`+"\n\n")
			_, _ = io.WriteString(w, `data: {"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12,"prompt_cache_hit_tokens":10,"prompt_cache_miss_tokens":0}}`+"\n\n")
			_, _ = io.WriteString(w, "data: [DONE]\n\n")
			return
		}
		_, _ = io.WriteString(w, `data: {"choices":[{"delta":{"content":"done"},"finish_reason":"stop"}]}`+"\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	prov, err := openai.New(provider.Config{
		Name: "deepseek", BaseURL: srv.URL, Model: "deepseek-v4-flash", APIKey: "k",
		Extra: map[string]any{"reasoning_protocol": "deepseek", "thinking": "enabled"},
	})
	if err != nil {
		t.Fatalf("New provider: %v", err)
	}
	sink := &recordSink{}
	a := New(prov, echoRegistry(), NewSession(""), Options{}, sink)
	if err := a.Run(context.Background(), "go"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	mu.Lock()
	gotBodies := append([][]byte(nil), bodies...)
	mu.Unlock()
	if len(gotBodies) != 3 {
		t.Fatalf("HTTP requests = %d, want malformed + recovery + final", len(gotBodies))
	}
	if !bytes.Equal(gotBodies[0], gotBodies[1]) {
		t.Fatalf("recovery request changed bytes:\nfirst=%s\nretry=%s", gotBodies[0], gotBodies[1])
	}
	var toolTurns int
	for _, message := range a.Session().Messages {
		if message.Role == provider.RoleAssistant && len(message.ToolCalls) > 0 {
			toolTurns++
			if message.ReasoningContent != "call echo safely" {
				t.Fatalf("adopted reasoning = %q", message.ReasoningContent)
			}
		}
	}
	if toolTurns != 1 {
		t.Fatalf("saved tool turns = %d, want 1", toolTurns)
	}
	// One partial dispatch from the adopted SSE plus one full execution
	// dispatch. The discarded malformed stream must not add a third card.
	if got := len(sink.kinds(event.ToolDispatch)); got != 2 {
		t.Fatalf("tool dispatch events = %d, want adopted partial + full", got)
	}
	for _, notice := range sink.kinds(event.Notice) {
		if strings.Contains(notice.Text, "reasoning_content") || strings.Contains(notice.Detail, "reasoning_content") {
			t.Fatalf("protocol warning leaked to UI: %+v", notice)
		}
	}
}

func TestGLMToolTurnWithoutReasoningContinuesWithoutRecovery(t *testing.T) {
	var mu sync.Mutex
	var bodies [][]byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, append([]byte(nil), body...))
		requestNo := len(bodies)
		mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		if requestNo == 1 {
			_, _ = io.WriteString(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"echo","arguments":"{\"text\":\"hi\"}"}}]},"finish_reason":"tool_calls"}]}`+"\n\n")
		} else {
			_, _ = io.WriteString(w, `data: {"choices":[{"delta":{"content":"done"},"finish_reason":"stop"}]}`+"\n\n")
		}
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	prov, err := openai.New(provider.Config{
		Name: "glm", BaseURL: srv.URL, Model: "glm-5.2", APIKey: "k",
		Extra: map[string]any{"reasoning_protocol": "glm"},
	})
	if err != nil {
		t.Fatalf("New provider: %v", err)
	}
	sink := &recordSink{}
	a := New(prov, echoRegistry(), NewSession(""), Options{}, sink)
	if err := a.Run(context.Background(), "go"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	mu.Lock()
	requestBodies := append([][]byte(nil), bodies...)
	mu.Unlock()
	if len(requestBodies) != 2 {
		t.Fatalf("HTTP requests = %d, want tool turn and final turn without recovery", len(requestBodies))
	}
	if !bytes.Contains(requestBodies[1], []byte(`"reasoning_content":""`)) {
		t.Fatal("GLM replay did not preserve the empty reasoning_content field required for tool history")
	}
	if got := sink.recoveryCount(event.ProtocolRecoveryMissingReasoningRetryAttempted); got != 0 {
		t.Fatalf("missing-reasoning retries = %d, want 0", got)
	}
	if got := len(sink.kinds(event.ToolResult)); got != 1 {
		t.Fatalf("tool results = %d, want 1", got)
	}
}

func TestGLMTextWithoutReasoningStreamsBeforeResponseCompletes(t *testing.T) {
	responseStarted := make(chan struct{})
	releaseResponse := make(chan struct{})
	var releaseOnce sync.Once

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `data: {"choices":[{"delta":{"content":"streamed"}}]}`+"\n\n")
		w.(http.Flusher).Flush()
		close(responseStarted)
		<-releaseResponse
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { releaseOnce.Do(func() { close(releaseResponse) }) })

	prov, err := openai.New(provider.Config{
		Name: "glm", BaseURL: srv.URL, Model: "glm-5.2", APIKey: "k",
		Extra: map[string]any{"reasoning_protocol": "glm"},
	})
	if err != nil {
		t.Fatalf("New provider: %v", err)
	}
	sink := &textSignalSink{recordSink: &recordSink{}, textSeen: make(chan struct{})}
	a := New(prov, tool.NewRegistry(), NewSession(""), Options{}, sink)
	done := make(chan error, 1)
	go func() { done <- a.Run(withNoClosedLoop(context.Background()), "reply with streamed") }()

	<-responseStarted
	select {
	case <-sink.textSeen:
	case err := <-done:
		t.Fatalf("Run completed before the held response was released: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("GLM text stayed buffered until the response completed")
	}
	releaseOnce.Do(func() { close(releaseResponse) })
	if err := <-done; err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func TestGLMReasoningOverflowFailsBeforeToolExecution(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestNo := requests.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		if requestNo == 1 {
			_, _ = io.WriteString(w, `data: {"choices":[{"delta":{"reasoning_content":"`+strings.Repeat("reason", 16)+`"}}]}`+"\n\n")
			_, _ = io.WriteString(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"echo","arguments":"{\"text\":\"must not run\"}"}}]},"finish_reason":"tool_calls"}]}`+"\n\n")
		} else {
			_, _ = io.WriteString(w, `data: {"choices":[{"delta":{"content":"unexpected continuation"},"finish_reason":"stop"}]}`+"\n\n")
		}
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	prov, err := openai.New(provider.Config{
		Name: "glm", BaseURL: srv.URL, Model: "glm-5.2", APIKey: "k",
		Extra: map[string]any{"reasoning_protocol": "glm"},
	})
	if err != nil {
		t.Fatalf("New provider: %v", err)
	}
	sink := &recordSink{}
	a := New(prov, echoRegistry(), NewSession(""), Options{ReasoningByteLimit: 16}, sink)
	var replayErr *ReasoningReplayError
	if err := a.Run(withNoClosedLoop(context.Background()), "go"); !errors.As(err, &replayErr) || replayErr.Kind != ReasoningReplayOverflow {
		t.Fatalf("Run error = %v, want ReasoningReplayOverflow", err)
	}
	if got := len(sink.kinds(event.ToolResult)); got != 0 {
		t.Fatalf("tool results = %d, want 0 after incomplete GLM reasoning", got)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("HTTP requests = %d, want no continuation after incomplete GLM reasoning", got)
	}
}
