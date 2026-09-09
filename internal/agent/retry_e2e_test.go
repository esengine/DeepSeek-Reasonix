package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/provider/anthropic"
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
	if retries[0].RetryMax != maxStreamRecoveries {
		t.Errorf("RetryMax = %d, want %d", retries[0].RetryMax, maxStreamRecoveries)
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

		if requestNo == 1 {
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
	if len(gotBodies) != 2 {
		t.Fatalf("HTTP requests = %d, want malformed + recovery + final", len(gotBodies))
	}
	if !bytes.Contains(gotBodies[1], []byte(`"role":"tool"`)) {
		t.Fatalf("recovery request changed bytes:\nfirst=%s\nretry=%s", gotBodies[0], gotBodies[1])
	}
	var toolTurns int
	for _, message := range a.Session().Messages {
		if message.Role == provider.RoleAssistant && len(message.ToolCalls) > 0 {
			toolTurns++
			if message.ReasoningContent != "" {
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

// TestDeepSeekOpenAIReasoningReplay400RepairsOldHistory drives the OpenAI
// adapter through the shared stale-history recovery path. The first request
// replays an old assistant reasoning turn and is rejected; the repair retry
// strips only provider-visible reasoning while preserving canonical history.
func TestDeepSeekOpenAIReasoningReplay400RepairsOldHistory(t *testing.T) {
	var mu sync.Mutex
	var bodies [][]byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, append([]byte(nil), body...))
		requestNo := len(bodies)
		mu.Unlock()

		if requestNo == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":{"message":"The reasoning_content in the thinking mode must be passed back to the API"}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `data: {"choices":[{"delta":{"content":"recovered"},"finish_reason":"stop"}]}`+"\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	prov, err := openai.New(provider.Config{
		Name: "deepseek-openai", BaseURL: srv.URL, Model: "deepseek-v4-pro", APIKey: "k",
		Extra: map[string]any{"reasoning_protocol": "deepseek", "thinking": "enabled"},
	})
	if err != nil {
		t.Fatalf("New provider: %v", err)
	}
	session := NewSession("system")
	session.Add(provider.Message{Role: provider.RoleUser, Content: "earlier"})
	session.Add(provider.Message{Role: provider.RoleAssistant, Content: "old answer", ReasoningContent: "stale thinking"})
	sink := &recordSink{}
	a := New(prov, echoRegistry(), session, Options{}, sink)

	if err := a.Run(withNoClosedLoop(context.Background()), "next"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	mu.Lock()
	gotBodies := append([][]byte(nil), bodies...)
	mu.Unlock()
	if len(gotBodies) != 2 {
		t.Fatalf("HTTP requests = %d, want rejected attempt plus one repair retry", len(gotBodies))
	}
	if !bytes.Contains(gotBodies[0], []byte(`"reasoning_content":"stale thinking"`)) {
		t.Fatalf("first request did not replay old reasoning: %s", gotBodies[0])
	}
	if bytes.Contains(gotBodies[1], []byte("stale thinking")) || bytes.Contains(gotBodies[1], []byte("reasoning_content")) {
		t.Fatalf("repair retry still carries old reasoning: %s", gotBodies[1])
	}
	if !bytes.Contains(gotBodies[1], []byte("old answer")) {
		t.Fatalf("repair retry lost visible assistant text: %s", gotBodies[1])
	}
	var first, second map[string]json.RawMessage
	if err := json.Unmarshal(gotBodies[0], &first); err != nil {
		t.Fatalf("decode first request: %v", err)
	}
	if err := json.Unmarshal(gotBodies[1], &second); err != nil {
		t.Fatalf("decode repair request: %v", err)
	}
	delete(first, "messages")
	delete(second, "messages")
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("repair retry changed non-message fields:\nfirst=%s\nretry=%s", gotBodies[0], gotBodies[1])
	}
	for _, message := range session.Snapshot() {
		if message.Role == provider.RoleAssistant && message.Content == "old answer" && message.ReasoningContent != "stale thinking" {
			t.Fatalf("canonical history lost old reasoning: %+v", message)
		}
	}
	if got := sink.recoveryCount(event.ProtocolRecoveryReasoningReplay400Detected); got != 1 {
		t.Fatalf("reasoning_replay_400_detected audits = %d, want 1", got)
	}
	if got := sink.recoveryCount(event.ProtocolRecoveryReasoningReplay400Recovered); got != 1 {
		t.Fatalf("reasoning_replay_400_recovered audits = %d, want 1", got)
	}
}

func TestDeepSeekOpenAIReasoningReplay400StripsOldToolHistory(t *testing.T) {
	var mu sync.Mutex
	var bodies [][]byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, append([]byte(nil), body...))
		requestNo := len(bodies)
		mu.Unlock()
		if requestNo == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":{"message":"The reasoning_content in the thinking mode must be passed back to the API"}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `data: {"choices":[{"delta":{"content":"recovered"},"finish_reason":"stop"}]}`+"\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	prov, err := openai.New(provider.Config{
		Name: "deepseek-openai", BaseURL: srv.URL, Model: "deepseek-v4-pro", APIKey: "k",
		Extra: map[string]any{"reasoning_protocol": "deepseek", "thinking": "enabled"},
	})
	if err != nil {
		t.Fatalf("New provider: %v", err)
	}
	session := NewSession("system")
	session.Add(provider.Message{Role: provider.RoleUser, Content: "earlier"})
	session.Add(provider.Message{
		Role: provider.RoleAssistant, Content: "I will inspect the file", ReasoningContent: "stale thinking",
		ToolCalls: []provider.ToolCall{{ID: "old-call", Name: "read_file", Arguments: `{"path":"old.go"}`}},
	})
	session.Add(provider.Message{Role: provider.RoleTool, ToolCallID: "old-call", Name: "read_file", Content: "old result"})
	a := New(prov, echoRegistry(), session, Options{}, &recordSink{})
	if err := a.Run(withNoClosedLoop(context.Background()), "next"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	mu.Lock()
	gotBodies := append([][]byte(nil), bodies...)
	mu.Unlock()
	if len(gotBodies) != 2 {
		t.Fatalf("HTTP requests = %d, want rejected attempt plus one repair retry", len(gotBodies))
	}
	if !bytes.Contains(gotBodies[0], []byte("old-call")) || !bytes.Contains(gotBodies[0], []byte("stale thinking")) {
		t.Fatalf("first request did not contain old tool history: %s", gotBodies[0])
	}
	if bytes.Contains(gotBodies[1], []byte(`"tool_calls"`)) || bytes.Contains(gotBodies[1], []byte(`"role":"tool"`)) || bytes.Contains(gotBodies[1], []byte("stale thinking")) {
		t.Fatalf("repair retry retained stale tool history: %s", gotBodies[1])
	}
	if !bytes.Contains(gotBodies[1], []byte("old result")) {
		t.Fatal("repair lost the completed tool output")
	}
	if !bytes.Contains(gotBodies[1], []byte("I will inspect the file")) {
		t.Fatalf("repair retry lost visible old assistant text: %s", gotBodies[1])
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

// TestDeepSeekAnthropicThinking400CatchAndRepair drives the full self-heal
// against the real Anthropic-adapter wire shape: the first request replays the
// stored thinking block and the server rejects it with DeepSeek's documented
// 400; the agent must repair the projection once (stripping all reasoning),
// retry, and keep the strong projection for the following run.
func TestDeepSeekAnthropicThinking400CatchAndRepair(t *testing.T) {
	var mu sync.Mutex
	var bodies [][]byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, append([]byte(nil), body...))
		requestNo := len(bodies)
		mu.Unlock()

		if requestNo == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"The ` + "`content[].thinking`" + ` in the thinking mode must be passed back to the API","type":"invalid_request_error"}}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":20,\"output_tokens\":1}}}\n\n")
		if requestNo == 2 {
			_, _ = io.WriteString(w, "data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"thinking\",\"thinking\":\"\"}}\n\n")
			_, _ = io.WriteString(w, "data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"fresh thinking\"}}\n\n")
			_, _ = io.WriteString(w, "data: {\"type\":\"content_block_stop\",\"index\":0}\n\n")
		}
		_, _ = io.WriteString(w, "data: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"text_delta\",\"text\":\"repaired answer\"}}\n\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"content_block_stop\",\"index\":1}\n\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":5}}\n\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"message_stop\"}\n\n")
	}))
	defer srv.Close()

	prov, err := anthropic.New(provider.Config{
		Name: "deepseek-anthropic", BaseURL: srv.URL, Model: "deepseek-v4-flash", APIKey: "k",
		Extra: map[string]any{"reasoning_protocol": "deepseek", "thinking": "enabled"},
	})
	if err != nil {
		t.Fatalf("New provider: %v", err)
	}
	session := NewSession("system")
	session.Add(provider.Message{Role: provider.RoleUser, Content: "earlier"})
	session.Add(provider.Message{Role: provider.RoleAssistant, Content: "old answer", ReasoningContent: "stale thinking"})
	sink := &recordSink{}
	a := New(prov, echoRegistry(), session, Options{}, sink)

	if err := a.Run(withNoClosedLoop(context.Background()), "next"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	mu.Lock()
	gotBodies := append([][]byte(nil), bodies...)
	mu.Unlock()
	if len(gotBodies) != 2 {
		t.Fatalf("HTTP requests = %d, want rejected attempt plus one repair retry", len(gotBodies))
	}
	if !bytes.Contains(gotBodies[0], []byte(`"type":"thinking"`)) || !bytes.Contains(gotBodies[0], []byte("stale thinking")) {
		t.Fatalf("first request did not replay the stored thinking block: %s", gotBodies[0])
	}
	if bytes.Contains(gotBodies[1], []byte(`"type":"thinking"`)) || bytes.Contains(gotBodies[1], []byte("stale thinking")) {
		t.Fatalf("repair retry still carries thinking blocks: %s", gotBodies[1])
	}
	if !bytes.Contains(gotBodies[1], []byte("old answer")) {
		t.Fatalf("repair retry lost the visible assistant text: %s", gotBodies[1])
	}
	// Only Messages may change between the rejected request and its repair.
	var first, second map[string]json.RawMessage
	if err := json.Unmarshal(gotBodies[0], &first); err != nil || json.Unmarshal(gotBodies[1], &second) != nil {
		t.Fatalf("decode request bodies: %v", err)
	}
	delete(first, "messages")
	delete(second, "messages")
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("repair retry changed non-message fields:\nfirst=%s\nretry=%s", gotBodies[0], gotBodies[1])
	}
	if got := sink.recoveryCount(event.ProtocolRecoveryReasoningReplay400Detected); got != 1 {
		t.Fatalf("reasoning_replay_400_detected audits = %d, want 1", got)
	}
	if got := sink.recoveryCount(event.ProtocolRecoveryReasoningReplay400Recovered); got != 1 {
		t.Fatalf("reasoning_replay_400_recovered audits = %d, want 1", got)
	}
	var repairNotices int
	for _, e := range sink.kinds(event.Notice) {
		if e.Code == event.NoticeCodeReasoningReplayRepair && e.Level == event.LevelWarn {
			repairNotices++
		}
	}
	if repairNotices != 1 {
		t.Fatalf("repair notices = %d, want 1", repairNotices)
	}
	// The adopted answer streamed through and the canonical history keeps both
	// turns' reasoning untouched by the provider-visible repair.
	var answer strings.Builder
	for _, e := range sink.kinds(event.Text) {
		answer.WriteString(e.Text)
	}
	if answer.String() != "repaired answer" {
		t.Fatalf("streamed answer = %q, want the repaired response", answer.String())
	}

	// The next run keeps the repaired prefix stripped while replaying reasoning
	// from the newly committed assistant turn normally.
	if err := a.Run(withNoClosedLoop(context.Background()), "again"); err != nil {
		t.Fatalf("second Run: %v", err)
	}
	mu.Lock()
	third := append([]byte(nil), bodies[len(bodies)-1]...)
	total := len(bodies)
	mu.Unlock()
	if total != 3 {
		t.Fatalf("HTTP requests = %d, want one more for the follow-up run", total)
	}
	if bytes.Contains(third, []byte("stale thinking")) {
		t.Fatalf("strong projection retained stale reasoning in the next run: %s", third)
	}
	if !bytes.Contains(third, []byte("fresh thinking")) {
		t.Fatalf("strong projection dropped new-turn reasoning in the next run: %s", third)
	}
	for _, m := range session.Snapshot() {
		if m.Role == provider.RoleAssistant && m.Content == "old answer" && m.ReasoningContent != "stale thinking" {
			t.Fatalf("canonical history lost its reasoning: %+v", m)
		}
	}
}
