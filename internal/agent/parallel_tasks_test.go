package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestParallelTasksToolIsWriteCapable(t *testing.T) {
	if (&ParallelTasksTool{}).ReadOnly() {
		t.Fatal("parallel_tasks must be write-capable because spawned sub-agents can invoke writer tools")
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
