package main

import (
	"encoding/json"
	"testing"

	"reasonix/internal/provider"
)

func TestHistoryMessagesReplayCompleteStepsIntoTodoWrite(t *testing.T) {
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

	history := historyMessages(msgs, func(s string) string { return s })
	var todoArgs string
	for _, m := range history {
		for _, tc := range m.ToolCalls {
			if tc.ID == "todo-1" {
				todoArgs = tc.Arguments
			}
		}
	}
	if todoArgs == "" {
		t.Fatal("todo_write arguments missing from history")
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
	if got := payload.Todos[0].Status; got != "completed" {
		t.Fatalf("first todo status = %q, want completed", got)
	}
	if got := payload.Todos[1].Status; got != "in_progress" {
		t.Fatalf("second todo status = %q, want in_progress", got)
	}
}

func TestHistoryMessagesDoNotReplayFailedCompleteStepIntoTodoWrite(t *testing.T) {
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
		{Role: provider.RoleTool, ToolCallID: "step-1", Name: "complete_step", Content: "error: no evidence"},
	}

	history := historyMessages(msgs, func(s string) string { return s })
	var todoArgs string
	for _, m := range history {
		for _, tc := range m.ToolCalls {
			if tc.ID == "todo-1" {
				todoArgs = tc.Arguments
			}
		}
	}
	if todoArgs == "" {
		t.Fatal("todo_write arguments missing from history")
	}

	var payload struct {
		Todos []struct {
			Status string `json:"status"`
		} `json:"todos"`
	}
	if err := json.Unmarshal([]byte(todoArgs), &payload); err != nil {
		t.Fatalf("todo args are not JSON: %v", err)
	}
	if got := payload.Todos[0].Status; got != "in_progress" {
		t.Fatalf("failed complete_step changed first todo to %q", got)
	}
	if got := payload.Todos[1].Status; got != "pending" {
		t.Fatalf("failed complete_step changed second todo to %q", got)
	}
}
