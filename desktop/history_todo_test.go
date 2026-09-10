package main

import (
	"encoding/json"
	"testing"

	"reasonix/internal/provider"
)

func TestHistoryMessagesPreserveOriginalTodoWriteArgumentsAfterCompleteStep(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID: "todo-1", Name: "todo_write",
			Arguments: `{"todos":[{"content":"Create the file","status":"in_progress"},{"content":"Update the file","status":"pending"}]}`,
		}}},
		{Role: provider.RoleTool, ToolCallID: "todo-1", Name: "todo_write", Content: "Todos updated"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID: "step-1", Name: "complete_step",
			Arguments: `{"step":"Create the file"}`,
		}}},
		{Role: provider.RoleTool, ToolCallID: "step-1", Name: "complete_step", Content: "signed off"},
	}

	payload := restoredTodoPayload(t, msgs, "todo-1")
	if got := payload.Todos[0].Status; got != "in_progress" {
		t.Fatalf("first historical todo status = %q, want original in_progress", got)
	}
	if got := payload.Todos[1].Status; got != "pending" {
		t.Fatalf("second historical todo status = %q, want original pending", got)
	}
}

func TestHistoryMessagesRequireSuccessfulCompleteStepResult(t *testing.T) {
	tests := []struct {
		name       string
		toolResult *provider.Message
	}{
		{
			name: "failed complete_step",
			toolResult: &provider.Message{
				Role: provider.RoleTool, ToolCallID: "step-1", Name: "complete_step", Content: "error: no evidence",
			},
		},
		{name: "missing complete_step result"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msgs := []provider.Message{
				{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
					ID: "todo-1", Name: "todo_write",
					Arguments: `{"todos":[{"content":"Create the file","status":"in_progress"},{"content":"Update the file","status":"pending"}]}`,
				}}},
				{Role: provider.RoleTool, ToolCallID: "todo-1", Name: "todo_write", Content: "Todos updated"},
				{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
					ID: "step-1", Name: "complete_step",
					Arguments: `{"step":"Create the file"}`,
				}}},
			}
			if tc.toolResult != nil {
				msgs = append(msgs, *tc.toolResult)
			}

			payload := restoredTodoPayload(t, msgs, "todo-1")
			if got := payload.Todos[0].Status; got != "in_progress" {
				t.Fatalf("complete_step without success changed first todo to %q", got)
			}
			if got := payload.Todos[1].Status; got != "pending" {
				t.Fatalf("complete_step without success changed second todo to %q", got)
			}
		})
	}
}

func TestHistoryMessagesIgnoreHistoricalPendingSignoff(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID: "todo-1", Name: "todo_write",
			Arguments: `{"todos":[{"content":"Create the file","status":"in_progress"},{"content":"Update the file","status":"pending"}]}`,
		}}},
		{Role: provider.RoleTool, ToolCallID: "todo-1", Name: "todo_write", Content: "Todos updated"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID: "step-2", Name: "complete_step", Arguments: `{"step":"Update the file"}`,
		}}},
		{Role: provider.RoleTool, ToolCallID: "step-2", Name: "complete_step", Content: "signed off"},
	}

	payload := restoredTodoPayload(t, msgs, "todo-1")
	if got := payload.Todos[0].Status; got != "in_progress" {
		t.Fatalf("current todo status = %q, want in_progress", got)
	}
	if got := payload.Todos[1].Status; got != "pending" {
		t.Fatalf("historical pending signoff changed status to %q", got)
	}
}

func TestHistoryMessagesPreserveLegacyTodoWriteState(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID: "todo-1", Name: "todo_write",
			Arguments: `{"todos":[{"content":"Create the file","status":"in_progress"},{"content":"Update the file","status":"completed"}]}`,
		}}},
		{Role: provider.RoleTool, ToolCallID: "todo-1", Name: "todo_write", Content: "Todos updated"},
	}

	payload := restoredTodoPayload(t, msgs, "todo-1")
	if got := payload.Todos[0].Status; got != "in_progress" {
		t.Fatalf("legacy current todo status = %q, want in_progress", got)
	}
	if got := payload.Todos[1].Status; got != "completed" {
		t.Fatalf("legacy historical todo status changed to %q, want original completed", got)
	}
}

