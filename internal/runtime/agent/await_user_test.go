package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/safety/evidence"
)

func awaitCtx(todos []evidence.TodoItem, interactive bool) context.Context {
	ctx := evidence.WithTodoState(context.Background(), todos)
	ctx = evidence.WithLedger(ctx, evidence.NewLedger())
	var asker Asker
	if interactive {
		asker = &recordingAsker{}
	}
	return WithCallContext(ctx, "", nil, asker, false)
}

func newsList() []evidence.TodoItem {
	return []evidence.TodoItem{
		{StepID: "n1", Content: "discuss the first item", Status: "in_progress"},
		{StepID: "n2", Content: "discuss the second item", Status: "pending"},
	}
}

func TestAwaitUserNamesTheWaitingItem(t *testing.T) {
	out, err := NewAwaitUserTool().Execute(awaitCtx(newsList(), true),
		json.RawMessage(`{"need":"your view on this one"}`))
	if err != nil {
		t.Fatalf("the current item should be the default: %v", err)
	}
	if !strings.Contains(out, "n1") {
		t.Fatalf("output %q should cite the item it parked", out)
	}

	if _, err := NewAwaitUserTool().Execute(awaitCtx(newsList(), true),
		json.RawMessage(`{"step_id":"n2","need":"your view"}`)); err != nil {
		t.Fatalf("a pending item may be named explicitly: %v", err)
	}
}

func TestAwaitUserRejectsWhatItCannotPark(t *testing.T) {
	for name, tc := range map[string]struct {
		todos       []evidence.TodoItem
		interactive bool
		args        string
		want        string
	}{
		"no need": {
			todos: newsList(), interactive: true,
			args: `{"need":"   "}`, want: "need",
		},
		"no list at all": {
			todos: nil, interactive: true,
			args: `{"need":"your view"}`, want: "no list open",
		},
		"list already finished": {
			todos:       []evidence.TodoItem{{StepID: "n1", Content: "done", Status: "completed"}},
			interactive: true,
			args:        `{"need":"your view"}`, want: "completed",
		},
		// A need carrying the options belongs on an ask card.
		"need too long for a status line": {
			todos: newsList(), interactive: true,
			args: `{"need":"` + strings.Repeat("选", awaitUserNeedLimit+1) + `"}`, want: "call ask",
		},
		"unknown step": {
			todos: newsList(), interactive: true,
			args: `{"step_id":"n9","need":"your view"}`, want: "n1, n2",
		},
		"completed step": {
			todos: []evidence.TodoItem{
				{StepID: "n1", Content: "first", Status: "completed"},
				{StepID: "n2", Content: "second", Status: "in_progress"},
			},
			interactive: true,
			args:        `{"step_id":"n1","need":"your view"}`, want: "already completed",
		},
		// No asker, so no gate is recorded and readiness is unchanged.
		"headless run": {
			todos: newsList(), interactive: false,
			args: `{"need":"your view"}`, want: "conclude_blocked",
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := NewAwaitUserTool().Execute(awaitCtx(tc.todos, tc.interactive), json.RawMessage(tc.args))
			if err == nil {
				t.Fatal("expected the call to be refused")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

// A list written but not started has no in_progress item. The host names the
// ids it accepts rather than choosing one.
func TestAwaitUserAsksForAnIDWhenNoItemIsCurrent(t *testing.T) {
	todos := []evidence.TodoItem{
		{StepID: "n1", Content: "first", Status: "pending"},
		{StepID: "n2", Content: "second", Status: "pending"},
	}
	_, err := NewAwaitUserTool().Execute(awaitCtx(todos, true), json.RawMessage(`{"need":"your view"}`))
	if err == nil {
		t.Fatal("expected a refusal naming the citable ids")
	}
	if !strings.Contains(err.Error(), "n1, n2") {
		t.Fatalf("error = %v, want the available ids", err)
	}
}

// The gate is the only thing separating a deliberate hand-back from a turn that
// stopped early. Without one the open list is debt the host continues.
func TestReadinessParksAnOpenListOnlyForAGate(t *testing.T) {
	newAgent := func(gated bool) *Agent {
		ledger := evidence.NewLedger()
		ledger.Record(evidence.Receipt{ToolName: "todo_write", Success: true, Todos: newsList()})
		ledger.Record(evidence.Receipt{ToolName: "complete_step", Success: true, Step: "n1"})
		if gated {
			ledger.Record(evidence.Receipt{
				ToolName: evidence.UserGateTool, Success: true, Read: true,
				Args: json.RawMessage(`{"step_id":"n1","need":"your view on this one"}`),
			})
		}
		return &Agent{task: taskRuntime{ledger: ledger}}
	}

	if reason := newAgent(false).finalReadinessCheckFor().reason; reason == "" {
		t.Fatal("an open list with no gate is still outstanding work")
	}
	gated := newAgent(true)
	if reason := gated.finalReadinessCheckFor().reason; reason != "" {
		t.Fatalf("a parked list must not be owed: %q", reason)
	}
	gate, ok := gated.UserGate()
	if !ok || gate.StepID != "n1" || gate.Need != "your view on this one" {
		t.Fatalf("UserGate() = %+v, %v", gate, ok)
	}
}

// A refused call records no gate, so readiness is unchanged.
func TestRefusedGateLeavesTheListOutstanding(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.Receipt{ToolName: "todo_write", Success: true, Todos: newsList()})
	ledger.Record(evidence.Receipt{ToolName: "complete_step", Success: true, Step: "n1"})
	ledger.Record(evidence.Receipt{ToolName: evidence.UserGateTool, Success: false})
	a := &Agent{task: taskRuntime{ledger: ledger}}
	if reason := a.finalReadinessCheckFor().reason; reason == "" {
		t.Fatal("a refused gate must not park the list")
	}
	if _, ok := a.UserGate(); ok {
		t.Fatal("a refused gate is not a gate")
	}
}
