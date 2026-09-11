package agent

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/agent/testutil"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// A read-only subagent refusing its own bash call must reach the frontend as a
// blocked result, not as a completed one: the command never ran.
func TestReadOnlySubagentBashRefusalReachesSinkAsBlocked(t *testing.T) {
	parent := tool.NewRegistry()
	parent.Add(subagentRegistryTool{
		name:   "bash",
		schema: `{"type":"object","properties":{"command":{"type":"string"}},"required":["command"]}`,
		result: "ran",
	})
	sub := ReadOnlySubagentToolRegistry(parent, nil)

	mp := testutil.NewMock("m",
		testutil.Turn{ToolCalls: []provider.ToolCall{
			{ID: "call-1", Name: "bash", Arguments: `{"command":"node --version 2>&1; bash --version"}`},
		}},
		testutil.Turn{Text: "done"},
	)
	sink := &recordSink{}
	a := New(mp, sub, NewSession(""), Options{}, sink)
	if err := a.Run(context.Background(), "probe the toolchain"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	results := sink.kinds(event.ToolResult)
	if len(results) != 1 {
		t.Fatalf("ToolResult events = %d, want 1", len(results))
	}
	got := results[0].Tool
	if got.Err == "" {
		t.Fatalf("refused call must carry an error so a frontend can show it as blocked: %+v", got)
	}
	if !strings.Contains(got.Output, "blocked:") {
		t.Fatalf("model-facing output should state the refusal, got %q", got.Output)
	}
	if got.Output == "ran" {
		t.Fatal("refused command must not execute")
	}
	if got.Execution == nil || got.Execution.State != tool.ShellStateNotRun {
		t.Fatalf("refused bash card needs not-run shell metadata, got %+v", got.Execution)
	}
}
