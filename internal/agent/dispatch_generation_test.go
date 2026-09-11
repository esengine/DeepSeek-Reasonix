package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

type genShiftTool struct {
	gen string
}

func (g *genShiftTool) Name() string            { return "shift" }
func (g *genShiftTool) Description() string     { return "" }
func (g *genShiftTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (g *genShiftTool) ReadOnly() bool          { return true }
func (g *genShiftTool) Execute(context.Context, json.RawMessage) (string, error) {
	return "", nil
}
func (g *genShiftTool) ClassifyCall(json.RawMessage) tool.CallClass {
	return tool.CallClass{Known: true, ReadOnly: true, ParallelSafe: true, Generation: g.gen}
}

func TestDispatchGenerationGateBlocksStaleClassification(t *testing.T) {
	target := &genShiftTool{gen: "v1"}
	a := &Agent{}
	a.turn.loop.setDispatchClasses(map[string]tool.CallClass{
		"c1": {Known: true, ReadOnly: true, ParallelSafe: true, Generation: "v1"},
	})
	plan := &toolCallPlan{
		call: provider.ToolCall{ID: "c1", Name: "shift", Arguments: `{}`},
		tool: target,
	}
	if _, blocked := a.applyDispatchGenerationGate(plan); blocked {
		t.Fatal("matching generation must dispatch")
	}
	target.gen = "v2"
	out, blocked := a.applyDispatchGenerationGate(plan)
	if !blocked || !strings.Contains(out.output, "generation changed") {
		t.Fatalf("stale generation outcome = %+v", out)
	}
}
