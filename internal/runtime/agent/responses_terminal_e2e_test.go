package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/model/responses"
	"reasonix/internal/state/sessionstore"
)

func TestResponsesTerminalRejectsLostCalls(t *testing.T) {
	for _, tc := range []struct {
		name   string
		events []string
	}{
		{"unclosed_DONE", []string{
			`{"type":"response.output_text.delta","delta":"I will run the tool."}`,
			`{"type":"response.output_item.added","item":{"id":"fc_1","type":"function_call","call_id":"call_1","name":"echo"}}`,
			`{"type":"response.function_call_arguments.delta","item_id":"fc_1","delta":"{\"text\":"}`,
			`[DONE]`,
		}},
		{"malformed_completed_neighbor", []string{
			`{"type":"response.output_text.delta","delta":"I will run the tool."}`,
			`{"type":"response.completed","response":{"id":"r","output":[{"id":"good","type":"function_call","call_id":"good","name":"echo","arguments":"{\"text\":\"good\"}"},{"id":"bad","type":"function_call","call_id":"bad","name":"echo","arguments":{}}]}}`,
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requests, executions atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				if requests.Add(1) > 1 {
					fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"done\"}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"final\"}}\n")
					return
				}
				for _, e := range tc.events {
					fmt.Fprintf(w, "data: %s\n\n", e)
				}
			}))
			defer srv.Close()
			p := responses.New(responses.Config{Name: "fixture", APIKey: "local-fixture", BaseURL: srv.URL, Model: "fixture", Mode: "stateless"})
			reg := tool.NewRegistry()
			reg.Add(terminalCountingEcho{calls: &executions})
			a := New(p, reg, sessionstore.NewSession(""), Options{}, &recordSink{})
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			err := a.Run(ctx, "Run the required tool and report the result")
			if err == nil {
				t.Error("Run succeeded; want terminal call-integrity failure")
			}
			if requests.Load() != 1 {
				t.Errorf("requests=%d, want one refused attempt without retry", requests.Load())
			}
			if executions.Load() != 0 {
				t.Errorf("executions=%d, want zero", executions.Load())
			}
			for _, m := range a.Session().Snapshot() {
				if !m.LocalOnly && (m.Role == provider.RoleAssistant || m.Role == provider.RoleTool) {
					t.Errorf("committed refused output: %+v", m)
				}
			}
		})
	}
}

type terminalCountingEcho struct {
	echoTool
	calls *atomic.Int32
}

func (t terminalCountingEcho) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	t.calls.Add(1)
	return t.echoTool.Execute(ctx, args)
}

