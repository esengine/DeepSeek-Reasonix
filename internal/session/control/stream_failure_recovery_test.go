package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/model/responses"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/state/sessionstore"
)

func TestControllerPreservesStreamFailureRecovery(t *testing.T) {
	const invalid = `{"type":"response.completed","response":{"output":[{"type":"function_call","call_id":"failed_call","name":"probe","arguments":{"value":"failed-arguments"}}]}}`
	const unfinished = `{"type":"response.output_item.added","item":{"type":"function_call","id":"failed_item","call_id":"failed_call","name":"probe"}}`
	for _, tc := range []struct {
		name     string
		terminal []string
		err      error
		cause    provider.StreamFailureCause
	}{
		{"invalid", []string{invalid}, provider.ErrInvalidFunctionCall, provider.StreamFailureInvalidFunctionCall},
		{"unfinished", []string{unfinished, "[DONE]"}, provider.ErrUnfinishedFunctionCall, provider.StreamFailureUnfinishedFunctionCall},
		{"unknown", []string{`{"type":"response.failed","response":{"error":{"code":"unknown_failure","message":"failed-error-prose"}}}`}, nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bodies := make(chan []byte, 8)
			var requests atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
				}
				bodies <- body
				w.Header().Set("Content-Type", "text/event-stream")
				events := []string{`{"type":"response.output_text.delta","delta":"done"}`, `{"type":"response.completed","response":{"output":[]}}`}
				switch requests.Add(1) {
				case 1:
					events = []string{invalid}
				case 2:
					events = []string{`{"type":"response.completed","response":{"output":[{"type":"function_call","id":"successful_item","call_id":"successful_call","name":"probe","arguments":"{}"}]}}`}
				case 3:
					events = append([]string{
						`{"type":"response.output_text.delta","delta":"failed-text"}`,
						`{"type":"response.reasoning_summary_text.delta","delta":"failed-reasoning"}`,
					}, tc.terminal...)
				}
				for _, e := range events {
					_, _ = fmt.Fprintf(w, "data: %s\n\n", e)
				}
			}))
			defer srv.Close()
			probe := &recoveryWriteTool{name: "probe", readOnly: true}
			reg := tool.NewRegistry()
			reg.Add(probe)
			p := responses.New(responses.Config{Name: "local-controller-fixture", APIKey: "fake-local-only", BaseURL: srv.URL, Model: "fixture", Mode: "stateless"})
			a := agent.New(p, reg, sessionstore.NewSession("stable system instructions"), agent.Options{}, event.Discard)
			c := New(Options{Runner: a, Executor: a, SessionPath: sessionstore.NewSessionPath(testenv.TempDir(t), "fixture")})
			defer c.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := c.RunTurn(ctx, "Earlier failed task"); !errors.Is(err, provider.ErrInvalidFunctionCall) {
				t.Fatalf("earlier turn: %v", err)
			}
			previous := a.Session().Snapshot()
			if old := previous[len(previous)-1].InterruptedTurn; old == nil || old.StreamFailure != provider.StreamFailureInvalidFunctionCall {
				t.Errorf("empty-output failure lost cause: %+v", old)
			}
			if err := c.RunTurn(ctx, "Run probe and report the result"); err == nil || tc.err != nil && !errors.Is(err, tc.err) {
				t.Fatalf("current turn: %v, want %v", err, tc.err)
			}
			if requests.Load() != 3 {
				t.Fatalf("requests=%d, want earlier failure + completed tool + refused attempt", requests.Load())
			}
			msgs := a.Session().Snapshot()
			last := msgs[len(msgs)-1]
			if !last.LocalOnly || last.InterruptedTurn == nil {
				t.Fatalf("missing local recovery: %+v", last)
			}
			recovery := last.InterruptedTurn
			if !recovery.Pending || recovery.StreamFailure != tc.cause {
				t.Errorf("current recovery=%+v, want pending cause %q", recovery, tc.cause)
			}
			loaded, err := sessionstore.LoadSession(c.SessionPath())
			if err != nil {
				t.Fatal(err)
			}
			durable := loaded.Messages[len(loaded.Messages)-1]
			if !durable.LocalOnly || durable.InterruptedTurn == nil || durable.InterruptedTurn.StreamFailure != tc.cause {
				t.Errorf("persisted recovery lost cause: %+v", durable)
			}
			if len(recovery.CompletedTools) != 1 || recovery.CompletedTools[0].Name != "probe" {
				t.Errorf("completed tool pair lost: %+v", recovery)
			}
			if last.Content != "failed-text" || last.ReasoningContent != "failed-reasoning" {
				t.Errorf("local display lost: %+v", last)
			}
			for range 3 {
				<-bodies
			}
			for i, input := range []string{"Continue if still needed", "A different task"} {
				if err := c.RunTurn(ctx, input); err != nil {
					t.Fatal(err)
				}
				if requests.Load() != int32(4+i) {
					t.Fatalf("unexpected retry: requests=%d", requests.Load())
				}
				body := <-bodies
				var req struct {
					Instructions string `json:"instructions"`
					Input        []struct {
						Role    string `json:"role"`
						Content string `json:"content"`
					} `json:"input"`
				}
				if err := json.Unmarshal(body, &req); err != nil || len(req.Input) == 0 {
					t.Fatalf("decode request: %v, %s", err, body)
				}
				tail := req.Input[len(req.Input)-1]
				if tail.Role != "user" || sessionstore.StripTransientUserBlocks(tail.Content) != input || req.Instructions != "stable system instructions" {
					t.Errorf("recovery changed user input or prefix: %s", body)
				}
				wantBlock, wantCause := 0, 0
				if i == 0 {
					wantBlock = 1
					if tc.cause != "" {
						wantCause = 1
						if !strings.Contains(tail.Content, "stream_failure: "+string(tc.cause)+"\n"+tc.cause.Description()) {
							t.Errorf("next real request lost typed cause: %s", tail.Content)
						}
					}
				}
				if strings.Count(tail.Content, "<interrupted-turn-recovery>") != wantBlock || strings.Count(tail.Content, "stream_failure:") != wantCause {
					t.Errorf("recovery replayed or inherited an older cause: %s", tail.Content)
				}
				for _, unsafe := range []string{"failed_call", "failed-text", "failed-reasoning", "failed-arguments", "failed-error-prose"} {
					if strings.Contains(string(body), unsafe) {
						t.Errorf("refused payload %q entered provider request", unsafe)
					}
				}
				if !strings.Contains(string(body), "successful_call") {
					t.Error("completed tool pair missing from provider request")
				}
			}
			if probe.runs != 1 {
				t.Errorf("tool executions=%d, want only the completed tool", probe.runs)
			}
		})
	}
}
