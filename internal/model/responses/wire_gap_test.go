package responses

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"reasonix/internal/contract/provider"
)

func TestCompletedWebSearchCallSurfacesAsAFinishedServerTool(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeEvents(w,
			`{"type":"response.output_item.done","item":{"id":"ws_1","type":"web_search_call","status":"completed","action":{"type":"search","query":"deepseek responses api","sources":[{"url":"https://api-docs.deepseek.com/guides/responses_api"}]}}}`,
			`{"type":"response.output_text.delta","item_id":"msg_1","delta":"answered"}`,
			`{"type":"response.completed","response":{"id":"resp_1","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`,
		)
	}))
	defer server.Close()

	var card *provider.Chunk
	for _, chunk := range collect(t, New(Config{Name: "deepseek", APIKey: "k", BaseURL: server.URL, Model: "m", Mode: "stateless", WebSearch: true}),
		provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "search"}}}) {
		if chunk.Type == provider.ChunkProviderTool {
			card = &chunk
		}
	}
	if card == nil || card.ToolCall == nil {
		t.Fatal("a completed web_search_call produced no provider-run tool chunk")
	}
	if card.ToolCall.Name != "web_search" || card.ToolCall.ID != "ws_1" {
		t.Fatalf("card call = %#v", card.ToolCall)
	}
	if !strings.Contains(card.ToolCall.Arguments, "deepseek responses api") {
		t.Fatalf("card arguments = %q, want the searched query", card.ToolCall.Arguments)
	}
	if !strings.Contains(card.Text, "https://api-docs.deepseek.com/guides/responses_api") {
		t.Fatalf("card text = %q, want the named source", card.Text)
	}
}

func TestTopLevelErrorEventEndsTheStreamWithWhatTheServerSaid(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeEvents(w, `{"type":"error","code":"server_error","message":"upstream model unavailable"}`)
	}))
	defer server.Close()

	var failure error
	for _, chunk := range collect(t, New(Config{Name: "compatible", APIKey: "k", BaseURL: server.URL, Model: "m", Mode: "stateless"}),
		provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}}}) {
		if chunk.Type == provider.ChunkError {
			failure = chunk.Err
		}
	}
	if failure == nil {
		t.Fatal("a protocol error event produced no error chunk")
	}
	if !strings.Contains(failure.Error(), "upstream model unavailable") {
		t.Fatalf("error = %v, want the server's own words rather than a premature EOF", failure)
	}
	if provider.IsStreamInterrupted(failure) {
		t.Fatalf("error = %v, want a reported failure rather than an interrupt worth resuming", failure)
	}
}

func TestRefusalPartsAreTheTurnsVisibleAnswer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeEvents(w,
			`{"type":"response.refusal.delta","item_id":"msg_1","content_index":0,"delta":"I cannot help "}`,
			`{"type":"response.refusal.done","item_id":"msg_1","content_index":0,"refusal":"I cannot help with that."}`,
			`{"type":"response.completed","response":{"id":"resp_1","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`,
		)
	}))
	defer server.Close()

	var text strings.Builder
	for _, chunk := range collect(t, New(Config{Name: "compatible", APIKey: "k", BaseURL: server.URL, Model: "m", Mode: "stateless"}),
		provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}}}) {
		if chunk.Type == provider.ChunkText {
			text.WriteString(chunk.Text)
		}
	}
	if text.String() != "I cannot help " {
		t.Fatalf("streamed text = %q, want the refusal delta once and no duplicate from done", text.String())
	}
}

func TestContinuationBadRequestRecoversWithoutProtocolMetadata(t *testing.T) {
	var attempts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		if attempts == 2 {
			http.Error(w, `{"error":{"message":"previous response id invalid or expired","type":"invalid_request_error"}}`, http.StatusBadRequest)
			return
		}
		writeEvents(w, `{"type":"response.completed","response":{"id":"resp_1","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`)
	}))
	defer server.Close()

	p := New(Config{Name: "stateful", APIKey: "k", BaseURL: server.URL, Model: "m", Mode: "stateful"})
	collect(t, p, provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "one"}}})
	collect(t, p, provider.Request{Messages: []provider.Message{
		{Role: provider.RoleUser, Content: "one"},
		{Role: provider.RoleAssistant, Content: ""},
		{Role: provider.RoleUser, Content: "two"},
	}})
	if attempts != 3 {
		t.Fatalf("attempts = %d, want initial, continuation and full-history recovery", attempts)
	}
}

