package agent

import (
	"context"
	"reasonix/internal/state/sessionstore"
	"reflect"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
)

func TestDeliveryExecutionScopeDoesNotChangeProviderRequestBytes(t *testing.T) {
	turn := []provider.Chunk{{Type: provider.ChunkText, Text: "Here is the explanation."}, {Type: provider.ChunkDone}}
	unscopedProvider := &scriptedProvider{name: "delivery", turns: [][]provider.Chunk{turn}}
	scopedProvider := &scriptedProvider{name: "delivery", turns: [][]provider.Chunk{turn}}
	reg := tool.NewRegistry()
	reg.Add(fakeReadFileTool{})

	unscoped := New(unscopedProvider, reg, sessionstore.NewSession("stable system"), Options{DeliveryProfile: true}, event.Discard)
	scoped := New(scopedProvider, reg, sessionstore.NewSession("stable system"), Options{DeliveryProfile: true}, event.Discard)
	input := "explain the current implementation"
	if err := unscoped.Run(context.Background(), input); err != nil {
		t.Fatalf("unscoped run: %v", err)
	}
	if err := scoped.Run(deliveryGoalContext("goal-stable", input), input); err != nil {
		t.Fatalf("scoped run: %v", err)
	}
	if len(unscopedProvider.requests) != 1 || len(scopedProvider.requests) != 1 {
		t.Fatalf("request counts = (%d, %d), want one each", len(unscopedProvider.requests), len(scopedProvider.requests))
	}
	left, right := unscopedProvider.requests[0], scopedProvider.requests[0]
	if !reflect.DeepEqual(left.Messages, right.Messages) {
		t.Fatalf("Delivery scope changed provider-visible messages:\nunscoped=%+v\nscoped=%+v", left.Messages, right.Messages)
	}
	if !reflect.DeepEqual(left.Tools, right.Tools) {
		t.Fatal("Delivery scope changed provider-visible tool schemas")
	}
}

func deliveryGoalContext(id, task string) context.Context {
	return WithDeliveryExecutionScope(context.Background(), DeliveryExecutionScope{ID: id, TaskText: task})
}
func TestDeliveryGoalScopeCarriesSignedOffMutationAcrossTurns(t *testing.T) {
	reg := evidenceRegistry()
	reg.Add(fakeTool{name: "read_file", readOnly: true})
	prov := &scriptedProvider{name: "delivery", turns: [][]provider.Chunk{
		{toolCallChunk("criteria", "todo_write", `{"todos":[{"content":"Ship main","status":"in_progress"}]}`), {Type: provider.ChunkDone}},
		{toolCallChunk("write", "write_file", `{"path":"main.go"}`), {Type: provider.ChunkDone}},
		{toolCallChunk("review", "read_file", `{"path":"main.go"}`), {Type: provider.ChunkDone}},
		{toolCallChunk("verify", "bash", `{"command":"go test ./..."}`), {Type: provider.ChunkDone}},
		{toolCallChunk("signoff", "complete_step", `{"step":"Ship main","result":"implemented","evidence":[{"kind":"verification","summary":"tests pass","command":"go test ./..."}]}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "Implementation complete."}, {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "Final summary."}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, sessionstore.NewSession(""), Options{DeliveryProfile: true}, event.Discard)
	ctx := deliveryGoalContext("goal-1", "implement main")
	if err := a.Run(ctx, "implement the first chunk"); err != nil {
		t.Fatalf("mutation turn: %v", err)
	}
	if err := a.Run(ctx, "finish the goal"); err != nil {
		t.Fatalf("verification-only completion should reuse scoped evidence: %v", err)
	}
}
