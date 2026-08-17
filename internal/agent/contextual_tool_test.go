package agent

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

type unavailableTool struct {
	fakeTool
	calls *int32
}

func (u unavailableTool) ProviderVisible(context.Context) bool { return false }

func (u unavailableTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	if u.calls != nil {
		atomic.AddInt32(u.calls, 1)
	}
	return u.fakeTool.Execute(ctx, args)
}

func TestContextualToolHostGateRunsBeforePermissionAndExecute(t *testing.T) {
	var executions int32
	reg := tool.NewRegistry()
	reg.Add(unavailableTool{fakeTool: fakeTool{name: "phase_tool", readOnly: false}, calls: &executions})
	gate := &recordingPermissionGate{allow: true}
	a := New(nil, reg, NewSession("sys"), Options{Gate: gate}, event.Discard)

	out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{ID: "phase", Name: "phase_tool", Arguments: `{}`})
	if !out.blocked || !strings.Contains(out.output, "unavailable") {
		t.Fatalf("contextual tool outcome = %+v", out)
	}
	if executions != 0 || len(gate.calls) != 0 {
		t.Fatalf("unavailable tool crossed host gate: executions=%d permission=%+v", executions, gate.calls)
	}
}

func TestMixedContextualBatchExecutesAvailableCallsOnce(t *testing.T) {
	var executions int32
	reg := tool.NewRegistry()
	reg.Add(unavailableTool{fakeTool: fakeTool{name: "phase_tool", readOnly: true}, calls: &executions})
	reg.Add(fakeTool{name: "read_file", readOnly: true, calls: &executions})
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{toolCallChunk("phase", "phase_tool", `{}`), toolCallChunk("read", "read_file", `{}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "answer after the available read"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, NewSession("sys"), Options{}, event.Discard)
	if err := a.Run(context.Background(), "inspect the file"); err != nil {
		t.Fatalf("mixed contextual batch failed: %v", err)
	}
	if got := atomic.LoadInt32(&executions); got != 1 {
		t.Fatalf("available tool executions = %d, want exactly one", got)
	}
	if got := lastToolResult(a.Session(), "phase_tool"); !strings.Contains(got, "unavailable") {
		t.Fatalf("contextual result = %q", got)
	}
}

func TestRepeatedMixedContextualBatchStopsAllCalls(t *testing.T) {
	var executions int32
	reg := tool.NewRegistry()
	reg.Add(unavailableTool{fakeTool: fakeTool{name: "phase_tool", readOnly: true}, calls: &executions})
	reg.Add(fakeTool{name: "read_file", readOnly: true, calls: &executions})
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{toolCallChunk("phase-1", "phase_tool", `{}`), toolCallChunk("read-1", "read_file", `{}`), {Type: provider.ChunkDone}},
		{toolCallChunk("phase-2", "phase_tool", `{}`), toolCallChunk("read-2", "read_file", `{}`), {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, NewSession("sys"), Options{}, event.Discard)
	err := a.Run(context.Background(), "inspect the file")
	if err == nil || !strings.Contains(err.Error(), "context-unavailable tools") {
		t.Fatalf("repeated contextual batch error = %v", err)
	}
	if got := atomic.LoadInt32(&executions); got != 1 {
		t.Fatalf("available tool was re-executed after repair: %d", got)
	}
	if got := lastToolResult(a.Session(), "read_file"); !strings.Contains(got, "called again") {
		t.Fatalf("second legal call was not paired with stop result: %q", got)
	}
}

func TestRepeatedPureContextualCallWithVisibleAnswerFinishes(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(unavailableTool{fakeTool: fakeTool{name: "phase_tool", readOnly: true}})
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{toolCallChunk("phase-1", "phase_tool", `{}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "visible answer"}, toolCallChunk("phase-2", "phase_tool", `{}`), {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, NewSession("sys"), Options{}, event.Discard)
	if err := a.Run(context.Background(), "answer normally"); err != nil {
		t.Fatalf("visible answer after repeated contextual call failed: %v", err)
	}
	if got := lastAssistantContent(a.Session()); got != "visible answer" {
		t.Fatalf("final answer = %q", got)
	}
}
