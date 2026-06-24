package codegraph

import "testing"

func TestMCPSpecSetsQiaotongAgentDaemonIdleTimeout(t *testing.T) {
	spec := MCPSpec("/tmp/codegraph", "/tmp/project")
	if spec.Name != "codegraph" {
		t.Fatalf("Name = %q, want codegraph", spec.Name)
	}
	if spec.StripRawPrefix != "codegraph_" {
		t.Fatalf("StripRawPrefix = %q, want codegraph_", spec.StripRawPrefix)
	}
	if got := spec.Env[DaemonIdleTimeoutEnv]; got != QiaotongAgentDaemonIdleTimeoutMS {
		t.Fatalf("%s = %q, want %q", DaemonIdleTimeoutEnv, got, QiaotongAgentDaemonIdleTimeoutMS)
	}
	if !spec.LowPriority {
		t.Fatal("LowPriority = false, want true")
	}
	if !spec.ReadOnlyToolNames["codegraph_search"] {
		t.Fatal("ReadOnlyToolNames missing codegraph_search")
	}
}
