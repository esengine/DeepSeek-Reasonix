package agent

import (
	"context"
	"errors"
	"slices"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func standardTaskProgressContext(mutationExpected bool) context.Context {
	ctx := WithAutomaticReadinessContinuation(context.Background())
	return WithMutationExpected(ctx, mutationExpected)
}

func standardTaskTextTurn(text string) []provider.Chunk {
	return []provider.Chunk{{Type: provider.ChunkText, Text: text}, {Type: provider.ChunkDone}}
}

func TestStandardMutationExpectationRequiresSuccessfulMutation(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "write_file"})
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{standardTaskTextTurn("I am ready to implement it.")}}
	a := New(prov, reg, NewSession(""), Options{}, event.Discard)

	err := a.Run(standardTaskProgressContext(true), "update main.go")
	var readinessErr *FinalReadinessError
	if !errors.As(err, &readinessErr) {
		t.Fatalf("Run error = %v, want FinalReadinessError", err)
	}
	if readinessErr.ContinuationClass != ReadinessContinuationTaskProgress {
		t.Fatalf("ContinuationClass = %q, want task progress", readinessErr.ContinuationClass)
	}
	if !slices.Equal(readinessErr.Missing, []string{"mutation"}) {
		t.Fatalf("Missing = %v, want mutation only", readinessErr.Missing)
	}
}

func TestStandardMutationExpectationRequiresWritableExecutor(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts Options
		reg  *tool.Registry
	}{
		{name: "no writer", reg: tool.NewRegistry()},
		{name: "read only executor", opts: Options{ReadOnlyExecution: true}, reg: func() *tool.Registry {
			reg := tool.NewRegistry()
			reg.Add(fakeTool{name: "write_file"})
			return reg
		}()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{standardTaskTextTurn("analysis complete")}}
			a := New(prov, tc.reg, NewSession(""), tc.opts, event.Discard)
			if err := a.Run(standardTaskProgressContext(true), "update main.go"); err != nil {
				t.Fatalf("Run returned %v for executor that cannot mutate", err)
			}
		})
	}
}

func TestStandardCurrentTaskTodoRequiresCompletionAfterMutation(t *testing.T) {
	todoWrite, ok := tool.LookupBuiltin("todo_write")
	if !ok {
		t.Fatal("todo_write builtin not registered")
	}
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "write_file"})
	reg.Add(todoWrite)
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{
			toolCallChunk("write", "write_file", `{"path":"main.go","content":"package main"}`),
			toolCallChunk("todo", "todo_write", `{"todos":[{"content":"Finish implementation","status":"in_progress"}]}`),
			{Type: provider.ChunkDone},
		},
		standardTaskTextTurn("partially implemented"),
	}}
	a := New(prov, reg, NewSession(""), Options{}, event.Discard)

	err := a.Run(standardTaskProgressContext(true), "update main.go")
	var readinessErr *FinalReadinessError
	if !errors.As(err, &readinessErr) {
		t.Fatalf("Run error = %v, want FinalReadinessError", err)
	}
	if !slices.Equal(readinessErr.Missing, []string{"todo"}) {
		t.Fatalf("Missing = %v, want current todo only", readinessErr.Missing)
	}
	if got := a.CurrentTaskTodoState(); len(got) != 1 || got[0].Content != "Finish implementation" {
		t.Fatalf("CurrentTaskTodoState = %+v", got)
	}
}

func TestStandardExplicitlyDeferredActionRequiresContinuationAfterMutation(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "write_file"})
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{toolCallChunk("write", "write_file", `{"path":"main.go","content":"updated"}`), {Type: provider.ChunkDone}},
		standardTaskTextTurn("main.go is updated. Next I will update functions.php."),
	}}
	a := New(prov, reg, NewSession(""), Options{}, event.Discard)

	err := a.Run(standardTaskProgressContext(true), "update main.go")
	var readinessErr *FinalReadinessError
	if !errors.As(err, &readinessErr) {
		t.Fatalf("Run error = %v, want FinalReadinessError", err)
	}
	if readinessErr.ContinuationClass != ReadinessContinuationTaskProgress {
		t.Fatalf("ContinuationClass = %q, want task progress", readinessErr.ContinuationClass)
	}
	if !slices.Equal(readinessErr.Missing, []string{"task"}) {
		t.Fatalf("Missing = %v, want deferred task only", readinessErr.Missing)
	}
}

func TestStandardCompletedMutationMayEndWithoutTodo(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "write_file"})
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{toolCallChunk("write", "write_file", `{"path":"main.go","content":"package main"}`), {Type: provider.ChunkDone}},
		standardTaskTextTurn("Implemented the requested change."),
	}}
	a := New(prov, reg, NewSession(""), Options{}, event.Discard)

	if err := a.Run(standardTaskProgressContext(true), "update main.go"); err != nil {
		t.Fatalf("completed mutation returned %v", err)
	}
}

func TestDirectAgentCallDoesNotInferDeferredTaskControl(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "write_file"})
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{toolCallChunk("write", "write_file", `{"path":"login.php","content":"updated"}`), {Type: provider.ChunkDone}},
		standardTaskTextTurn("Next I will update functions.php."),
	}}
	a := New(prov, reg, NewSession(""), Options{}, event.Discard)

	if err := a.Run(context.Background(), "update login.php and functions.php"); err != nil {
		t.Fatalf("direct Agent call opted into task continuation: %v", err)
	}
}

func TestStandardCompletedOrClearedCurrentTodoMayEnd(t *testing.T) {
	todoWrite, ok := tool.LookupBuiltin("todo_write")
	if !ok {
		t.Fatal("todo_write builtin not registered")
	}
	for _, args := range []string{
		`{"todos":[{"content":"Finish implementation","status":"completed"}]}`,
		`{"todos":[]}`,
	} {
		reg := tool.NewRegistry()
		reg.Add(fakeTool{name: "write_file"})
		reg.Add(todoWrite)
		prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
			{
				toolCallChunk("write", "write_file", `{"path":"main.go","content":"package main"}`),
				toolCallChunk("todo", "todo_write", args),
				{Type: provider.ChunkDone},
			},
			standardTaskTextTurn("implemented"),
		}}
		a := New(prov, reg, NewSession(""), Options{}, event.Discard)
		if err := a.Run(standardTaskProgressContext(true), "update main.go"); err != nil {
			t.Fatalf("Run with todo args %s returned %v", args, err)
		}
	}
}

func TestStandardIgnoresCanonicalTodoFromPriorTask(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "write_file"})
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{standardTaskTextTurn("answered the new request")}}
	a := New(prov, reg, NewSession(""), Options{}, event.Discard)
	a.SeedTodoState([]evidence.TodoItem{{Content: "Old task", Status: "in_progress"}})

	if err := a.Run(standardTaskProgressContext(false), "explain the current code"); err != nil {
		t.Fatalf("prior canonical todo blocked a new Standard task: %v", err)
	}
	if got := a.CurrentTaskTodoState(); len(got) != 0 {
		t.Fatalf("current task todos = %+v, want none", got)
	}
	if got := a.CanonicalTodoState(); len(got) != 1 || got[0].Content != "Old task" {
		t.Fatalf("canonical todo was not preserved: %+v", got)
	}
}
