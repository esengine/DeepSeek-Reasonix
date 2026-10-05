package delegation

import (
	"testing"

	"reasonix/internal/runtime/writeclaim"
)

func TestWhichWriterChildObservesTheWorkspace(t *testing.T) {
	task := &TaskTool{workspaceRoot: "/ws"}
	scoped := writeclaim.NewWriteGrant(writeclaim.WritePathSet{Paths: []string{"/ws/billing"}})
	whole := writeclaim.NewWriteGrant(writeclaim.WritePathSet{WholeWorkspace: true, WorkspaceRoot: "/ws"})
	if got := task.observeRootFor(false, whole); got != "/ws" {
		t.Fatalf("foreground writer observe root = %q, want the workspace", got)
	}
	if got := task.observeRootFor(true, scoped); got != "/ws" {
		t.Fatalf("background writer with declared paths observe root = %q, want the workspace", got)
	}
	for name, g := range map[string]*writeclaim.WriteGrant{"whole workspace": whole, "no grant": nil} {
		if got := task.observeRootFor(true, g); got != "" {
			t.Fatalf("background writer with %s observe root = %q, want none: its walk would claim the parent's writes", name, got)
		}
	}
}
