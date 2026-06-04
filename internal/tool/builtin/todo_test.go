package builtin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestTodoWriteAcceptsLevels(t *testing.T) {
	args := json.RawMessage(`{"todos":[` +
		`{"content":"Phase","status":"in_progress","level":0},` +
		`{"content":"sub","status":"pending","level":1}]}`)
	if _, err := (todoWrite{}).Execute(context.Background(), args); err != nil {
		t.Fatalf("levels 0/1 should be accepted: %v", err)
	}
}

func TestTodoWriteRejectsBadLevel(t *testing.T) {
	args := json.RawMessage(`{"todos":[{"content":"x","status":"pending","level":2}]}`)
	_, err := (todoWrite{}).Execute(context.Background(), args)
	if err == nil || !strings.Contains(err.Error(), "level") {
		t.Fatalf("level 2 should be rejected with a level error, got %v", err)
	}
}

func TestTodoWriteAcceptsCompletedDirectly(t *testing.T) {
	args := json.RawMessage(`{"todos":[` +
		`{"content":"Step 1","status":"completed"},` +
		`{"content":"Step 2","status":"in_progress"},` +
		`{"content":"Step 3","status":"pending"}]}`)
	result, err := (todoWrite{}).Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("direct completion should be accepted: %v", err)
	}
	if !strings.Contains(result, "1 completed") {
		t.Errorf("expected 1 completed in result, got: %s", result)
	}
}
