package delegation

import (
	"testing"

	"reasonix/internal/contract/event"
)

func TestSubSinkForwardsWorkspaceMutationToParent(t *testing.T) {
	parent := newWorkspaceSignalSink()
	event.RecordWorkspaceMutation(subSinkFor("task_1", parent), event.WorkspaceMutation{
		ToolID: "write", ToolName: "write_file", Paths: []string{"child.go"}, Content: true,
	})
	select {
	case mutation := <-parent.mutations:
		if mutation.ToolName != "write_file" || len(mutation.Paths) != 1 || mutation.Paths[0] != "child.go" {
			t.Fatalf("forwarded workspace mutation = %+v", mutation)
		}
	default:
		t.Fatal("sub-agent workspace mutation was not forwarded")
	}
}
