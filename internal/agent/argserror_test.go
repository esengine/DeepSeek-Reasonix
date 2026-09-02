package agent

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// A model that emits structurally-invalid JSON must be rejected by the host
// validator before hooks or Execute. The correction contract is deliberately
// value-free and bounded rather than echoing the full provider schema.
func TestMalformedToolArgsReturnHostValidationContract(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(NewAskTool())
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{toolCallChunk("c1", "ask", `{"questions":["q":1]}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, NewSession(""), Options{}, event.Discard)
	if err := a.Run(context.Background(), "ask me"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got := toolResult(a.sess.conversation, "ask")
	if !strings.Contains(got, "argument validation failed") ||
		!strings.Contains(got, "one valid JSON object") ||
		!strings.Contains(got, "remote_dispatched=false") {
		t.Fatalf("malformed-args result should carry the host correction contract, got %q", got)
	}
	if strings.Contains(got, `"properties"`) || strings.Contains(got, `"options"`) {
		t.Fatalf("malformed-args result must not echo the full schema, got %q", got)
	}
}

// A valid-JSON arg that violates the schema must surface the precise keyword
// and expectation without echoing the full schema.
func TestValidArgsErrorOmitsSchema(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(NewAskTool())
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{toolCallChunk("c1", "ask", `{"questions":[{"question":"q","header":"h","options":[{"label":"a"}]}]}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, NewSession(""), Options{}, event.Discard)
	if err := a.Run(context.Background(), "ask me"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got := toolResult(a.sess.conversation, "ask")
	if strings.Contains(got, `"properties"`) {
		t.Fatalf("a valid-JSON arg error must not get the full schema, got %q", got)
	}
	if !strings.Contains(got, "minItems") || !strings.Contains(got, "at least 2 items") {
		t.Fatalf("expected the precise host validation error, got %q", got)
	}
}
