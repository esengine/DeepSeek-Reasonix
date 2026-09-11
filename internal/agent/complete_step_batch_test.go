package agent

import (
	"context"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func batchSignoffRegistry(t *testing.T) *tool.Registry {
	t.Helper()
	reg := tool.NewRegistry()
	for _, name := range []string{"todo_write", "complete_step"} {
		builtin, ok := tool.LookupBuiltin(name)
		if !ok {
			t.Fatalf("%s builtin not registered", name)
		}
		reg.Add(builtin)
	}
	return reg
}

func TestEvidenceFlowAllowsThreeOrderedCompleteStepSignoffs(t *testing.T) {
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{toolCallChunk("t", "todo_write", `{"todos":[{"content":"one","status":"in_progress"},{"content":"two","status":"pending"},{"content":"three","status":"pending"}]}`), {Type: provider.ChunkDone}},
		{
			toolCallChunk("c1", "complete_step", `{"step":"one","result":"done","evidence":[{"kind":"manual","summary":"checked"}]}`),
			toolCallChunk("c2", "complete_step", `{"step":"two","result":"done","evidence":[{"kind":"manual","summary":"checked"}]}`),
			toolCallChunk("c3", "complete_step", `{"step":"three","result":"done","evidence":[{"kind":"manual","summary":"checked"}]}`),
			{Type: provider.ChunkDone},
		},
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, batchSignoffRegistry(t), NewSession(""), Options{}, event.Discard)
	if err := a.Run(withNoClosedLoop(context.Background()), "sign off three ordered steps"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := len(toolResults(a.sess.conversation, "complete_step")); got != 3 || prov.call != 3 {
		t.Fatalf("sign-offs/provider calls = %d/%d, want 3/3", got, prov.call)
	}
	assertCanonicalTodosCompleted(t, a)
}

func TestEvidenceFlowBatchCompletesPhaseChildrenThenPhaseHeader(t *testing.T) {
	manual := `[{"kind":"manual","summary":"checked"}]`
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{toolCallChunk("t", "todo_write", `{"todos":[{"content":"Phase","status":"pending","level":0},{"content":"child one","status":"in_progress","level":1},{"content":"child two","status":"pending","level":1},{"content":"Finalize","status":"pending","level":0}]}`), {Type: provider.ChunkDone}},
		{
			toolCallChunk("c1", "complete_step", `{"step":"child one","result":"done","evidence":`+manual+`}`),
			toolCallChunk("c2", "complete_step", `{"step":"child two","result":"done","evidence":`+manual+`}`),
			toolCallChunk("c3", "complete_step", `{"step":"Phase","result":"done","evidence":`+manual+`}`),
			toolCallChunk("c4", "complete_step", `{"step":"Finalize","result":"done","evidence":`+manual+`}`),
			{Type: provider.ChunkDone},
		},
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, batchSignoffRegistry(t), NewSession(""), Options{}, event.Discard)
	if err := a.Run(withNoClosedLoop(context.Background()), "sign off phase children and header"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := len(toolResults(a.sess.conversation, "complete_step")); got != 4 {
		t.Fatalf("complete_step results = %d, want four ordered sign-offs", got)
	}
	assertCanonicalTodosCompleted(t, a)
}

func TestEvidenceFlowMixedBatchKeepsReceiptOrderAcrossSignoffs(t *testing.T) {
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{toolCallChunk("t", "todo_write", `{"todos":[{"content":"one","status":"in_progress"},{"content":"two","status":"pending"}]}`), {Type: provider.ChunkDone}},
		{
			toolCallChunk("b1", "bash", `{"command":"go test ./one"}`),
			toolCallChunk("c1", "complete_step", `{"step":"one","result":"verified","evidence":[{"kind":"verification","summary":"one passed","command":"go test ./one"}]}`),
			toolCallChunk("b2", "bash", `{"command":"go test ./two"}`),
			toolCallChunk("c2", "complete_step", `{"step":"two","result":"verified","evidence":[{"kind":"verification","summary":"two passed","command":"go test ./two"}]}`),
			{Type: provider.ChunkDone},
		},
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, evidenceRegistry(), NewSession(""), Options{}, event.Discard)
	if err := a.Run(withNoClosedLoop(context.Background()), "verify and sign off mixed work"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := len(toolResults(a.sess.conversation, "complete_step")); got != 2 {
		t.Fatalf("complete_step results = %d, want two", got)
	}
	assertCanonicalTodosCompleted(t, a)
}

func assertCanonicalTodosCompleted(t *testing.T, a *Agent) {
	t.Helper()
	for i, todo := range a.CanonicalTodoState() {
		if todo.Status != "completed" {
			t.Fatalf("canonical todo %d = %+v, want completed", i+1, todo)
		}
	}
}
