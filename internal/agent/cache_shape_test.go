package agent

import (
	"context"
	"encoding/json"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func TestCaptureShapeNormalizesToolSchemaOrder(t *testing.T) {
	schemas := []provider.ToolSchema{
		{
			Name:        "write_file",
			Description: "write a file",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`),
		},
		{
			Name:        "read_file",
			Description: "read a file",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`),
		},
	}
	reordered := []provider.ToolSchema{schemas[1], schemas[0]}

	first := CaptureShape("system", schemas, 1)
	second := CaptureShape("system", reordered, 1)

	if first.ToolsHash != second.ToolsHash {
		t.Fatalf("ToolsHash should be stable across schema order: %q != %q", first.ToolsHash, second.ToolsHash)
	}
	if first.PrefixHash != second.PrefixHash {
		t.Fatalf("PrefixHash should be stable across schema order: %q != %q", first.PrefixHash, second.PrefixHash)
	}
	if schemas[0].Name != "write_file" || schemas[1].Name != "read_file" {
		t.Fatalf("CaptureShape mutated caller schema order: got [%s %s]", schemas[0].Name, schemas[1].Name)
	}
}

type schemaSnapshotProvider struct {
	req provider.Request
}

func (p *schemaSnapshotProvider) Name() string { return "schema-snapshot" }

func (p *schemaSnapshotProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.req = req
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "ok"}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func TestStreamUsesCapturedSchemaSnapshot(t *testing.T) {
	prov := &schemaSnapshotProvider{}
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "cached_tool", readOnly: true})
	a := New(prov, reg, NewSession("sys"), Options{}, event.Discard)

	snapshot := []provider.ToolSchema{{
		Name:        "cached_tool",
		Description: "cached schema visible at turn start",
		Parameters:  json.RawMessage(`{"type":"object"}`),
	}}

	// A lazy/background plugin can finish handshaking after prefix diagnostics
	// capture the turn-start schema surface. That newly registered tool should
	// wait for the next model request; changing Tools mid-request would perturb
	// the provider's cache key and make the diagnostics lie about the prefix.
	reg.Add(fakeTool{name: "late_tool", readOnly: true})

	if _, _, _, _, _, _, _, err := a.stream(context.Background(), 1, snapshot); err != nil {
		t.Fatalf("stream: %v", err)
	}
	if len(prov.req.Tools) != 1 || prov.req.Tools[0].Name != "cached_tool" {
		t.Fatalf("request tools = %+v, want only captured cached_tool schema", prov.req.Tools)
	}
}
