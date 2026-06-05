package agent

import (
	"context"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// TestFailedCallsSurfaceError guards the bug where a failed tool call (an unknown
// tool, e.g. a hallucinated "find", or a plan-mode-blocked writer) was reported
// with an empty Err and so rendered with a success check. A failed call must set
// errMsg; a successful one must not.
func TestFailedCallsSurfaceError(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "ok_tool", readOnly: true})
	reg.Add(fakeTool{name: "writer", readOnly: false})
	a := New(nil, reg, NewSession(""), Options{}, event.Discard)

	if o := a.executeOne(context.Background(), provider.ToolCall{Name: "ok_tool"}); o.errMsg != "" {
		t.Errorf("successful call should have empty errMsg, got %q", o.errMsg)
	}
	if o := a.executeOne(context.Background(), provider.ToolCall{Name: "find"}); o.errMsg == "" {
		t.Errorf("unknown tool should surface an errMsg (renders as failed), got %+v", o)
	}

	a.SetPlanMode(true)
	if o := a.executeOne(context.Background(), provider.ToolCall{Name: "writer"}); o.errMsg == "" {
		t.Errorf("plan-mode-blocked writer should surface an errMsg, got %+v", o)
	}
}
