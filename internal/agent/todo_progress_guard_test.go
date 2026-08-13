package agent

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"reasonix/internal/agent/testutil"
	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/provider"
	"reasonix/internal/tool"

	_ "reasonix/internal/tool/builtin"
)

// stalledTodoTurns drives a todo that never advances: the first unique read
// renews the lease, exact repeats after it do not.
func stalledTodoTurns(extra int) []testutil.Turn {
	turns := []testutil.Turn{{ToolCalls: []provider.ToolCall{{
		ID: "todo", Name: "todo_write",
		Arguments: `{"todos":[{"content":"finish the task","status":"in_progress"}]}`,
	}}}}
	for i := range todoProgressNudgeRounds*2 + extra {
		turns = append(turns, testutil.Turn{ToolCalls: []provider.ToolCall{{
			ID: fmt.Sprintf("read-%d", i), Name: "inspect", Arguments: `{"path":"same"}`,
		}}})
	}
	return turns
}

func stalledTodoAgent(t *testing.T, turns []testutil.Turn) (*Agent, *testutil.MockProvider) {
	t.Helper()
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "inspect", readOnly: true})
	reg.Add(mustBuiltinTool(t, "todo_write"))
	mp := testutil.NewMock("m", turns...)
	return New(mp, reg, NewSession(""), Options{}, event.Discard), mp
}

// A stalled todo never ends a run, under Goal or ordinary chat. The model is
// asked to reassess once; what it does after that is its own call, and the
// zero-evidence ladder already owns the structural stop on the same receipts.
func TestTodoProgressGuardNeverPausesARun(t *testing.T) {
	for _, tc := range []struct {
		name string
		ctx  func() context.Context
	}{
		{"chat", context.Background},
		{"goal", func() context.Context {
			return WithDeliveryExecutionScope(context.Background(), DeliveryExecutionScope{ID: "goal-1"})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			turns := append(stalledTodoTurns(4), testutil.Turn{Text: "Done."})
			a, mp := stalledTodoAgent(t, turns)

			err := a.Run(tc.ctx(), "work until the todo is complete")
			if err != nil && !isToolLoopPause(err) {
				t.Fatalf("Run error = %v", err)
			}
			if err != nil {
				// Goal's structural guard may stop first on its own terms; what
				// must not exist is a stop keyed to the todo streak.
				if got := PauseClass(err); got == "todo_stall" {
					t.Fatalf("pause class = %q, want the todo stall pause gone", got)
				}
				return
			}
			if got, want := mp.CallCount(), len(turns); got != want {
				t.Fatalf("provider calls = %d, want all %d turns to run past the old threshold", got, want)
			}
			if !sessionContains(a, "Host progress check") {
				t.Fatal("the reassessment nudge went missing; only the pause was meant to go")
			}
		})
	}
}

func TestGoalTodoProgressGuardReplansWithoutPausing(t *testing.T) {
	turns := []testutil.Turn{{ToolCalls: []provider.ToolCall{{
		ID: "todo", Name: "todo_write",
		Arguments: `{"todos":[{"content":"finish the task","status":"in_progress"}]}`,
	}}}}
	// The first unique read renews the lease; maxTodoStallRounds exact repeats
	// after it reach the Goal redirect threshold.
	for i := range maxTodoStallRounds + 1 {
		turns = append(turns, testutil.Turn{ToolCalls: []provider.ToolCall{{
			ID: fmt.Sprintf("read-%d", i), Name: "inspect", Arguments: `{"path":"same"}`,
		}}})
	}
	turns = append(turns, testutil.Turn{Text: "Replanned; a real blocker would be reported through update_goal."})

	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "inspect", readOnly: true})
	reg.Add(mustBuiltinTool(t, "todo_write"))
	mp := testutil.NewMock("m", turns...)
	a := New(mp, reg, NewSession(""), Options{}, event.Discard)
	ctx := WithDeliveryExecutionScope(context.Background(), DeliveryExecutionScope{ID: "goal-1", TaskText: "finish the task"})
	if err := a.Run(ctx, "work until the todo is complete"); err != nil {
		t.Fatalf("Goal todo stall must redirect, not pause: %v", err)
	}
	if !sessionContains(a, "Host progress redirect") {
		t.Fatal("Goal todo stall did not inject a re-plan redirect")
	}
}

