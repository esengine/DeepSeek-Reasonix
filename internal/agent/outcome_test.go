package agent

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

type mcpAliasTool struct {
	fakeTool
	server  string
	raw     string
	visible string
	pkg     string
}

func (t mcpAliasTool) MCPServerName() string      { return t.server }
func (t mcpAliasTool) MCPRawToolName() string     { return t.raw }
func (t mcpAliasTool) MCPVisibleToolName() string { return t.visible }
func (t mcpAliasTool) MCPPackageName() string     { return t.pkg }
func (t mcpAliasTool) MCPServerAuthorized() bool  { return true }

// TestFailedCallsSurfaceError guards the bug where a failed tool call (an unknown
// tool, e.g. a hallucinated "find", or a permission-denied writer) was reported
// with an empty Err and so rendered with a success check. A failed call must set
// errMsg; a successful one must not.
func TestFailedCallsSurfaceError(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "ok_tool", readOnly: true})
	reg.Add(fakeTool{name: "writer", readOnly: false})
	gate := &recordingPermissionGate{allow: true}
	a := New(nil, reg, NewSession(""), Options{Gate: gate}, event.Discard)

	if o := a.executeOne(context.Background(), provider.ToolCall{Name: "ok_tool"}); o.errMsg != "" {
		t.Errorf("successful call should have empty errMsg, got %q", o.errMsg)
	}
	if o := a.executeOne(context.Background(), provider.ToolCall{Name: "find"}); o.errMsg == "" {
		t.Errorf("unknown tool should surface an errMsg (renders as failed), got %+v", o)
	}

	a.SetPlanMode(true)
	gate.allow = false
	gate.reason = "denied by permission policy"
	if o := a.executeOne(context.Background(), provider.ToolCall{Name: "writer"}); o.errMsg == "" {
		t.Errorf("permission-denied writer should surface an errMsg, got %+v", o)
	}
}

func TestCompletedMCPConnectIsNotReportedAsUnknown(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(mcpAliasTool{
		fakeTool: fakeTool{name: "mcp__mock__echo", readOnly: true},
		server:   "mock",
		raw:      "echo",
		visible:  "echo",
	})
	a := New(nil, reg, NewSession(""), Options{}, event.Discard)

	out := a.executeOne(context.Background(), provider.ToolCall{Name: "mcp__mock__connect", Arguments: `{}`})
	if out.errMsg != "" || !strings.Contains(out.output, `MCP server "mock" is connected`) {
		t.Fatalf("completed connect was not recovered: %+v", out)
	}
	for _, name := range []string{"mcp__missing__connect", "mcp__mock__not_connect"} {
		if out := a.executeOne(context.Background(), provider.ToolCall{Name: name, Arguments: `{}`}); out.errMsg == "" {
			t.Fatalf("unadvertised call %q should remain unknown: %+v", name, out)
		}
	}
}

func TestPortableMCPCallUsesCanonicalSecurityIdentity(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(mcpAliasTool{
		fakeTool: fakeTool{name: "mcp__figma__get_design_context", readOnly: true},
		server:   "figma",
		raw:      "figma_get_design_context",
		visible:  "get_design_context",
		pkg:      "figma",
	})
	gate := &recordingPermissionGate{allow: true}
	a := New(nil, reg, NewSession(""), Options{Gate: gate}, event.Discard)

	out := a.executeOne(context.Background(), provider.ToolCall{Name: "get_design_context", Arguments: `{}`})
	if out.errMsg != "" {
		t.Fatalf("portable MCP call failed: %+v", out)
	}
	if len(gate.denyCalls) != 1 || gate.denyCalls[0] != "mcp__figma__get_design_context" {
		t.Fatalf("permission deny checks = %v, want canonical MCP name", gate.denyCalls)
	}
}

func TestAmbiguousPortableMCPCallIsRejected(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(mcpAliasTool{fakeTool: fakeTool{name: "mcp__one__search", readOnly: true}, server: "one", raw: "search", visible: "search"})
	reg.Add(mcpAliasTool{fakeTool: fakeTool{name: "mcp__two__search", readOnly: true}, server: "two", raw: "search", visible: "search"})
	a := New(nil, reg, NewSession(""), Options{}, event.Discard)

	out := a.executeOne(context.Background(), provider.ToolCall{Name: "search", Arguments: `{}`})
	if out.errMsg == "" || !strings.Contains(out.errMsg, "ambiguous MCP tool reference") {
		t.Fatalf("ambiguous MCP alias was not rejected: %+v", out)
	}
}
