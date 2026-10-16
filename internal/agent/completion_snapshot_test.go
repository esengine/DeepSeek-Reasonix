package agent

import (
	"testing"

	"reasonix/internal/evidence"
)

func TestCompletionSnapshotSeparatesTodoPresenceAndObservedMutation(t *testing.T) {
	a := &Agent{task: taskRuntime{ledger: evidence.NewLedger()}}
	if got := a.CompletionSnapshot(); got.TodosWritten || got.TodosComplete || got.HasObservedMutation {
		t.Fatalf("empty completion snapshot = %+v", got)
	}

	a.setTodoState([]evidence.TodoItem{{Content: "write summary", Status: "completed"}})
	a.task.ledger.Record(evidence.Receipt{ToolName: "write_file", Success: true, Mutation: true})
	got := a.CompletionSnapshot()
	if !got.TodosWritten || !got.TodosComplete || !got.HasObservedMutation || got.HasVerifiedEvidence {
		t.Fatalf("completed mutation snapshot = %+v", got)
	}

	a.setTodoState([]evidence.TodoItem{{Content: "write summary", Status: "in_progress"}})
	got = a.CompletionSnapshot()
	if !got.TodosWritten || got.TodosComplete || !got.HasObservedMutation {
		t.Fatalf("incomplete todo snapshot = %+v", got)
	}
}
