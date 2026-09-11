package cli

import (
	"testing"

	"reasonix/internal/capability"
)

func TestMergeCapabilityAuditCopiesNestedCounters(t *testing.T) {
	var m RunMetrics
	audit := capability.Audit{
		Routes:    2,
		Discovery: capability.DiscoveryAudit{Lists: 1, Searches: 3},
		MCPLists:  capability.MCPListAudit{SharedHost: 4, DiskCache: 1, Remote: 0, DurationMs: 9, ToolCount: 6, SchemaBytes: 128, Triggers: map[string]int{"inspect": 5}},
		ToolExec:  capability.ToolExecAudit{Calls: 5, ReadOnly: 4, Parallel: 2},
		Phases:    capability.PhaseAudit{ReviewMs: 40},
	}
	m.MergeCapabilityAudit(&audit)
	if m.CapabilityRoutes != 2 || m.CapabilityDiscovery.Searches != 3 {
		t.Fatalf("scalar/discovery merge = %+v %+v", m, m.CapabilityDiscovery)
	}
	if m.CapabilityMCPLists.SharedHost != 4 || m.CapabilityMCPLists.ToolCount != 6 || m.CapabilityMCPLists.Triggers["inspect"] != 5 {
		t.Fatalf("mcp list merge = %+v", m.CapabilityMCPLists)
	}
	if m.CapabilityToolExec.Parallel != 2 || m.CapabilityPhases.ReviewMs != 40 {
		t.Fatalf("exec/phase merge = %+v %+v", m.CapabilityToolExec, m.CapabilityPhases)
	}
}