func TestReasoningInputIdentityFollowsTheVendorTable(t *testing.T) {
	message := provider.Message{
		Role: provider.RoleAssistant, Content: "answer",
		ReasoningContent: "thought", ReasoningID: "rs_1", ReasoningStatus: "completed",
	}
	for _, tc := range []struct {
		name, baseURL string
		wantIdentity  bool
	}{
		{"deepseek folds reasoning into the assistant message", "https://api.deepseek.com", false},
		{"an unknown endpoint keeps OpenAI's required id", "https://gateway.example/v1", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := New(Config{Name: "p", BaseURL: tc.baseURL, Model: "m", Mode: "stateless"}).(*client)
			body, _, _ := client.buildRequestBody(provider.Request{Messages: []provider.Message{
				{Role: provider.RoleUser, Content: "hi"}, message,
			}})
			wire, err := json.Marshal(body["input"])
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Contains(string(wire), `"rs_1"`); got != tc.wantIdentity {
				t.Fatalf("reasoning id on the wire = %v, want %v: %s", got, tc.wantIdentity, wire)
			}
		})
	}
}

func TestUnknownFailureCodeIsNotGuessedIntoAnAuthError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeEvents(w, `{"type":"response.failed","response":{"id":"resp","error":{"code":"rate_limit_exceeded","message":"too many requests for this api key"}}}`)
	}))
	defer server.Close()

	for _, chunk := range collect(t, New(Config{Name: "compatible", APIKey: "k", BaseURL: server.URL, Model: "m", Mode: "stateless"}),
		provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}}}) {
		if chunk.Type != provider.ChunkError {
			continue
		}
		var authErr *provider.AuthError
		if errors.As(chunk.Err, &authErr) {
			t.Fatalf("a rate-limit code became an auth error because its message named a key: %v", chunk.Err)
		}
	}
}

func chunksOf(t *testing.T, events ...string) []provider.Chunk {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeEvents(w, events...)
	}))
	defer server.Close()
	return collect(t, New(Config{Name: "compatible", APIKey: "k", BaseURL: server.URL, Model: "m", Mode: "stateless"}),
		provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "list files"}}})
}

func toolCallsOf(t *testing.T, events ...string) []provider.ToolCall {
	t.Helper()
	var calls []provider.ToolCall
	for _, chunk := range chunksOf(t, events...) {
		if chunk.Type == provider.ChunkToolCall && chunk.ToolCall != nil {
			calls = append(calls, *chunk.ToolCall)
		}
	}
	return calls
}

func TestCompletedOutputClosesACallWhoseDoneEventsNeverCame(t *testing.T) {
	calls := toolCallsOf(t,
		`{"type":"response.output_item.added","item":{"id":"fc_1","type":"function_call","call_id":"call_1","name":"ls","arguments":""}}`,
		`{"type":"response.function_call_arguments.delta","item_id":"fc_1","delta":"{\"path\":"}`,
		`{"type":"response.function_call_arguments.delta","item_id":"fc_1","delta":"\".\"}"}`,
		`{"type":"response.completed","response":{"id":"resp_1","output":[{"id":"fc_1","type":"function_call","status":"completed","call_id":"call_1","name":"ls","arguments":"{\"path\":\".\"}"}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`,
	)
	if len(calls) != 1 || calls[0].ID != "call_1" || calls[0].Name != "ls" || calls[0].Arguments != `{"path":"."}` {
		t.Fatalf("calls = %#v, want the one call the completed response lists", calls)
	}
}

