package agent

import (
	"context"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

func TestClosedLoopRejectsTextOnlyImplementationClaim(t *testing.T) {
	reg := evidenceRegistry()
	reg.Add(fakeTool{name: "read_file", readOnly: true})
	prov := &scriptedProvider{name: "delivery", turns: [][]provider.Chunk{
		{{Type: provider.ChunkText, Text: "implemented"}, {Type: provider.ChunkDone}},
		{toolCallChunk("criteria", "todo_write", `{"todos":[{"content":"Implement main","status":"in_progress"}]}`), {Type: provider.ChunkDone}},
		{toolCallChunk("write", "write_file", `{"path":"main.go"}`), {Type: provider.ChunkDone}},
		{toolCallChunk("review", "read_file", `{"path":"main.go"}`), {Type: provider.ChunkDone}},
		{toolCallChunk("verify", "bash", `{"command":"go test ./..."}`), {Type: provider.ChunkDone}},
		{toolCallChunk("signoff", "complete_step", `{"step":"Implement main","result":"implemented","evidence":[{"kind":"verification","summary":"tests pass","command":"go test ./..."}]}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "implemented with evidence"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, NewSession(""), Options{}, event.Discard)
	err := a.Run(withClosedLoopContext(context.Background()), "implement main")
	if err != nil {
		t.Fatalf("text-only claim without a writer = %v, want ready", err)
	}
	if prov.call != 1 {
		t.Fatalf("provider calls = %d, want 1 (text-only claim ends immediately)", prov.call)
	}
}
