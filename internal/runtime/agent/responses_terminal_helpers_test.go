package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/model/responses"
	"reasonix/internal/state/sessionstore"
)

const terminalFinalAnswerSSE = "data: {\"type\":\"response.output_text.delta\",\"item_id\":\"msg_final\",\"content_index\":0,\"delta\":\"done\"}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_final\",\"status\":\"completed\",\"output\":[{\"id\":\"msg_final\",\"type\":\"message\",\"role\":\"assistant\",\"status\":\"completed\",\"content\":[{\"type\":\"output_text\",\"text\":\"done\",\"annotations\":[]}]}]}}\n\n"

const terminalSystemPrompt = "Stable system instructions for the Responses integrity fixture."

var terminalUnsafeMarkers = []string{"partial-text-marker", "private-reasoning-marker", "partial-arguments-marker"}

type terminalExpectation struct {
	requests             int
	calls                map[string]terminalCallExpectation
	finals               []string
	err                  error
	cause                provider.StreamFailureCause
	description          string
	retries              int
	exactRetry           bool
	explicitContinuation bool
	notice               string
	noNotices            bool
}

type terminalCallExpectation struct {
	name      string
	arguments string
	output    string
}

func runTerminalFixture(t *testing.T, events []string, want terminalExpectation) {
	t.Helper()
	var captured terminalRequests
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestNo := captured.add(t, r)
		w.Header().Set("Content-Type", "text/event-stream")
		if requestNo > 1 {
			_, _ = io.WriteString(w, terminalFinalAnswerSSE)
			return
		}
		for _, e := range events {
			_, _ = fmt.Fprintf(w, "data: %s\n\n", e)
		}
	}))
	defer srv.Close()
	sink := &recordSink{}
	a, executions := newTerminalAgent(srv.URL, sink)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := a.Run(ctx, "Run the required tool and report the result")
	if !errors.Is(err, want.err) {
		t.Errorf("Run error=%v, want %v", err, want.err)
	}
	if cause := provider.StreamFailureCauseOf(err); cause != want.cause {
		t.Errorf("stream failure=%q, want %q", cause, want.cause)
	}
	if want.err != nil {
		wrapped := fmt.Errorf("caller: %w", err)
		if !errors.Is(wrapped, want.err) || provider.StreamFailureCauseOf(wrapped) != want.cause {
			t.Errorf("wrapped failure lost identity: %v", wrapped)
		}
	}
	if provider.IsStreamInterrupted(err) {
		t.Errorf("unexpected retryable terminal error: %v", err)
	}
	bodies := captured.snapshot()
	if len(bodies) != want.requests {
		t.Errorf("requests=%d, want %d", len(bodies), want.requests)
	}
	retries := sink.kinds(event.Retrying)
	if len(retries) != want.retries {
		t.Errorf("automatic retries=%d, want %d", len(retries), want.retries)
	}
	for i, retry := range retries {
		if retry.RetryScope != event.RetryScopeStream || retry.RetryCause != provider.RetryCauseConnectionClosed || retry.RetryAttempt != i+1 {
			t.Errorf("retry=%+v, want stream connection-close retry %d", retry, i+1)
		}
	}
	if want.exactRetry && len(bodies) > 1 && !bytes.Equal(bodies[0], bodies[1]) {
		t.Errorf("EOF retry changed frozen request\nfirst=%s\nretry=%s", bodies[0], bodies[1])
	}
	assertTerminalExecutions(t, executions, want.calls)
	assertTerminalHistory(t, a.Session().Snapshot(), sink, want.calls, want.finals)
	assertTerminalNotices(t, sink, want)
	for _, body := range bodies[1:] {
		assertTerminalWireCalls(t, body, want.calls)
	}
	if !want.explicitContinuation {
		return
	}
	assertTerminalRecoveryMetadata(t, a.Session().Snapshot(), want.cause)
	if err := a.Run(ctx, "Continue if the task is still needed"); err != nil {
		t.Fatalf("explicit continuation: %v", err)
	}
	continued := captured.snapshot()
	if len(continued) != want.requests+1 {
		t.Fatalf("explicit continuation requests=%d, want %d", len(continued), want.requests+1)
	}
	if len(bodies) == 0 {
		t.Fatal("first request was never captured")
	}
	assertTerminalRecoveryRequest(t, bodies[0], continued[len(continued)-1], want)
	assertTerminalExecutions(t, executions, nil)
	assertTerminalHistory(t, a.Session().Snapshot(), sink, nil, []string{"done"})
	if len(sink.kinds(event.Retrying)) != 0 {
		t.Error("explicit continuation introduced an automatic retry")
	}
	if a.pendingInterruptedRecovery() != nil {
		t.Error("explicit user continuation did not consume pending recovery")
	}
}

