package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func TestParallelTasksToolIsWriteCapable(t *testing.T) {
	if (&ParallelTasksTool{}).ReadOnly() {
		t.Fatal("parallel_tasks must be write-capable because spawned sub-agents can invoke writer tools")
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

func TestParallelTasksRejectsDependencyCyclesBeforeRuntimeLookup(t *testing.T) {
	tool := &ParallelTasksTool{}
	_, err := tool.Execute(context.Background(), json.RawMessage(`{
		"tasks": [
			{"prompt": "first", "depends_on": [1]},
			{"prompt": "second", "depends_on": [0]}
		]
	}`))
	if err == nil {
		t.Fatal("Execute returned nil error for cyclic dependencies")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("Execute error = %v, want dependency cycle validation", err)
	}
	if strings.Contains(err.Error(), "background jobs are not available") {
		t.Fatalf("Execute looked up background jobs before validating dependencies: %v", err)
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

type parallelStaticProvider struct{}

func (parallelStaticProvider) Name() string { return "parallel-static" }

func (parallelStaticProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "ok"}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

type stringsError string

func (e stringsError) Error() string { return string(e) }
