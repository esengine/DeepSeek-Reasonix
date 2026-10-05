package agent

import (
	"context"
	"reasonix/internal/state/sessionstore"
	"strings"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/runtime/agent/testutil"

	_ "reasonix/internal/tools/builtin"
)

// planCards counts the todo_write cards a frontend would draw for this run,
// host-advanced ones included: one ToolResult per card.
func planCards(sink *recordSink) []event.Event {
	var out []event.Event
	for _, e := range sink.kinds(event.ToolResult) {
		if e.Tool.Name == "todo_write" {
			out = append(out, e)
		}
	}
	return out
}

// #3990: the plan appeared twice. complete_step advances the list and emits the
// card for that advance; the model, told to "re-send this todo_write", sent the
// same list again and a second identical card landed under it.
func TestEchoedTodoWriteDrawsNoSecondPlanCard(t *testing.T) {
	list := `{"todos":[{"content":"test","status":"completed"},{"content":"vet","status":"in_progress"}]}`
	mp := testutil.NewMock("m",
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "t0", Name: "todo_write",
			Arguments: `{"todos":[{"content":"test","status":"in_progress"},{"content":"vet","status":"pending"}]}`}}},
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "b1", Name: "bash",
			Arguments: `{"command":"go test ./..."}`}}},
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "c1", Name: "complete_step",
			Arguments: `{"step":"test","result":"tests pass","evidence":[{"kind":"verification","summary":"tests pass","command":"go test ./..."}]}`}}},
		// The re-send: the same list the sign-off just produced.
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "t1", Name: "todo_write", Arguments: list}}},
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "b2", Name: "bash",
			Arguments: `{"command":"go vet ./..."}`}}},
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "c2", Name: "complete_step",
			Arguments: `{"step":"vet","result":"vet passes","evidence":[{"kind":"verification","summary":"vet passes","command":"go vet ./..."}]}`}}},
		testutil.Turn{Text: "all done"},
	)
	sink := &recordSink{}
	a := New(mp, evidenceRegistry(), sessionstore.NewSession("sys"), Options{}, sink)
	if err := a.Run(context.Background(), "implement the plan"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	for _, card := range planCards(sink) {
		if card.Tool.ID == "t1" {
			t.Fatal("the re-sent list drew its own plan card; the host had already drawn that state")
		}
	}
	// The call still happened and the model still hears about it — only the
	// second drawing of one plan is dropped.
	if !sessionContains(a, "Task list unchanged") {
		t.Fatal("the re-send was not reported to the model as changing nothing")
	}
	// Establishing the list and each host advance are real cards.
	if n := len(planCards(sink)); n != 3 {
		t.Fatalf("plan cards = %d, want 3 (one initial, one per sign-off)", n)
	}
}

// A todo_write that moves the list is a card like any other, dispatch included,
// so holding the card back until the outcome is known cannot lose one.
func TestChangedTodoWriteStillDrawsItsCard(t *testing.T) {
	mp := testutil.NewMock("m",
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "t0", Name: "todo_write",
			Arguments: `{"todos":[{"content":"test","status":"in_progress"}]}`}}},
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "t1", Name: "todo_write",
			Arguments: `{"todos":[{"content":"test","status":"in_progress"},{"content":"vet","status":"pending"}]}`}}},
		testutil.Turn{Text: "listed"},
	)
	sink := &recordSink{}
	a := New(mp, evidenceRegistry(), sessionstore.NewSession("sys"), Options{}, sink)
	_ = a.Run(context.Background(), "plan the work")

	var dispatched, resulted int
	for _, e := range sink.kinds(event.ToolDispatch) {
		if e.Tool.Name == "todo_write" {
			dispatched++
		}
	}
	resulted = len(planCards(sink))
	if dispatched != 2 || resulted != 2 {
		t.Fatalf("todo_write dispatch/result = %d/%d, want 2/2", dispatched, resulted)
	}
}

// The hint a blocked completion gives must not send the model back to re-send
// the list: doing that is what drew the plan twice.
func TestCompletionGateHintDoesNotAskForAReSend(t *testing.T) {
	mp := testutil.NewMock("m",
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "t0", Name: "todo_write",
			Arguments: `{"todos":[{"content":"test","status":"in_progress"}]}`}}},
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "t1", Name: "todo_write",
			Arguments: `{"todos":[{"content":"test","status":"completed"}]}`}}},
		testutil.Turn{Text: "done"},
	)
	a := New(mp, evidenceRegistry(), sessionstore.NewSession("sys"), Options{}, event.Discard)
	_ = a.Run(context.Background(), "finish the step")

	if !sessionContains(a, "complete_step receipt") {
		t.Fatal("completing an item without a sign-off was not refused")
	}
	for _, m := range a.Session().Messages {
		if strings.Contains(m.Content, "re-send this todo_write") {
			t.Fatal("the gate still tells the model to re-send the list the host will advance itself")
		}
	}
}