type terminalRequests struct {
	mu     sync.Mutex
	bodies [][]byte
}

func (r *terminalRequests) add(t *testing.T, req *http.Request) int {
	t.Helper()
	body, err := io.ReadAll(req.Body)
	if err != nil {
		t.Errorf("read request: %v", err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.bodies = append(r.bodies, body)
	return len(r.bodies)
}

func (r *terminalRequests) snapshot() [][]byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([][]byte(nil), r.bodies...)
}

func newTerminalAgent(url string, sink event.Sink) (*Agent, *terminalExecutions) {
	executions := &terminalExecutions{calls: make(map[string]int)}
	reg := tool.NewRegistry()
	reg.Add(terminalEchoTool{executions: executions})
	reg.Add(terminalReadyTool{executions: executions})
	p := responses.New(responses.Config{Name: "local-terminal-fixture", APIKey: "fake-local-only", BaseURL: url, Model: "fixture-model", Mode: "stateless"})
	return New(p, reg, sessionstore.NewSession(terminalSystemPrompt), Options{}, sink), executions
}

func assertTerminalExecutions(t *testing.T, executions *terminalExecutions, want map[string]terminalCallExpectation) {
	t.Helper()
	expected := make(map[string]int, len(want))
	for _, call := range want {
		expected[call.name+"\x00"+call.arguments]++
	}
	executions.mu.Lock()
	defer executions.mu.Unlock()
	if !reflect.DeepEqual(executions.calls, expected) {
		t.Errorf("executions=%v, want %v", executions.calls, expected)
	}
}

func assertTerminalHistory(t *testing.T, messages []provider.Message, sink *recordSink, want map[string]terminalCallExpectation, wantFinals []string) {
	t.Helper()
	calls := map[string]int{}
	results := map[string]int{}
	var finals []string
	for _, m := range messages {
		if m.LocalOnly {
			continue
		}
		switch m.Role {
		case provider.RoleAssistant:
			finals = append(finals, m.Content)
			for _, call := range m.ToolCalls {
				calls[call.ID]++
				if expected, ok := want[call.ID]; !ok || call.Name != expected.name || call.Arguments != expected.arguments {
					t.Errorf("committed call=%+v, want %+v", call, expected)
				}
			}
		case provider.RoleTool:
			results[m.ToolCallID]++
			if expected, ok := want[m.ToolCallID]; !ok || m.Name != expected.name || m.Content != expected.output {
				t.Errorf("committed tool result=%+v, want %+v", m, expected)
			}
		}
	}
	if !reflect.DeepEqual(finals, wantFinals) {
		t.Errorf("committed assistant text=%q, want %q", finals, wantFinals)
	}
	eventCounts := map[string]int{}
	for _, e := range sink.kinds(event.ToolResult) {
		eventCounts[e.Tool.ID]++
		if expected, ok := want[e.Tool.ID]; !ok || e.Tool.Name != expected.name || e.Tool.Output != expected.output || e.Tool.Err != "" {
			t.Errorf("tool result event=%+v, want %+v", e.Tool, expected)
		}
	}
	for label, counts := range map[string]map[string]int{"history calls": calls, "history results": results, "result events": eventCounts} {
		if len(counts) != len(want) {
			t.Errorf("%s=%v, want %d distinct calls", label, counts, len(want))
		}
		for id := range want {
			if counts[id] != 1 {
				t.Errorf("%s for %q=%d, want 1", label, id, counts[id])
			}
		}
	}
}

func assertTerminalNotices(t *testing.T, sink *recordSink, want terminalExpectation) {
	t.Helper()
	var texts []string
	for _, e := range sink.kinds(event.Notice) {
		texts = append(texts, e.Text)
	}
	if want.noNotices && len(texts) != 0 {
		t.Errorf("unexpected notices=%q", texts)
	}
	if want.notice != "" && !strings.Contains(strings.Join(texts, "\n"), want.notice) {
		t.Errorf("notices=%q, missing %q", texts, want.notice)
	}
}

func assertTerminalRecoveryMetadata(t *testing.T, messages []provider.Message, want provider.StreamFailureCause) {
	t.Helper()
	var pending int
	for _, m := range messages {
		if m.InterruptedTurn == nil {
			continue
		}
		if !m.LocalOnly {
			t.Error("recovery metadata is not local-only")
		}
		if m.InterruptedTurn.Pending {
			pending++
			if m.InterruptedTurn.StreamFailure != want {
				t.Errorf("durable failure=%q, want %q", m.InterruptedTurn.StreamFailure, want)
			}
			if len(m.InterruptedTurn.CompletedTools) != 0 {
				t.Errorf("refused attempt claims completed tools: %+v", m.InterruptedTurn.CompletedTools)
			}
		}
	}
	if pending != 1 {
		t.Errorf("pending recovery records=%d, want 1", pending)
	}
}

type terminalRequest struct {
	Instructions json.RawMessage   `json:"instructions"`
	Tools        json.RawMessage   `json:"tools"`
	Input        []json.RawMessage `json:"input"`
}

type terminalInput struct {
	Type      string `json:"type"`
	Role      string `json:"role"`
	Content   string `json:"content"`
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
	Output    string `json:"output"`
}

func decodeTerminalRequest(t *testing.T, body []byte) terminalRequest {
	t.Helper()
	var request terminalRequest
	if err := json.Unmarshal(body, &request); err != nil {
		t.Fatalf("decode request: %v\n%s", err, body)
	}
	return request
}

func assertTerminalWireCalls(t *testing.T, body []byte, want map[string]terminalCallExpectation) {
	t.Helper()
	request := decodeTerminalRequest(t, body)
	calls, outputs := map[string]int{}, map[string]int{}
	for _, raw := range request.Input {
		var item terminalInput
		if err := json.Unmarshal(raw, &item); err != nil {
			t.Fatalf("decode input: %v", err)
		}
		switch item.Type {
		case "function_call":
			calls[item.CallID]++
			if expected, ok := want[item.CallID]; !ok || item.Name != expected.name || item.Arguments != expected.arguments {
				t.Errorf("continuation call=%+v, want %+v", item, expected)
			}
		case "function_call_output":
			outputs[item.CallID]++
			if expected, ok := want[item.CallID]; !ok || item.Output != expected.output {
				t.Errorf("continuation output=%+v, want %+v", item, expected)
			}
		}
	}
	for label, counts := range map[string]map[string]int{"calls": calls, "outputs": outputs} {
		if len(counts) != len(want) {
			t.Errorf("continuation %s=%v, want %d calls", label, counts, len(want))
		}
		for id := range want {
			if counts[id] != 1 {
				t.Errorf("continuation %s for %q=%d, want 1", label, id, counts[id])
			}
		}
	}
}

func assertTerminalRecoveryRequest(t *testing.T, firstBody, nextBody []byte, want terminalExpectation) {
	t.Helper()
	first := decodeTerminalRequest(t, firstBody)
	var instructions string
	if err := json.Unmarshal(first.Instructions, &instructions); err != nil || instructions != terminalSystemPrompt {
		t.Errorf("initial system instructions=%s, want %q", first.Instructions, terminalSystemPrompt)
	}
	if len(first.Tools) == 0 || bytes.Equal(first.Tools, []byte("[]")) {
		t.Error("initial tool schema prefix is empty")
	}
	next := decodeTerminalRequest(t, nextBody)
	if !bytes.Equal(first.Instructions, next.Instructions) || !bytes.Equal(first.Tools, next.Tools) {
		t.Error("protocol recovery changed cache-stable instructions or tool schemas")
	}
	if len(next.Input) != len(first.Input)+1 {
		t.Fatalf("continuation input items=%d, want original %d plus one user tail", len(next.Input), len(first.Input))
	}
	for i := range first.Input {
		if !bytes.Equal(first.Input[i], next.Input[i]) {
			t.Errorf("continuation changed prior input item %d", i)
		}
	}
	var tail terminalInput
	if err := json.Unmarshal(next.Input[len(next.Input)-1], &tail); err != nil {
		t.Fatal(err)
	}
	if tail.Role != "user" || !strings.Contains(tail.Content, "Continue if the task is still needed") {
		t.Errorf("recovery is not on next explicit user tail: %+v", tail)
	}
	for _, required := range []string{"<interrupted-turn-recovery>", "stream_failure: " + string(want.cause), want.description} {
		if !strings.Contains(tail.Content, required) {
			t.Errorf("recovery tail missing %q: %s", required, tail.Content)
		}
	}
	if strings.Count(string(nextBody), string(want.cause)) != 1 {
		t.Errorf("failure identity must appear once on user tail: %s", nextBody)
	}
	for _, marker := range terminalUnsafeMarkers {
		if strings.Contains(string(nextBody), marker) {
			t.Errorf("raw refused payload %q leaked to continuation", marker)
		}
	}
	if want.err != nil && strings.Contains(string(nextBody), want.err.Error()) {
		t.Error("raw error prose leaked instead of bounded cause projection")
	}
	assertTerminalWireCalls(t, nextBody, nil)
}

type terminalExecutions struct {
	mu    sync.Mutex
	calls map[string]int
}

func (e *terminalExecutions) record(name string, args json.RawMessage) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls[name+"\x00"+string(args)]++
}

