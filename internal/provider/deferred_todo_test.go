package provider

import (
	"encoding/json"
	"testing"
)

func TestDeferredTodoCompletionMetadataPersistsInProjectionButNotModel(t *testing.T) {
	stored := []Message{{
		Role:                    RoleTool,
		Content:                 "Todos updated",
		ToolCallID:              "todo-1",
		Name:                    "todo_write",
		DeferredTodoCompletions: &DeferredTodoCompletionState{Items: []DeferredTodoCompletion{{ID: "b", Level: 0}}},
		HostTodoState: &HostTodoState{
			Todos:    []HostTodoItem{{Content: "A", Status: "in_progress", StepID: "a"}},
			Deferred: []DeferredTodoCompletion{{ID: "b", Level: 0}},
		},
	}}
	encoded, err := json.Marshal(stored)
	if err != nil {
		t.Fatalf("marshal stored transcript: %v", err)
	}
	var reloaded []Message
	if err := json.Unmarshal(encoded, &reloaded); err != nil {
		t.Fatalf("unmarshal stored transcript: %v", err)
	}
	projection := ProjectionMessages(reloaded)
	if projection[0].DeferredTodoCompletions == nil || len(projection[0].DeferredTodoCompletions.Items) != 1 || projection[0].DeferredTodoCompletions.Items[0].ID != "b" {
		t.Fatalf("projection lost deferred completion: %+v", projection[0].DeferredTodoCompletions)
	}
	if projection[0].HostTodoState == nil || len(projection[0].HostTodoState.Todos) != 1 || projection[0].HostTodoState.Todos[0].Status != "in_progress" || len(projection[0].HostTodoState.Deferred) != 1 {
		t.Fatalf("projection lost host todo state: %+v", projection[0].HostTodoState)
	}
	model := ModelMessages(projection)
	if model[0].DeferredTodoCompletions != nil {
		t.Fatalf("provider model messages leaked deferred metadata: %+v", model[0].DeferredTodoCompletions)
	}
	if model[0].HostTodoState != nil {
		t.Fatalf("provider model messages leaked host todo state: %+v", model[0].HostTodoState)
	}
	if reloaded[0].DeferredTodoCompletions == nil || reloaded[0].DeferredTodoCompletions.Items[0].ID != "b" {
		t.Fatal("ModelMessages mutated the stored metadata")
	}
	if reloaded[0].HostTodoState == nil || reloaded[0].HostTodoState.Todos[0].Status != "in_progress" {
		t.Fatal("ModelMessages mutated the stored host todo state")
	}
}
