package delegation

import (
	"reasonix/internal/runtime/agent"
	"testing"

	"reasonix/internal/contract/tool"
)

// The protocol tool is not a capability. Attaching it before the "can this
// child investigate anything?" check would let a child with no research tools
// pass as having one, and it would then report on work it could not do.
func TestReportingToolDoesNotCountAsAResearchCapability(t *testing.T) {
	parent := tool.NewRegistry()
	parent.Add(subagentRegistryTool{name: "write_file"}) // not read-only

	sub := agent.ReadOnlySubagentToolRegistry(parent, nil)
	if sub.Len() != 0 {
		t.Fatalf("a parent with no read-only tools produced %v", sub.Names())
	}
	// And once it is attached, the registry is no longer empty — which is
	// exactly why the emptiness check has to come first.
	agent.AttachCompleteSubtaskTool(sub)
	if sub.Len() == 0 {
		t.Fatal("complete_subtask was not attached at all")
	}
}
