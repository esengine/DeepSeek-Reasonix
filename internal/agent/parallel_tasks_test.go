package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func TestParallelTasksToolIsReadOnly(t *testing.T) {
	p := &ParallelTasksTool{}
	if !p.ReadOnly() {
		t.Fatal("parallel_tasks must be read-only because spawned sub-agents receive only read-only tools")
	}
	if !p.PlanModeSafe() {
		t.Fatal("parallel_tasks must be plan-mode safe because spawned sub-agents receive only read-only tools")
	}
}

func TestParallelTasksSchemaKeepsDependencyOrderingHidden(t *testing.T) {
	schema := string((&ParallelTasksTool{}).Schema())
	if strings.Contains(schema, "depends_on") {
		t.Fatal("parallel_tasks schema should not expose depends_on by default; changing tool schema hurts prompt-cache stability")
	}
}

func TestParallelTasksValidatesAllTasksBeforeRuntimeLookup(t *testing.T) {
	tool := &ParallelTasksTool{}
	_, err := tool.Execute(context.Background(), json.RawMessage(`{
		"tasks": [
			{"prompt": "inspect the parser"},
			{"prompt": "   "}
		]
	}`))
	if err == nil {
		t.Fatal("Execute returned nil error for an empty later task")
	}
	if !strings.Contains(err.Error(), "task 2: prompt is required") {
		t.Fatalf("Execute error = %v, want task validation before runtime lookup", err)
	}
	if strings.Contains(err.Error(), "background jobs are not available") {
		t.Fatalf("Execute looked up background jobs before validating all tasks: %v", err)
	}
}

func TestParallelTasksRejectsHiddenDependencyFieldBeforeRuntimeLookup(t *testing.T) {
	tool := &ParallelTasksTool{}
	_, err := tool.Execute(context.Background(), json.RawMessage(`{
		"tasks": [
			{"prompt": "first", "depends_on": [1]},
			{"prompt": "second"}
		]
	}`))
	if err == nil {
		t.Fatal("Execute returned nil error for a hidden dependency field")
	}
	if !strings.Contains(err.Error(), "depends_on") {
		t.Fatalf("Execute error = %v, want hidden dependency field rejection", err)
	}
	if strings.Contains(err.Error(), "background jobs are not available") {
		t.Fatalf("Execute looked up background jobs before rejecting hidden dependencies: %v", err)
	}
}

func TestParallelTasksForegroundCompletesAndClosesWorkers(t *testing.T) {
	task := newTestTaskTool(t, parallelStaticProvider{}, tool.NewRegistry(), "sys", "", "", nil)
	parallel := NewParallelTasksTool(task, tool.NewRegistry())
	ctx := withCallContext(context.Background(), "parallel-call", event.Discard, nil, false)

	done := make(chan error, 1)
	go func() {
		out, err := parallel.Execute(ctx, json.RawMessage(`{
			"tasks": [
				{"prompt": "first"},
				{"prompt": "second"}
			]
		}`))
		if err != nil {
			done <- err
			return
		}
		if !strings.Contains(out, "Completed 2 parallel tasks") {
			done <- stringsError("missing aggregate output: " + out)
			return
		}
		done <- nil
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("parallel_tasks foreground execution did not return; workers likely waited on spawnCh forever")
	}
}

