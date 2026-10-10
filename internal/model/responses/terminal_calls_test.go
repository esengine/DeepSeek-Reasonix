package responses

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"reasonix/internal/contract/provider"
)

func TestTerminalCallsRejectUnsafeCompletion(t *testing.T) {
	for _, tc := range []struct{ name, output string }{
		{"arguments object", `{"type":"function_call","call_id":"call_1","name":"echo","arguments":{"secret":"must not appear"}}`},
		{"arguments null", `{"type":"function_call","call_id":"call_1","name":"echo","arguments":null}`},
		{"arguments missing", `{"type":"function_call","call_id":"call_1","name":"echo"}`},
		{"missing call id", `{"type":"function_call","name":"echo","arguments":"{}"}`},
		{"missing name", `{"type":"function_call","call_id":"call_1","arguments":"{}"}`},
		{"in progress", `{"type":"function_call","call_id":"call_1","name":"echo","status":"in_progress","arguments":"{}"}`},
		{"incomplete", `{"type":"function_call","call_id":"call_1","name":"echo","status":"incomplete","arguments":"{}"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertTerminalFailure(t, chunksOf(t, `{"type":"response.completed","response":{"id":"r","output":[`+tc.output+`]}}`), provider.ErrInvalidFunctionCall)
		})
	}
}

func assertTerminalFailure(t *testing.T, chunks []provider.Chunk, want error) {
	t.Helper()
	failed := false
	for _, chunk := range chunks {
		switch chunk.Type {
		case provider.ChunkError:
			failed = true
			if !errors.Is(chunk.Err, want) || !errors.Is(fmt.Errorf("consumer: %w", chunk.Err), want) {
				t.Errorf("error=%v want identity=%v", chunk.Err, want)
			}
			if provider.IsStreamInterrupted(chunk.Err) {
				t.Error("protocol failure became retryable interruption")
			}
			if strings.Contains(chunk.Err.Error(), "secret") {
				t.Error("error leaked arguments")
			}
		case provider.ChunkDone:
			t.Error("refused response emitted done")
		}
	}
	if !failed {
		t.Fatal("unsafe response succeeded")
	}
}

func TestTerminalCallsRejectUnfinishedCalls(t *testing.T) {
	for _, terminal := range []string{"[DONE]", `{"type":"response.completed","response":{"id":"r","output":[]}}`, `{"type":"response.completed"}`} {
		for _, events := range [][]string{
			{`{"type":"response.output_item.added","item":{"id":"fc_1","type":"function_call","call_id":"call_1","name":"echo"}}`},
			{`{"type":"response.output_item.added","item":{"id":"fc_1","type":"function_call"}}`},
			{`{"type":"response.function_call_arguments.done","item_id":"fc_1","arguments":"{}"}`},
			{`{"type":"response.function_call_arguments.delta","delta":"{}"}`},
		} {
			assertTerminalFailure(t, chunksOf(t, append(events, terminal)...), provider.ErrUnfinishedFunctionCall)
		}
	}
}

func TestTerminalCallsRecoverAlternateItemIdentity(t *testing.T) {
	chunks := chunksOf(t,
		`{"type":"response.output_item.added","item":{"id":"old","type":"function_call","call_id":"call_1","name":"echo"}}`,
		`{"type":"response.function_call_arguments.delta","item_id":"orphan","delta":"{}"}`,
		`{"type":"response.completed","response":{"id":"r","output":[{"type":"function_call","id":"new","call_id":"call_1","name":"echo","arguments":"{}"}]}}`)
	calls := 0
	done := false
	for _, chunk := range chunks {
		if chunk.Type == provider.ChunkError {
			t.Fatal(chunk.Err)
		}
		if chunk.Type == provider.ChunkDone {
			done = true
		}
		if chunk.Type == provider.ChunkToolCall {
			calls++
			if chunk.ToolCall.ID != "call_1" {
				t.Errorf("call=%+v", chunk.ToolCall)
			}
		}
	}
	if calls != 1 || !done {
		t.Fatalf("calls=%d done=%v", calls, done)
	}
}

func TestTerminalCallsKeepTextOnlyDONE(t *testing.T) {
	done := false
	for _, chunk := range chunksOf(t, `{"type":"response.output_text.delta","delta":"I will open the browser"}`, "[DONE]") {
		if chunk.Type == provider.ChunkError {
			t.Fatal(chunk.Err)
		}
		done = done || chunk.Type == provider.ChunkDone
	}
	if !done {
		t.Fatal("text-only DONE compatibility lost")
	}
}

func TestTerminalCallsLeaveArgumentJSONToToolValidation(t *testing.T) {
	if err := (&turn{}).terminalOutputError([]json.RawMessage{json.RawMessage(`{"type":"function_call","call_id":"call_1","name":"echo","arguments":"{"}`)}); err != nil {
		t.Fatal(err)
	}
}

func TestTerminalCallsCanceledSendDoesNotBlock(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	turn := newTurn(&client{}, make(chan provider.Chunk))
	if turn.refuseCalls(ctx, provider.ErrUnfinishedFunctionCall) {
		t.Fatal("refusal continued")
	}
}

func TestTerminalCallsRefusalClearsStatefulContinuation(t *testing.T) {
	for _, bad := range []struct {
		name   string
		events []string
		cause  error
	}{
		{"invalid", []string{`{"type":"response.completed","response":{"id":"poisoned","output":[{"type":"function_call","call_id":"c","name":"echo","arguments":{}}]}}`}, provider.ErrInvalidFunctionCall},
		{"unfinished", []string{`{"type":"response.output_item.added","item":{"type":"function_call","id":"f","call_id":"c","name":"echo"}}`, `{"type":"response.completed","response":{"id":"poisoned"}}`}, provider.ErrUnfinishedFunctionCall},
	} {
		t.Run(bad.name, func(t *testing.T) {
			var attempt atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n := attempt.Add(1)
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if n == 2 && body["previous_response_id"] != "good" {
					t.Errorf("fixture did not reach stateful continuation: %v", body)
				}
				if n == 3 {
					if _, ok := body["previous_response_id"]; ok {
						t.Errorf("refused continuation reused: %v", body)
					}
				}
				if n == 2 {
					writeEvents(w, bad.events...)
					return
				}
				writeEvents(w, `{"type":"response.output_text.delta","delta":"answer"}`, `{"type":"response.completed","response":{"id":"good"}}`)
			}))
			defer srv.Close()
			p := New(Config{Name: "fixture", APIKey: "local", BaseURL: srv.URL, Model: "fixture", Mode: "stateful"})
			req := provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "one"}}}
			collect(t, p, req)
			req.Messages = append(req.Messages, provider.Message{Role: provider.RoleAssistant, Content: "answer"}, provider.Message{Role: provider.RoleUser, Content: "two"})
			assertTerminalFailure(t, collect(t, p, req), bad.cause)
			c := p.(*client)
			c.mu.Lock()
			id, digest := c.lastResponseID, c.expectedPrefixDigest
			c.mu.Unlock()
			if id != "" || digest != "" {
				t.Errorf("refused state retained id=%q digest=%q", id, digest)
			}
			req.Messages = append(req.Messages, provider.Message{Role: provider.RoleUser, Content: "continue"})
			collect(t, p, req)
		})
	}
}
