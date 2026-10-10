package responses

import (
	"fmt"
	"testing"

	"reasonix/internal/contract/provider"
)

func TestRelayDoneRejectsMalformedArguments(t *testing.T) {
	for _, args := range []string{`null`, `{}`, `[]`, `42`} {
		for _, done := range []string{
			fmt.Sprintf(`{"type":"response.function_call_arguments.done","item_id":"f","arguments":%s}`, args),
			fmt.Sprintf(`{"type":"response.output_item.done","item":{"type":"function_call","id":"f","call_id":"c","name":"ready","arguments":%s}}`, args),
		} {
			t.Run(done, func(t *testing.T) {
				assertTerminalFailure(t, chunksOf(t, `{"type":"response.output_item.added","item":{"type":"function_call","id":"f","call_id":"c","name":"ready"}}`, done, "[DONE]"), provider.ErrInvalidFunctionCall)
			})
		}
	}
}

func TestRelayTerminalRelaxationRequiresMatchingClosure(t *testing.T) {
	const start = `{"type":"response.output_item.added","item":{"type":"function_call","id":"f","call_id":"c","name":"ready"}}`
	const done = `{"type":"response.function_call_arguments.done","item_id":"f","arguments":""}`
	for _, item := range []string{
		`{"type":"function_call","call_id":"other","name":"ready"}`,
		`{"type":"function_call","call_id":"c","name":"other"}`,
		`{"type":"function_call","call_id":"c","name":"ready","arguments":null}`,
		`{"type":"function_call","call_id":"c","name":"ready","arguments":{}}`,
		`{"type":"function_call","call_id":"c","name":"ready","arguments":"","status":"incomplete"}`,
	} {
		t.Run(item, func(t *testing.T) {
			assertTerminalFailure(t, chunksOf(t, start, done, `{"type":"response.completed","response":{"output":[`+item+`]}}`), provider.ErrInvalidFunctionCall)
		})
	}
	for _, args := range []string{"", `,"arguments":""`} {
		assertTerminalFailure(t, chunksOf(t, `{"type":"response.completed","response":{"output":[{"type":"function_call","call_id":"c","name":"ready"`+args+`}]}}`), provider.ErrInvalidFunctionCall)
	}
}
