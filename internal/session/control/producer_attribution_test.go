package control

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reasonix/internal/state/sessionstore"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/eventwire"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/runtime/agent/testutil"
	"reasonix/internal/runtime/coordinator"
)

// TestTwoModelTurnSaysWhichModelWroteEachFrame runs a real plan-and-execute turn
// and reads the producer off the frames themselves. Both models answer and both
// call a tool, so a stream that named neither — or named them by which phase
// marker came last — would put one model's work under the other's name.
func TestTwoModelTurnSaysWhichModelWroteEachFrame(t *testing.T) {
	dir := testenv.TempDir(t)
	sess := sessionstore.NewSession("sys")
	execTools := tool.NewRegistry()
	execTools.Add(&attributionProbeTool{})
	exec := agent.New(testutil.NewMock("exec",
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "exec-probe", Name: "probe", Arguments: `{}`}}},
		testutil.Turn{Reasoning: "that settles it", Text: "executor answered"},
	), execTools, sess, agent.Options{}, event.Discard)

	plannerTools := tool.NewRegistry()
	plannerTools.Add(&attributionProbeTool{})
	sink, done, events := collectSink()
	coordinator := coordinator.NewCoordinatorWithPlannerPolicy(
		testutil.NewMock("planner",
			testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "planner-probe", Name: "probe", Arguments: `{}`}}},
			testutil.Turn{Reasoning: "the shape is clear", Text: "1. do the thing"},
		), sessionstore.NewSession("planner sys"), nil, plannerTools, agent.Options{}, exec, 0, sink,
		func(context.Context, string) agent.PlannerDecision {
			return agent.PlannerDecision{Route: agent.PlannerRoutePlanAndExecute, Depth: agent.PlannerDepthFull, Reason: "test"}
		},
	)
	c := New(Options{
		Runner: coordinator, Executor: exec, Sink: sink,
		SessionDir: dir, SessionPath: filepath.Join(dir, "s.jsonl"),
	})
	defer c.Close()
	defer c.autosaveWG.Wait()
	c.Submit("把这件事做掉")
	waitForDone(t, done)

	byTool := map[string]string{}
	answers := map[string]string{}
	for _, e := range events() {
		switch e.Kind {
		case event.ToolDispatch:
			byTool[e.Tool.ID] = e.Source
		case event.Message:
			if e.Text != "" {
				answers[e.Text] = e.Source
			}
		}
	}
	for id, want := range map[string]string{"planner-probe": event.UsageSourcePlanner, "exec-probe": event.UsageSourceExecutor} {
		if got, ok := byTool[id]; !ok || got != want {
			t.Fatalf("tool %s produced by %q (present=%v), want %q", id, got, ok, want)
		}
	}
	for text, want := range map[string]string{"1. do the thing": event.UsageSourcePlanner, "executor answered": event.UsageSourceExecutor} {
		if got, ok := answers[text]; !ok || got != want {
			t.Fatalf("answer %q produced by %q (present=%v), want %q", text, got, ok, want)
		}
	}
	// The fact has to survive the transport, or a rebuild off the record is
	// back to reading it from the last phase marker — which the record drops.
	for _, e := range events() {
		if e.Kind != event.ToolDispatch && e.Kind != event.Message {
			continue
		}
		if w := eventwire.ToWire(e); w.Source != e.Source {
			t.Fatalf("wire dropped the producer: %q became %q", e.Source, w.Source)
		}
	}
}

type attributionProbeTool struct{}

func (*attributionProbeTool) Name() string        { return "probe" }
func (*attributionProbeTool) Description() string { return "a read-only probe" }
func (*attributionProbeTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (*attributionProbeTool) ReadOnly() bool { return true }
func (*attributionProbeTool) Execute(context.Context, json.RawMessage) (string, error) {
	return "probed", nil
}