func TestResponsesTerminalToolIntegrity(t *testing.T) {
	const text = `{"type":"response.output_text.delta","item_id":"msg_1","content_index":0,"delta":"I will run the tool: partial-text-marker."}`
	const reasoning = `{"type":"response.reasoning_summary_text.delta","item_id":"rs_1","content_index":0,"delta":"private-reasoning-marker"}`
	const start = `{"type":"response.output_item.added","output_index":1,"item":{"id":"fc_1","type":"function_call","call_id":"call_1","name":"echo","status":"in_progress","arguments":""}}`
	const args = `{"type":"response.function_call_arguments.delta","item_id":"fc_1","delta":"{\"text\":\"hi\"}"}`
	const partial = `{"type":"response.function_call_arguments.delta","item_id":"fc_1","delta":"{\"text\":\"partial-arguments-marker"}`
	const done = `{"type":"response.function_call_arguments.done","item_id":"fc_1","name":"echo","arguments":"{\"text\":\"hi\"}"}`
	const itemDone = `{"type":"response.output_item.done","output_index":1,"item":{"id":"fc_1","type":"function_call","call_id":"call_1","name":"echo","status":"completed","arguments":"{\"text\":\"hi\"}"}}`
	const complete = `{"type":"response.completed","response":{"id":"resp_1","status":"completed","output":[{"id":"msg_1","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"I will run the tool: partial-text-marker.","annotations":[]}]},{"id":"fc_1","type":"function_call","call_id":"call_1","name":"echo","status":"completed","arguments":"{\"text\":\"hi\"}"}],"usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15}}}`
	const noCall = `{"type":"response.completed","response":{"id":"resp_1","status":"completed","output":[{"id":"msg_1","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"I will run the tool: partial-text-marker.","annotations":[]}]}],"usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15}}}`
	const malformed = `{"type":"response.completed","response":{"id":"resp_1","status":"completed","output":[{"id":"fc_bad","type":"function_call","call_id":"call_bad","name":"echo","status":"completed","arguments":{"text":"partial-arguments-marker"}}]}}`
	const malformedNeighbor = `{"type":"response.completed","response":{"id":"resp_1","status":"completed","output":[{"id":"fc_1","type":"function_call","call_id":"call_1","name":"echo","status":"completed","arguments":"{\"text\":\"hi\"}"},{"id":"fc_bad","type":"function_call","call_id":"call_bad","name":"echo","status":"completed","arguments":{"text":"partial-arguments-marker"}}]}}`
	const incomplete = `{"type":"response.incomplete","response":{"id":"resp_1","status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"output":[{"id":"fc_1","type":"function_call","call_id":"call_1","name":"echo","status":"incomplete","arguments":"{\"text\":\"partial-arguments-marker"}],"usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15}}}`
	const ready = `{"type":"response.completed","response":{"id":"resp_1","status":"completed","output":[{"id":"fc_ready","type":"function_call","call_id":"call_ready","name":"ready","status":"completed","arguments":"{}"}]}}`
	const missingArgs = `{"type":"response.completed","response":{"id":"resp_1","status":"completed","output":[{"id":"fc_ready","type":"function_call","call_id":"call_ready","name":"ready","status":"completed"}]}}`
	const inProgress = `{"type":"response.completed","response":{"id":"resp_1","status":"completed","output":[{"id":"fc_1","type":"function_call","call_id":"call_1","name":"echo","status":"in_progress","arguments":"{\"text\":\"hi\"}"}]}}`
	const secondStart = `{"type":"response.output_item.added","output_index":2,"item":{"id":"fc_2","type":"function_call","call_id":"call_2","name":"echo","status":"in_progress","arguments":""}}`
	const secondArgs = `{"type":"response.function_call_arguments.delta","item_id":"fc_2","delta":"{\"text\":\"second\"}"}`
	const secondDone = `{"type":"response.function_call_arguments.done","item_id":"fc_2","name":"echo","arguments":"{\"text\":\"second\"}"}`
	const secondItemDone = `{"type":"response.output_item.done","output_index":2,"item":{"id":"fc_2","type":"function_call","call_id":"call_2","name":"echo","status":"completed","arguments":"{\"text\":\"second\"}"}}`
	const parallelComplete = `{"type":"response.completed","response":{"id":"resp_1","status":"completed","output":[{"id":"fc_1","type":"function_call","call_id":"call_1","name":"echo","status":"completed","arguments":"{\"text\":\"hi\"}"},{"id":"fc_2","type":"function_call","call_id":"call_2","name":"echo","status":"completed","arguments":"{\"text\":\"second\"}"}]}}`

	success := terminalExpectation{requests: 2, calls: map[string]terminalCallExpectation{"call_1": {name: "echo", arguments: `{"text":"hi"}`, output: "echoed: hi"}}, finals: []string{"I will run the tool: partial-text-marker.", "done"}}
	invalid := terminalExpectation{requests: 1, err: provider.ErrInvalidFunctionCall, cause: provider.StreamFailureInvalidFunctionCall, description: "The Responses stream contained a function call with an invalid wire shape. The host refused the whole response; none of its tool calls ran or entered model history.", explicitContinuation: true}
	unfinished := terminalExpectation{requests: 1, err: provider.ErrUnfinishedFunctionCall, cause: provider.StreamFailureUnfinishedFunctionCall, description: "The Responses stream ended successfully while a function call was still unclosed. The host refused the whole response; none of its tool calls ran or entered model history.", explicitContinuation: true}
	cases := []struct {
		name   string
		events []string
		want   terminalExpectation
	}{
		{
			name: "schema_shaped_tool_lifecycle",
			events: []string{
				`{"type":"response.created","response":{"id":"resp_1","status":"in_progress","output":[]}}`,
				`{"type":"response.in_progress","response":{"id":"resp_1","status":"in_progress","output":[]}}`,
				`{"type":"response.output_item.added","output_index":0,"item":{"id":"msg_1","type":"message","role":"assistant","status":"in_progress","content":[]}}`,
				text,
				`{"type":"response.output_text.done","item_id":"msg_1","content_index":0,"text":"I will run the tool: partial-text-marker."}`,
				`{"type":"response.output_item.done","output_index":0,"item":{"id":"msg_1","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"I will run the tool: partial-text-marker.","annotations":[]}]}}`,
				start, args, done, itemDone, complete,
			},
			want: success,
		},
		{name: "completed_output_recovers_missing_done_events", events: []string{text, start, args, complete}, want: success},
		{name: "completed_output_recovers_missing_start_and_done", events: []string{text, complete}, want: success},
		{name: "duplicate_done_and_completed_output_execute_once", events: []string{text, start, args, done, done, itemDone, itemDone, complete}, want: success},
		{
			name:   "two_interleaved_calls_keep_distinct_ids_and_outputs",
			events: []string{text, start, secondStart, args, secondArgs, secondDone, done, itemDone, secondItemDone, parallelComplete},
			want: terminalExpectation{requests: 2, calls: map[string]terminalCallExpectation{
				"call_1": {name: "echo", arguments: `{"text":"hi"}`, output: "echoed: hi"},
				"call_2": {name: "echo", arguments: `{"text":"second"}`, output: "echoed: second"},
			}, finals: []string{"I will run the tool: partial-text-marker.", "done"}},
		},
		{name: "transition_text_without_calls_is_final", events: []string{text, noCall}, want: terminalExpectation{requests: 1, finals: []string{"I will run the tool: partial-text-marker."}, noNotices: true}},
		{name: "unclosed_call_at_DONE", events: []string{text, reasoning, start, partial, "[DONE]"}, want: unfinished},
		{name: "valid_arguments_do_not_close_call_at_DONE", events: []string{text, start, args, "[DONE]"}, want: unfinished},
		{name: "valid_arguments_do_not_close_call_at_completed", events: []string{text, start, args, noCall}, want: unfinished},
		{name: "unclosed_call_at_completed", events: []string{text, reasoning, start, partial, noCall}, want: unfinished},
		{name: "malformed_completed_neighbor_refuses_whole_attempt", events: []string{text, reasoning, malformedNeighbor}, want: invalid},
		{name: "malformed_terminal_without_any_streamed_payload", events: []string{malformed}, want: invalid},
		{name: "malformed_terminal_without_start", events: []string{text, reasoning, malformed}, want: invalid},
		{name: "completed_call_before_malformed_terminal", events: []string{text, reasoning, start, args, done, itemDone, malformed}, want: invalid},
		{name: "no_argument_tool_accepts_empty_object", events: []string{text, ready}, want: terminalExpectation{requests: 2, calls: map[string]terminalCallExpectation{"call_ready": {name: "ready", arguments: `{}`, output: "ready"}}, finals: []string{"I will run the tool: partial-text-marker.", "done"}}},
		{name: "no_argument_tool_cannot_omit_arguments", events: []string{text, missingArgs}, want: invalid},
		{name: "completed_item_cannot_remain_in_progress", events: []string{text, inProgress}, want: invalid},
		{name: "incomplete_max_output_stays_truncation", events: []string{text, start, partial, incomplete}, want: terminalExpectation{requests: 1, finals: []string{"I will run the tool: partial-text-marker."}, notice: "response truncated: hit max output tokens"}},
		{name: "EOF_retries_identical_request_without_protocol_refusal", events: []string{text, reasoning, start, partial}, want: terminalExpectation{requests: 2, retries: 1, exactRetry: true, finals: []string{"done"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { runTerminalFixture(t, tc.events, tc.want) })
	}
}

func TestResponsesTerminalCancellationDoesNotExecuteOrRetry(t *testing.T) {
	runTerminalCancellationFixture(t)
}
