package tool

import (
	"strings"
	"testing"
)

func heldFixture() HeldMCP {
	return HeldMCP{Class: HeldMCPChanged, Binding: MCPBinding{Server: "srv", RawName: "write", VisibleName: "write", CallableName: "mcp__srv__write"}}
}

func TestHeldMCPRefusalIsTypedAndServerQualified(t *testing.T) {
	reg := NewRegistry()
	reg.ReplaceHeldMCP("srv", []HeldMCP{heldFixture()})
	refusal, ok := reg.MCPRefusal("mcp__srv__write")
	if !ok || refusal.Code != CodeMCPToolHeld || !strings.Contains(refusal.Message, `"srv"`) {
		t.Fatalf("refusal = %+v, %v", refusal, ok)
	}
	if _, ok := reg.MCPRefusal("write"); ok {
		t.Fatal("a bare name is ambiguous across servers and must not be claimed")
	}
	reg.ReplaceHeldMCP("srv", nil)
	if _, ok := reg.MCPRefusal("mcp__srv__write"); ok {
		t.Fatal("a released tool kept refusing")
	}
}

func TestDisabledPolicyWinsOverHeldAndChildrenInheritBoth(t *testing.T) {
	parent := NewRegistry()
	b := heldFixture().Binding
	parent.ReplaceDisabledMCP("srv", []MCPBinding{b})
	parent.ReplaceHeldMCP("srv", []HeldMCP{heldFixture()})
	child := NewRegistry()
	child.CopyDisabledMCPFrom(parent)
	if r, _ := child.MCPRefusal("mcp__srv__write"); r.Code != CodeMCPToolDisabled {
		t.Fatalf("code = %q, want the configuration refusal first", r.Code)
	}
	parent.ReplaceDisabledMCP("srv", nil)
	child.CopyDisabledMCPFrom(parent)
	if r, ok := child.MCPRefusal("mcp__srv__write"); !ok || r.Code != CodeMCPToolHeld {
		t.Fatalf("child lost the held set: %+v", r)
	}
}
