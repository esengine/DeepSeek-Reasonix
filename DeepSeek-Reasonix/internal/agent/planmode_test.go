package agent

import (
	"context"
	"reasonix/internal/event"
	"strings"
	"testing"

	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// TestPlanModeBlocksWriters proves the read-only gate refuses non-ReadOnly
// tools while leaving the read-only ones to run normally. The returned tool
// result starts with "blocked:" so the model can adapt mid-turn.
func TestPlanModeBlocksWriters(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "read_only_tool", readOnly: true})
	reg.Add(fakeTool{name: "writer_tool", readOnly: false})

	a := New(nil, reg, NewSession(""), Options{}, event.Discard)
	a.SetPlanMode(true)

	ro := a.executeOne(context.Background(), provider.ToolCall{Name: "read_only_tool"})
	if !strings.Contains(ro.output, "done") {
		t.Errorf("read-only tool in plan mode should still run: %q", ro.output)
	}

	wr := a.executeOne(context.Background(), provider.ToolCall{Name: "writer_tool"})
	if !strings.HasPrefix(wr.output, "blocked:") {
		t.Errorf("writer tool in plan mode should return a 'blocked:' result, got: %q", wr.output)
	}
}

// TestPlanModeDoesNotMutateSystemOrTools is the cache-stability test. Toggling
// plan mode between two stream calls must not change the system prompt or the
// tool list seen by the provider — those are the cache-key prefix, and any
// change there forces an expensive cache miss.
func TestPlanModeDoesNotMutateSystemOrTools(t *testing.T) {
	prov := &mockProvider{name: "p", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "ok"},
		{Type: provider.ChunkDone},
	}}
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "read_file", readOnly: true})
	reg.Add(fakeTool{name: "write_file", readOnly: false})

	a := New(prov, reg, NewSession("STABLE-SYS"), Options{}, event.Discard)

	// Auto-mode round-trip.
	if err := a.Run(context.Background(), "explore"); err != nil {
		t.Fatalf("auto Run: %v", err)
	}
	autoSys := prov.lastReq.Messages[0]
	autoTools := serializeToolNames(prov.lastReq.Tools)

	// Flip the gate and run again. The user message changes (new turn), but
	// the system message and the tool list both have to come back identical.
	prov.chunks = []provider.Chunk{
		{Type: provider.ChunkText, Text: "ok"},
		{Type: provider.ChunkDone},
	}
	a.SetPlanMode(true)
	if err := a.Run(context.Background(), "now in plan mode"); err != nil {
		t.Fatalf("plan Run: %v", err)
	}
	planSys := prov.lastReq.Messages[0]
	planTools := serializeToolNames(prov.lastReq.Tools)

	if planSys.Role != autoSys.Role || planSys.Content != autoSys.Content {
		t.Errorf("system message changed across mode toggle:\n auto: %+v\n plan: %+v", autoSys, planSys)
	}
	if planTools != autoTools {
		t.Errorf("tool list changed across mode toggle:\n auto: %s\n plan: %s", autoTools, planTools)
	}
}

func serializeToolNames(ts []provider.ToolSchema) string {
	var names []string
	for _, t := range ts {
		names = append(names, t.Name)
	}
	return strings.Join(names, ",")
}