func TestTodoProgressGuardRenewsOnUniqueHostWork(t *testing.T) {
	turns := []testutil.Turn{{ToolCalls: []provider.ToolCall{{
		ID: "todo", Name: "todo_write",
		Arguments: `{"todos":[{"content":"finish the task","status":"in_progress"}]}`,
	}}}}
	for i := range todoProgressNudgeRounds - 1 {
		turns = append(turns, testutil.Turn{ToolCalls: []provider.ToolCall{{
			ID: fmt.Sprintf("read-a-%d", i), Name: "inspect", Arguments: `{"path":"same"}`,
		}}})
	}
	turns = append(turns,
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "write", Name: "write_file", Arguments: `{"path":"result.txt","content":"done"}`}}},
	)
	for i := range todoProgressNudgeRounds - 1 {
		turns = append(turns, testutil.Turn{ToolCalls: []provider.ToolCall{{
			ID: fmt.Sprintf("read-b-%d", i), Name: "inspect", Arguments: `{"path":"same"}`,
		}}})
	}
	turns = append(turns,
		testutil.Turn{ToolCalls: []provider.ToolCall{{
			ID: "done", Name: "complete_step",
			Arguments: `{"step":"finish the task","result":"done","evidence":[{"kind":"files","summary":"created result","paths":["result.txt"]}]}`,
		}}},
		testutil.Turn{Text: "done"},
	)

	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "inspect", readOnly: true})
	reg.Add(fakeTool{name: "write_file", readOnly: false})
	reg.Add(mustBuiltinTool(t, "todo_write"))
	reg.Add(mustBuiltinTool(t, "complete_step"))
	a := New(testutil.NewMock("m", turns...), reg, NewSession(""), Options{}, event.Discard)
	if err := a.Run(context.Background(), "finish the todo"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if sessionContains(a, "Host progress check") {
		t.Fatal("unique host work should renew the progress lease before the nudge threshold")
	}
}

func TestCanonicalTodoProgressIgnoresTitleAndPendingListChurn(t *testing.T) {
	a := &Agent{sess: sessionRuntime{todoState: []evidence.TodoItem{
		{Content: "finish the task", Status: "in_progress"},
		{Content: "write tests", Status: "pending"},
	}}}
	before, tracking := a.canonicalTodoProgress()
	if !tracking {
		t.Fatal("incomplete todo list should be tracked")
	}
	a.setTodoState([]evidence.TodoItem{
		{Content: "finish the task carefully", Status: "in_progress"},
		{Content: "write tests", Status: "pending"},
		{Content: "update docs", Status: "pending"},
	})
	after, tracking := a.canonicalTodoProgress()
	if !tracking || after != before {
		t.Fatalf("title/pending churn changed progress from %d to %d", before, after)
	}
}

func TestMaxStepsGraceSummaryBypassesIncompleteTodoReadiness(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(mustBuiltinTool(t, "todo_write"))
	reg.Add(fakeTool{name: "write_file", readOnly: false})
	mp := testutil.NewMock("m",
		testutil.Turn{ToolCalls: []provider.ToolCall{
			{ID: "todo", Name: "todo_write", Arguments: `{"todos":[{"content":"unfinished","status":"in_progress"}]}`},
			{ID: "write", Name: "write_file", Arguments: `{"path":"unfinished.txt"}`},
		}},
		testutil.Turn{Text: "Progress saved; the todo remains unfinished."},
	)
	a := New(mp, reg, NewSession(""), Options{MaxSteps: 1, DeliveryProfile: true}, event.Discard)

	err := a.Run(context.Background(), "start a long task")
	var pause *maxStepsPause
	if !errors.As(err, &pause) {
		t.Fatalf("Run error = %v, want maxStepsPause instead of final-readiness retries", err)
	}
	if mp.CallCount() != 2 {
		t.Fatalf("provider calls = %d, want tool round plus one summary round", mp.CallCount())
	}
}

func mustBuiltinTool(t *testing.T, name string) tool.Tool {
	t.Helper()
	builtin, ok := tool.LookupBuiltin(name)
	if !ok {
		t.Fatalf("builtin %q is not registered", name)
	}
	return builtin
}
