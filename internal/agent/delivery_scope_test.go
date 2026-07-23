package agent

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func TestDeliveryExecutionScopeDoesNotChangeProviderRequestBytes(t *testing.T) {
	turn := []provider.Chunk{{Type: provider.ChunkText, Text: "Here is the explanation."}, {Type: provider.ChunkDone}}
	unscopedProvider := &scriptedProvider{name: "delivery", turns: [][]provider.Chunk{turn}}
	scopedProvider := &scriptedProvider{name: "delivery", turns: [][]provider.Chunk{turn}}
	reg := tool.NewRegistry()
	reg.Add(fakeReadFileTool{})

	unscoped := New(unscopedProvider, reg, NewSession("stable system"), Options{DeliveryProfile: true}, event.Discard)
	scoped := New(scopedProvider, reg, NewSession("stable system"), Options{DeliveryProfile: true}, event.Discard)
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

func TestDeliveryGoalContinueDefersTaskMutationExpectation(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeReadFileTool{})
	reg.Add(fakeWriterTool{})
	prov := &scriptedProvider{name: "delivery", turns: [][]provider.Chunk{
		{toolCallChunk("read", "read_file", `{"path":"main.go"}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "Investigation complete.\n\n[goal:continue]"}, {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "Implemented.\n\n[goal:complete]"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, NewSession(""), Options{DeliveryProfile: true}, event.Discard)
	ctx := deliveryGoalContext("goal-1", "fix the crash in main.go")
	if err := a.Run(ctx, "investigate the crash"); err != nil {
		t.Fatalf("continue turn: %v", err)
	}
	err := a.Run(ctx, "continue the goal")
	var readiness *FinalReadinessError
	if !errors.As(err, &readiness) || !strings.Contains(readiness.Reason, "state change") {
		t.Fatalf("complete without any goal mutation err = %v, want mutation readiness failure", err)
	}
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
		{{Type: provider.ChunkText, Text: "Implementation complete.\n\n[goal:continue]"}, {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "Final summary.\n\n[goal:complete]"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, NewSession(""), Options{DeliveryProfile: true}, event.Discard)
	ctx := deliveryGoalContext("goal-1", "implement main")
	if err := a.Run(ctx, "implement the first chunk"); err != nil {
		t.Fatalf("mutation turn: %v", err)
	}
	if err := a.Run(ctx, "finish the goal"); err != nil {
		t.Fatalf("verification-only completion should reuse scoped evidence: %v", err)
	}
}

func TestDeliveryGoalRestoredPendingMutationCompletesWithoutNewWrite(t *testing.T) {
	// A controller rebuild or cold resume restores PendingMutation with no
	// mutation receipt in the fresh ledger (a -1 baseline). Fresh review,
	// verification, and sign-off receipts must finish the Goal without
	// manufacturing another write, and the checkpoint must clear.
	reg := evidenceRegistry()
	reg.Add(fakeTool{name: "read_file", readOnly: true})
	prov := &scriptedProvider{name: "delivery", turns: [][]provider.Chunk{
		{toolCallChunk("review", "read_file", `{"path":"main.go"}`), {Type: provider.ChunkDone}},
		{toolCallChunk("verify", "bash", `{"command":"go test ./..."}`), {Type: provider.ChunkDone}},
		{toolCallChunk("signoff", "complete_step", `{"step":"Ship main","result":"implemented","evidence":[{"kind":"verification","summary":"tests pass","command":"go test ./..."}]}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "Recovered and verified.\n\n[goal:complete]"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, NewSession(""), Options{DeliveryProfile: true}, event.Discard)
	a.RestoreDeliveryCheckpoint(evidence.DeliveryCheckpoint{
		ScopeID:             "goal-1",
		CriteriaEstablished: true,
		WorkObserved:        true,
		MutationObserved:    true,
		PendingMutation:     true,
	})
	if err := a.Run(deliveryGoalContext("goal-1", "implement main"), "continue the goal"); err != nil {
		t.Fatalf("restored pending mutation should complete with fresh review/verification/sign-off: %v", err)
	}
	if cp := a.DeliveryCheckpoint(); cp.PendingMutation {
		t.Fatalf("checkpoint = %+v, want PendingMutation cleared after sign-off", cp)
	}
}

func TestDeliveryGoalNewMutationInvalidatesPriorSignoff(t *testing.T) {
	reg := evidenceRegistry()
	reg.Add(fakeTool{name: "read_file", readOnly: true})
	prov := &scriptedProvider{name: "delivery", turns: [][]provider.Chunk{
		{toolCallChunk("criteria-1", "todo_write", `{"todos":[{"content":"Ship main","status":"in_progress"}]}`), {Type: provider.ChunkDone}},
		{toolCallChunk("write-1", "write_file", `{"path":"main.go"}`), {Type: provider.ChunkDone}},
		{toolCallChunk("review-1", "read_file", `{"path":"main.go"}`), {Type: provider.ChunkDone}},
		{toolCallChunk("verify-1", "bash", `{"command":"go test ./..."}`), {Type: provider.ChunkDone}},
		{toolCallChunk("signoff-1", "complete_step", `{"step":"Ship main","result":"implemented","evidence":[{"kind":"verification","summary":"tests pass","command":"go test ./..."}]}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "First chunk done.\n\n[goal:continue]"}, {Type: provider.ChunkDone}},
		{toolCallChunk("criteria-2", "todo_write", `{"todos":[{"content":"Polish main","status":"in_progress"}]}`), {Type: provider.ChunkDone}},
		{toolCallChunk("write-2", "write_file", `{"path":"main.go"}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "All done.\n\n[goal:complete]"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, NewSession(""), Options{DeliveryProfile: true}, event.Discard)
	ctx := deliveryGoalContext("goal-1", "implement main")
	if err := a.Run(ctx, "implement the first chunk"); err != nil {
		t.Fatalf("first turn: %v", err)
	}
	err := a.Run(ctx, "polish and finish")
	var readiness *FinalReadinessError
	if !errors.As(err, &readiness) || !strings.Contains(readiness.Reason, "verification") {
		t.Fatalf("new mutation err = %v, want fresh verification failure", err)
	}
}
