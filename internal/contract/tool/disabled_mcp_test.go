package tool

import "testing"

func disabledTestBinding() MCPBinding {
	return MCPBinding{
		Package:      "demo",
		Server:       "mock",
		RawName:      "write",
		VisibleName:  "write",
		CallableName: "mcp__mock__write",
		CapabilityID: "mcp-tool:mock/write",
	}
}

func TestReplaceDisabledMCPDropsStaleAliases(t *testing.T) {
	reg := NewRegistry()
	reg.ReplaceDisabledMCP("mock", []MCPBinding{disabledTestBinding()})
	if !reg.DisabledMCP("mcp__mock__write") || !reg.DisabledMCP("mcp-tool:mock/write") {
		t.Fatal("initial disabled policy was not recorded")
	}

	reg.ReplaceDisabledMCP("mock", nil)
	if reg.DisabledMCP("mcp__mock__write") || reg.DisabledMCP("mcp-tool:mock/write") {
		t.Fatal("replacing a server policy with an empty set left stale aliases")
	}
}

func TestDisabledMCPDoesNotClaimBareAliases(t *testing.T) {
	reg := NewRegistry()
	reg.MarkDisabledMCP(disabledTestBinding())
	if reg.DisabledMCP("write") {
		t.Fatal("bare raw name must not claim an ambiguous disabled-tool match")
	}
	if _, ok := reg.DisabledMCPRefusal("write"); ok {
		t.Fatal("bare raw name must not return a disabled-tool refusal")
	}
}

func TestCopyDisabledMCPFromDerivedRegistry(t *testing.T) {
	parent := NewRegistry()
	parent.ReplaceDisabledMCP("mock", []MCPBinding{disabledTestBinding()})
	child := NewRegistry()
	child.CopyDisabledMCPFrom(parent)

	if !child.DisabledMCP("mcp__mock__write") || !child.DisabledMCP("mcp-tool:mock/write") {
		t.Fatal("derived registry did not inherit the disabled-tool policy")
	}
	parent.ClearDisabledMCP("mock")
	if !child.DisabledMCP("mcp__mock__write") {
		t.Fatal("derived registry must remain independent after copying")
	}
}