func TestCompletedOutputClosesACallWhoseDeltasCarriedNoItemID(t *testing.T) {
	calls := toolCallsOf(t,
		`{"type":"response.output_item.added","item":{"id":"fc_1","type":"function_call","call_id":"call_1","name":"ls","arguments":""}}`,
		`{"type":"response.function_call_arguments.delta","delta":"{}"}`,
		`{"type":"response.completed","response":{"id":"resp_1","output":[{"id":"fc_1","type":"function_call","call_id":"call_1","name":"ls","arguments":"{}"}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`,
	)
	if len(calls) != 1 || calls[0].ID != "call_1" || calls[0].Name != "ls" {
		t.Fatalf("calls = %#v, want exactly the listed call", calls)
	}
}

func TestCompletedOutputDoesNotRepeatACallTheStreamClosed(t *testing.T) {
	calls := toolCallsOf(t,
		`{"type":"response.output_item.added","item":{"id":"fc_1","type":"function_call","call_id":"call_1","name":"ls","arguments":""}}`,
		`{"type":"response.function_call_arguments.done","item_id":"fc_other","arguments":"{}"}`,
		`{"type":"response.output_item.done","item":{"id":"fc_2","type":"function_call","status":"completed","call_id":"call_1","name":"ls","arguments":"{}"}}`,
		`{"type":"response.completed","response":{"id":"resp_1","output":[{"id":"fc_1","type":"function_call","status":"completed","call_id":"call_1","name":"ls","arguments":"{}"}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`,
	)
	named := 0
	for _, call := range calls {
		if call.ID == "call_1" {
			named++
		}
	}
	if named != 1 {
		t.Fatalf("calls = %#v, want call_1 dispatched once", calls)
	}
}

func TestIncompleteOutputCallIsNotDispatched(t *testing.T) {
	calls := toolCallsOf(t,
		`{"type":"response.output_item.added","item":{"id":"fc_1","type":"function_call","call_id":"call_1","name":"write_file","arguments":""}}`,
		`{"type":"response.function_call_arguments.delta","item_id":"fc_1","delta":"{\"path\":\"a"}`,
		`{"type":"response.incomplete","response":{"id":"resp_1","incomplete_details":{"reason":"max_output_tokens"},"output":[{"id":"fc_1","type":"function_call","status":"incomplete","call_id":"call_1","name":"write_file","arguments":"{\"path\":\"a"}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`,
	)
	if len(calls) != 0 {
		t.Fatalf("calls = %#v, want a truncated call left undispatched", calls)
	}
}

func TestCompletedOutputSurvivesAnUnrelatedItemItCannotRead(t *testing.T) {
	chunks := chunksOf(t,
		`{"type":"response.output_item.added","item":{"id":"fc_1","type":"function_call","call_id":"call_1","name":"ls","arguments":""}}`,
		`{"type":"response.completed","response":{"id":"resp_1","output":[{"id":"rs_1","type":"reasoning","status":{"phase":"done"}},{"id":"fc_1","type":"function_call","call_id":"call_1","name":"ls","arguments":"{}"}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`,
	)
	var calls []provider.ToolCall
	done := false
	for _, chunk := range chunks {
		switch chunk.Type {
		case provider.ChunkError:
			t.Fatalf("error chunk: %v", chunk.Err)
		case provider.ChunkDone:
			done = true
		case provider.ChunkToolCall:
			calls = append(calls, *chunk.ToolCall)
		}
	}
	if !done {
		t.Fatalf("chunks = %#v, want the turn to finish", chunks)
	}
	if len(calls) != 1 || calls[0].ID != "call_1" {
		t.Fatalf("calls = %#v, want only the readable call_1", calls)
	}
}

func TestOutputCallWithoutCallIDIsNotDispatched(t *testing.T) {
	calls := toolCallsOf(t,
		`{"type":"response.completed","response":{"id":"resp_1","output":[{"id":"fc_1","type":"function_call","status":"completed","name":"ls","arguments":"{}"}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`,
	)
	if len(calls) != 0 {
		t.Fatalf("calls = %#v, want a call with no call id left undispatched", calls)
	}
}

func TestArgumentsDoneWithoutItemIDIsNotDispatched(t *testing.T) {
	calls := toolCallsOf(t,
		`{"type":"response.function_call_arguments.done","arguments":"{}"}`,
		`{"type":"response.completed","response":{"id":"resp_1","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`,
	)
	if len(calls) != 0 {
		t.Fatalf("calls = %#v, want no call without a name", calls)
	}
}
