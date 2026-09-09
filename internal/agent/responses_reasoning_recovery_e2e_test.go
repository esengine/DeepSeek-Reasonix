package agent

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/provider/responses"
)

const responsesToolWithoutReasoningSSE = `data: {"type":"response.output_item.added","item":{"id":"fc_1","type":"function_call","call_id":"call_1","name":"echo"}}

data: {"type":"response.output_item.done","item":{"id":"fc_1","type":"function_call","call_id":"call_1","name":"echo","arguments":"{\"text\":\"hi\"}"}}

data: {"type":"response.completed","response":{"id":"resp_tool","usage":{"input_tokens":10,"output_tokens":2,"total_tokens":12}}}

`

const responsesFinalAnswerSSE = `data: {"type":"response.output_text.delta","item_id":"msg_1","content_index":0,"delta":"done"}

data: {"type":"response.completed","response":{"id":"resp_final","usage":{"input_tokens":12,"output_tokens":1,"total_tokens":13}}}

`

func TestResponsesToolTurnWithoutReasoningContinuesSafely(t *testing.T) {
	tests := []struct {
		name, baseURL, model, effort string
		wantRequests                 int
		wantReasoningRetries         int
	}{
		{name: "deepseek flash", baseURL: "https://api.deepseek.com", model: "deepseek-v4-flash", effort: "high", wantRequests: 2},
		{name: "deepseek pro", baseURL: "https://api.deepseek.com", model: "deepseek-v4-pro", effort: "high", wantRequests: 2},
		{name: "deepseek pro reasoning disabled", baseURL: "https://api.deepseek.com", model: "deepseek-v4-pro", effort: "none", wantRequests: 2},
		{name: "mimo", baseURL: "https://api.xiaomimimo.com/v1", model: "mimo-v2.5-pro", effort: "high", wantRequests: 2},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			var bodies [][]byte
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Errorf("read request: %v", err)
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				mu.Lock()
				bodies = append(bodies, append([]byte(nil), body...))
				mu.Unlock()

				w.Header().Set("Content-Type", "text/event-stream")
				if bytes.Contains(body, []byte(`"type":"function_call_output"`)) {
					_, _ = io.WriteString(w, responsesFinalAnswerSSE)
					return
				}
				_, _ = io.WriteString(w, responsesToolWithoutReasoningSSE)
			}))
			defer srv.Close()

			prov := responses.New(responses.Config{
				Name: "responses-replay", APIKey: "test-key", BaseURL: tc.baseURL,
				RequestURL: srv.URL, Model: tc.model, Effort: tc.effort,
			})
			sink := &recordSink{}
			a := New(prov, echoRegistry(), NewSession(""), Options{MissingReasoningWarnStateDir: t.TempDir()}, sink)
			if err := a.Run(withNoClosedLoop(context.Background()), "go"); err != nil {
				t.Fatalf("Run: %v", err)
			}

			mu.Lock()
			gotBodies := append([][]byte(nil), bodies...)
			mu.Unlock()
			if len(gotBodies) != tc.wantRequests {
				t.Fatalf("HTTP requests = %d, want %d", len(gotBodies), tc.wantRequests)
			}
			if tc.wantReasoningRetries == 1 && !bytes.Equal(gotBodies[0], gotBodies[1]) {
				t.Fatal("missing-reasoning probe did not replay the exact frozen request")
			}
			if got := sink.recoveryCount(event.ProtocolRecoveryMissingReasoningRetryAttempted); got != tc.wantReasoningRetries {
				t.Fatalf("reasoning retries = %d, want %d", got, tc.wantReasoningRetries)
			}
			if got := len(sink.kinds(event.ToolResult)); got != 1 {
				t.Fatalf("tool results = %d, want exactly one execution", got)
			}

			var toolTurns int
			for _, message := range a.Session().Snapshot() {
				if message.Role == provider.RoleAssistant && len(message.ToolCalls) > 0 {
					toolTurns++
					if message.ReasoningContent != "" {
						t.Fatalf("missing provider reasoning was fabricated as %q", message.ReasoningContent)
					}
				}
			}
			if toolTurns != 1 {
				t.Fatalf("saved tool turns = %d, want 1", toolTurns)
			}
		})
	}
}