type terminalEchoTool struct {
	echoTool
	executions *terminalExecutions
}

func (t terminalEchoTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	t.executions.record(t.Name(), args)
	return t.echoTool.Execute(ctx, args)
}

type terminalReadyTool struct{ executions *terminalExecutions }

func (terminalReadyTool) Name() string        { return "ready" }
func (terminalReadyTool) Description() string { return "report readiness without arguments" }
func (terminalReadyTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)
}
func (terminalReadyTool) ReadOnly() bool { return true }
func (t terminalReadyTool) Execute(_ context.Context, args json.RawMessage) (string, error) {
	t.executions.record(t.Name(), args)
	return "ready", nil
}

func runTerminalCancellationFixture(t *testing.T) {
	t.Helper()
	var captured terminalRequests
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.add(t, r)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial-text-marker\"}\n\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.output_item.added\",\"item\":{\"id\":\"fc_cancel\",\"type\":\"function_call\",\"call_id\":\"call_cancel\",\"name\":\"echo\"}}\n\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.function_call_arguments.delta\",\"item_id\":\"fc_cancel\",\"delta\":\"{\\\"text\\\":\\\"partial-arguments-marker\"}\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	sink := &terminalCancelSink{recordSink: &recordSink{}, cancel: cancel}
	a, executions := newTerminalAgent(srv.URL, sink)
	err := a.Run(ctx, "Run the required tool and report the result")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Run error=%v, want context.Canceled", err)
	}
	if provider.IsStreamInterrupted(err) || provider.StreamFailureCauseOf(err) != "" {
		t.Errorf("cancellation classified as transport/protocol failure: %v", err)
	}
	if !sink.sawPartial {
		t.Error("cancellation did not observe partial call arguments")
	}
	if len(captured.snapshot()) != 1 || len(sink.kinds(event.Retrying)) != 0 {
		t.Error("cancellation issued another request or automatic retry")
	}
	assertTerminalExecutions(t, executions, nil)
	assertTerminalHistory(t, a.Session().Snapshot(), sink.recordSink, nil, nil)
}

type terminalCancelSink struct {
	*recordSink
	cancel     context.CancelFunc
	sawPartial bool
}

func (s *terminalCancelSink) Emit(e event.Event) {
	s.recordSink.Emit(e)
	if e.Kind == event.ToolDispatch && e.Tool.Partial && e.Tool.ArgChars > 0 {
		s.sawPartial = true
		s.cancel()
	}
}