func TestHistoryMessagesIgnoreFailedTodoWriteAsReplayBase(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID: "todo-1", Name: "todo_write",
			Arguments: `{"todos":[{"content":"Create the file","status":"in_progress"}]}`,
		}}},
		{Role: provider.RoleTool, ToolCallID: "todo-1", Name: "todo_write", Content: "Todos updated"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID: "todo-2", Name: "todo_write",
			Arguments: `{"todos":[{"content":"Bad replacement","status":"in_progress"}]}`,
		}}},
		{Role: provider.RoleTool, ToolCallID: "todo-2", Name: "todo_write", Content: "error: rejected todo transition"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID: "step-1", Name: "complete_step", Arguments: `{"step":"Create the file"}`,
		}}},
		{Role: provider.RoleTool, ToolCallID: "step-1", Name: "complete_step", Content: "signed off"},
	}

	good := restoredTodoPayload(t, msgs, "todo-1")
	if got := good.Todos[0].Status; got != "in_progress" {
		t.Fatalf("successful base history was changed: %q", got)
	}

	bad := restoredTodoPayload(t, msgs, "todo-2")
	if got := bad.Todos[0].Content; got != "Bad replacement" {
		t.Fatalf("failed todo_write arguments should stay original, got %q", got)
	}
	if got := bad.Todos[0].Status; got != "in_progress" {
		t.Fatalf("failed todo_write should not be replayed, status = %q", got)
	}
}

func restoredTodoPayload(t *testing.T, msgs []provider.Message, todoID string) struct {
	Todos []struct {
		Content string `json:"content"`
		Status  string `json:"status"`
	} `json:"todos"`
} {
	t.Helper()
	history := historyMessages(msgs, func(s string) string { return s })
	var todoArgs string
	for _, m := range history {
		for _, tc := range m.ToolCalls {
			if tc.ID == todoID {
				todoArgs = tc.Arguments
			}
		}
	}
	if todoArgs == "" {
		t.Fatalf("todo_write %q arguments missing from history", todoID)
	}
	var payload struct {
		Todos []struct {
			Content string `json:"content"`
			Status  string `json:"status"`
		} `json:"todos"`
	}
	if err := json.Unmarshal([]byte(todoArgs), &payload); err != nil {
		t.Fatalf("todo args are not JSON: %v", err)
	}
	return payload
}

func TestHistoryMessagesGatePhaseSignoffOnSubSteps(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID: "todo-1", Name: "todo_write",
			Arguments: `{"todos":[{"content":"Port the parser","status":"pending"},{"content":"move files","status":"in_progress","level":1},{"content":"fix imports","status":"pending","level":1}]}`,
		}}},
		{Role: provider.RoleTool, ToolCallID: "todo-1", Name: "todo_write", Content: "Todos updated"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID: "step-1", Name: "complete_step", Arguments: `{"step":"Port the parser"}`,
		}}},
		{Role: provider.RoleTool, ToolCallID: "step-1", Name: "complete_step", Content: "signed off"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID: "step-2", Name: "complete_step", Arguments: `{"step":"move files"}`,
		}}},
		{Role: provider.RoleTool, ToolCallID: "step-2", Name: "complete_step", Content: "signed off"},
	}

	payload := restoredTodoPayload(t, msgs, "todo-1")
	if got := payload.Todos[0].Status; got != "pending" {
		t.Fatalf("phase sign-off with unfinished sub-steps replayed to %q, want pending", got)
	}
	if got := payload.Todos[1].Status; got != "in_progress" {
		t.Fatalf("historical sub-step status changed to %q, want original in_progress", got)
	}
	if got := payload.Todos[2].Status; got != "pending" {
		t.Fatalf("historical next sub-step status changed to %q, want original pending", got)
	}

	msgs = append(msgs,
		provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID: "step-3", Name: "complete_step", Arguments: `{"step":"fix imports"}`,
		}}},
		provider.Message{Role: provider.RoleTool, ToolCallID: "step-3", Name: "complete_step", Content: "signed off"},
	)
	payload = restoredTodoPayload(t, msgs, "todo-1")
	if got := payload.Todos[0].Status; got != "pending" {
		t.Fatalf("historical phase status changed after later complete_step to %q, want original pending", got)
	}
}
