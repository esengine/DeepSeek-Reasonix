package agent

import (
	"context"
	"testing"

	"reasonix/internal/evidence"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// TestOpenTurnMayEndWithIncompleteTodosAfterWrite proves an ordinary session
// can leave a partial cross-turn plan without entering delivery recovery.
func TestOpenTurnMayEndWithIncompleteTodosAfterWrite(t *testing.T) {
	todoWrite, ok := tool.LookupBuiltin("todo_write")
	if !ok {
		t.Fatal("todo_write builtin not registered")
	}
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "write_file", readOnly: false})
	reg.Add(todoWrite)
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{
			toolCallChunk("c1", "write_file", `{"path":"changed.go","content":"package main"}`),
			toolCallChunk("c2", "todo_write", `{"todos":[{"content":"Edit code","status":"in_progress"}]}`),
			{Type: provider.ChunkDone},
		},
		{{Type: provider.ChunkText, Text: "done, more steps remain for later turns"}, {Type: provider.ChunkDone}},
	}}
	sink := &readinessAuditSink{}
	a := New(prov, reg, NewSession(""), Options{}, sink)

	if err := a.Run(withNoClosedLoop(context.Background()), "edit with todo and leave remaining steps for later"); err != nil {
		t.Fatalf("open turn with incomplete todos must not fail the run: %v", err)
	}
	if prov.call != 2 {
		t.Fatalf("provider calls = %d, want 2 (write + final, no readiness retry)", prov.call)
	}
	for _, audit := range sink.events {
		if audit.Result == evidence.ReadinessErrored {
			t.Fatalf("open turn recorded errored readiness audit: %+v", audit)
		}
	}
}
