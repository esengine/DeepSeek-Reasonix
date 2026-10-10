package agent

import (
	"fmt"
	"testing"

	"reasonix/internal/contract/provider"
)

func TestResponsesRelayClosedNoArguments(t *testing.T) {
	const start = `{"type":"response.output_item.added","item":{"id":"fc_ready","type":"function_call","call_id":"call_ready","name":"ready","arguments":"","status":"in_progress"}}`
	for _, done := range []struct{ name, event string }{
		{"arguments_done", `{"type":"response.function_call_arguments.done","item_id":"fc_ready","arguments":""}`},
		{"item_done", `{"type":"response.output_item.done","item":{"id":"fc_ready","type":"function_call","call_id":"call_ready","name":"ready","status":"completed"}}`},
	} {
		t.Run(done.name+"/DONE", func(t *testing.T) {
			runTerminalFixture(t, []string{start, done.event, "[DONE]"}, terminalExpectation{
				requests: 2, calls: map[string]terminalCallExpectation{"call_ready": {name: "ready", arguments: `{}`, output: "ready"}}, finals: []string{"", "done"},
			})
		})
		for _, args := range []struct{ name, field string }{{"omitted", ""}, {"empty", `,"arguments":""`}} {
			for _, status := range []string{"completed", "in_progress"} {
				t.Run(done.name+"/"+args.name+"/"+status, func(t *testing.T) {
					terminal := fmt.Sprintf(`{"type":"response.completed","response":{"id":"r","output":[{"id":"fc_ready","type":"function_call","call_id":"call_ready","name":"ready","status":%q%s}]}}`, status, args.field)
					runTerminalFixture(t, []string{start, done.event, terminal}, terminalExpectation{
						requests: 2, calls: map[string]terminalCallExpectation{"call_ready": {name: "ready", arguments: `{}`, output: "ready"}}, finals: []string{"", "done"},
					})
				})
			}
		}
	}
}

func TestResponsesRelayEmptyArgumentsDoNotCloseCall(t *testing.T) {
	const start = `{"type":"response.output_item.added","item":{"id":"fc_ready","type":"function_call","call_id":"call_ready","name":"ready","arguments":""}}`
	for _, terminal := range []string{"[DONE]", `{"type":"response.completed","response":{"id":"r","output":[]}}`} {
		t.Run(terminal, func(t *testing.T) {
			runTerminalFixture(t, []string{start, terminal}, terminalExpectation{requests: 1, err: provider.ErrUnfinishedFunctionCall, cause: provider.StreamFailureUnfinishedFunctionCall})
		})
	}
}

func TestResponsesRelayMalformedDoneNeverExecutes(t *testing.T) {
	const start = `{"type":"response.output_item.added","item":{"id":"fc_ready","type":"function_call","call_id":"call_ready","name":"ready"}}`
	const done = `{"type":"response.function_call_arguments.done","item_id":"fc_ready","arguments":""}`
	const malformed = `{"type":"response.output_item.done","item":{"id":"bad","type":"function_call","call_id":"bad","name":"ready","arguments":null}}`
	runTerminalFixture(t, []string{start, done, malformed, "[DONE]"}, terminalExpectation{
		requests: 1, err: provider.ErrInvalidFunctionCall, cause: provider.StreamFailureInvalidFunctionCall,
		description: "The Responses stream contained a function call with an invalid wire shape. The host refused the whole response; none of its tool calls ran or entered model history.", explicitContinuation: true,
	})
}