func TestParallelTasksInjectsWorkspaceContextIntoChildren(t *testing.T) {
	workspace := t.TempDir()
	task := NewTaskTool(promptRoutingProvider{}, nil, tool.NewRegistry(), 20, 0, 0, 0, 0, 0, 0, 0.0, "", "sys", nil, 0, "", "", nil).
		WithTranscripts(NewSubagentStore(t.TempDir()), workspace, "base-model", "base-effort")
	parallel := NewParallelTasksTool(task, tool.NewRegistry())
	ctx := withCallContext(context.Background(), "parallel-call", event.Discard, nil, false)

	out, err := parallel.Execute(ctx, json.RawMessage(`{"tasks":[{"prompt":"inspect one"},{"prompt":"inspect two"}]}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "Current workspace: "+strconv.Quote(workspace)) ||
		!strings.Contains(out, `prefer "." or relative paths`) ||
		!strings.Contains(out, "inspect one ok") ||
		!strings.Contains(out, "inspect two ok") {
		t.Fatalf("parallel output = %q, want child workspace context and prompt", out)
	}
}

func TestParallelTasksDoesNotExposeWriterToolsToChildren(t *testing.T) {
	var writerCalls int32
	parentReg := tool.NewRegistry()
	parentReg.Add(fakeTool{name: "write_file", readOnly: false, calls: &writerCalls})
	task := newTestTaskTool(t, writerCallingProvider{}, parentReg, "sys", "", "", nil)
	parallel := NewParallelTasksTool(task, parentReg)
	ctx := withCallContext(context.Background(), "parallel-call", event.Discard, nil, false)

	out, err := parallel.Execute(ctx, json.RawMessage(`{
		"tasks": [
			{"prompt": "try writer one"},
			{"prompt": "try writer two"}
		]
	}`))
	if err != nil {
		t.Fatalf("Execute returned error: %v\n%s", err, out)
	}
	if got := atomic.LoadInt32(&writerCalls); got != 0 {
		t.Fatalf("writer tool was exposed to read-only sub-agents and called %d times", got)
	}
	if !strings.Contains(out, "Completed 2 parallel tasks") {
		t.Fatalf("missing aggregate output: %s", out)
	}
}

func TestParallelTasksCancelReturnsPartialAggregate(t *testing.T) {
	task := newTestTaskTool(t, promptRoutingProvider{}, tool.NewRegistry(), "sys", "", "", nil)
	parallel := NewParallelTasksTool(task, tool.NewRegistry())

	ctx, cancel := context.WithCancel(withCallContext(context.Background(), "parallel-call", event.Discard, nil, false))
	defer cancel()
	done := make(chan struct {
		out string
		err error
	}, 1)
	go func() {
		out, err := parallel.Execute(ctx, json.RawMessage(`{
			"tasks": [
				{"prompt": "done child"},
				{"prompt": "stuck child"}
			]
		}`))
		done <- struct {
			out string
			err error
		}{out: out, err: err}
	}()

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case got := <-done:
		if !errors.Is(got.err, context.Canceled) {
			t.Fatalf("Execute error = %v, want context cancellation", got.err)
		}
		if strings.Contains(got.out, "Completed 2 parallel tasks") {
			t.Fatalf("cancelled aggregate reported full completion:\n%s", got.out)
		}
		if !strings.Contains(got.out, "done child ok") {
			t.Fatalf("cancelled aggregate lost completed child output:\n%s", got.out)
		}
		if !strings.Contains(strings.ToLower(got.out), "cancelled") {
			t.Fatalf("cancelled aggregate did not mark unfinished child:\n%s", got.out)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("parallel_tasks did not return promptly after cancellation")
	}
}

type parallelStaticProvider struct{}

func (parallelStaticProvider) Name() string { return "parallel-static" }

func (parallelStaticProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "ok"}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

type promptRoutingProvider struct{}

func (promptRoutingProvider) Name() string { return "prompt-routing" }

func (promptRoutingProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	if strings.Contains(lastUser(req), "stuck") {
		return make(chan provider.Chunk), nil
	}
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: lastUser(req) + " ok"}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

type writerCallingProvider struct{}

func (writerCallingProvider) Name() string { return "writer-calling" }

func (writerCallingProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 2)
	if !hasToolResult(req, "write_file") {
		ch <- toolCallChunk("write-1", "write_file", `{"path":"x","content":"y"}`)
		ch <- provider.Chunk{Type: provider.ChunkDone}
		close(ch)
		return ch, nil
	}
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "writer unavailable"}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func hasToolResult(req provider.Request, name string) bool {
	for _, m := range req.Messages {
		if m.Role == provider.RoleTool && m.Name == name {
			return true
		}
	}
	return false
}

type stringsError string

func (e stringsError) Error() string { return string(e) }
