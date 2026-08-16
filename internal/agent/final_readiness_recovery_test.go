package agent

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

func TestTargetedVerificationGapDoesNotArmRecovery(t *testing.T) {
	reg := evidenceRegistry()
	prov := &scriptedProvider{name: "standard", turns: [][]provider.Chunk{
		{toolCallChunk("write", "write_file", `{"path":"docs/verify_v070.md"}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "validation script passed"}, {Type: provider.ChunkDone}},
		{toolCallChunk("recognized-check", "bash", `{"command":"git diff --check"}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "checks complete"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, NewSession("sys"), Options{}, event.Discard)

	if err := a.Run(context.Background(), "write docs/verify_v070.md and run the validation script"); err != nil {
		t.Fatalf("targeted Run returned a readiness failure: %v", err)
	}
	if a.closedLoopActive() {
		t.Fatal("ordinary targeted turn unexpectedly elevated to closed loop")
	}
	if a.PrepareDeliveryRecovery() {
		t.Fatal("a soft targeted-verification gap must not create a recovery card")
	}
	receipt := a.CompletionReceipt()
	if receipt == nil || receipt.Verdict == "complete" {
		t.Fatalf("completion receipt = %+v, want a visible non-complete verdict", receipt)
	}
}

func TestTargetedVerificationRecoverySurvivesAgentRebuild(t *testing.T) {
	reg := evidenceRegistry()
	first := &scriptedProvider{name: "standard", turns: [][]provider.Chunk{
		{toolCallChunk("todo", "todo_write", `{"todos":[{"content":"Write verification notes","status":"in_progress"}]}`), {Type: provider.ChunkDone}},
		{toolCallChunk("write", "write_file", `{"path":"docs/verify_v070.md","content":"sensitive-payload"}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "validation script passed"}, {Type: provider.ChunkDone}},
	}}
	session := NewSession("sys")
	a := New(first, reg, session, Options{}, event.Discard)

	var readinessErr *FinalReadinessError
	if err := a.Run(context.Background(), "write docs/verify_v070.md and run the validation script"); !errors.As(err, &readinessErr) {
		t.Fatalf("first Run error = %v, want final readiness failure", err)
	}
	var marker *provider.FinalReadinessRecovery
	for _, message := range session.Snapshot() {
		if message.FinalReadinessRecovery != nil {
			marker = message.FinalReadinessRecovery
		}
	}
	if marker == nil || bytes.Contains(marker.Checkpoint, []byte("sensitive-payload")) {
		t.Fatal("recovery checkpoint missing or duplicated writer content")
	}
	path := filepath.Join(t.TempDir(), "readiness-recovery.jsonl")
	if err := session.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}

	recoveryProvider := &scriptedProvider{name: "standard-reloaded", turns: [][]provider.Chunk{
		{toolCallChunk("recognized-check", "bash", `{"command":"git diff --check"}`), {Type: provider.ChunkDone}},
		{toolCallChunk("signoff", "complete_step", `{"step":"Write verification notes","result":"done","evidence":[{"kind":"verification","summary":"diff check passed","command":"git diff --check"}]}`), {Type: provider.ChunkDone}},
		{toolCallChunk("todo-done", "todo_write", `{"todos":[{"content":"Write verification notes","status":"completed"}]}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "checks complete"}, {Type: provider.ChunkDone}},
	}}
	reloaded := New(recoveryProvider, reg, loaded, Options{}, event.Discard)
	if !reloaded.PrepareFinalReadinessRecovery() || reloaded.PrepareFinalReadinessRecovery() {
		t.Fatal("rebuilt recovery authorization was not one-shot")
	}
	if err := reloaded.Run(context.Background(), "continue the remaining checks"); err != nil {
		t.Fatalf("reloaded recovery Run: %v", err)
	}
	for _, message := range loaded.Snapshot() {
		if message.FinalReadinessRecovery != nil && message.FinalReadinessRecovery.Pending {
			t.Fatal("started recovery left its durable action pending")
		}
	}
	writer, ok := reloaded.task.ledger.LatestSuccessfulWriterIndex()
	if !ok || !reloaded.task.ledger.HasSuccessfulVerificationCommandAfter(writer) {
		t.Fatal("reloaded recovery did not preserve write-before-verification ordering")
	}
}

func TestFinalReadinessRecoveryRejectsStaleMarkerAfterUserTurn(t *testing.T) {
	reg := evidenceRegistry()
	prov := &scriptedProvider{name: "standard", turns: [][]provider.Chunk{
		{toolCallChunk("write", "write_file", `{"path":"main.go"}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}
	session := NewSession("sys")
	a := New(prov, reg, session, Options{}, event.Discard)
	var readinessErr *FinalReadinessError
	if err := a.Run(withClosedLoopContext(context.Background()), "change README.md"); !errors.As(err, &readinessErr) {
		t.Fatalf("Run error = %v, want final readiness failure", err)
	}
	session.Add(provider.Message{Role: provider.RoleUser, Content: "unrelated follow-up"})
	reloaded := New(nil, reg, session, Options{}, event.Discard)
	if reloaded.PrepareFinalReadinessRecovery() {
		t.Fatal("stale readiness marker after a newer user turn must be rejected")
	}
}

func TestTodoOnlyReadinessMarkerConsumedOnReloadWhenTodosComplete(t *testing.T) {
	session := NewSession("sys")
	// A completed todo list already lives in the transcript, and a todo-only
	// readiness marker is still pending (the gated turn was the last one).
	session.Add(provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{
		{ID: "todo-done", Name: "todo_write", Arguments: `{"todos":[{"content":"Write verification notes","status":"completed"}]}`},
	}})
	session.Add(provider.Message{Role: provider.RoleTool, ToolCallID: "todo-done", Content: "task list updated"})
	session.Add(provider.Message{
		Role:       provider.RoleTool,
		ToolCallID: provider.LocalOnlyToolID,
		Name:       provider.LocalOnlyToolName,
		LocalOnly:  true,
		FinalReadinessRecovery: &provider.FinalReadinessRecovery{
			Pending: true,
			Missing: []string{"todo"},
		},
	})

	reloaded := New(nil, evidenceRegistry(), session, Options{}, event.Discard)
	reloaded.SetSession(session)
	for _, message := range session.Snapshot() {
		if message.FinalReadinessRecovery != nil && message.FinalReadinessRecovery.Pending {
			t.Fatal("todo-only readiness marker stayed pending after the rebuilt canonical list showed every todo complete")
		}
	}
}

func TestReadinessMarkerSurvivesReloadWhenGapExceedsTodos(t *testing.T) {
	session := NewSession("sys")
	session.Add(provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{
		{ID: "todo-done", Name: "todo_write", Arguments: `{"todos":[{"content":"Write verification notes","status":"completed"}]}`},
	}})
	session.Add(provider.Message{Role: provider.RoleTool, ToolCallID: "todo-done", Content: "task list updated"})
	session.Add(provider.Message{
		Role:       provider.RoleTool,
		ToolCallID: provider.LocalOnlyToolID,
		Name:       provider.LocalOnlyToolName,
		LocalOnly:  true,
		FinalReadinessRecovery: &provider.FinalReadinessRecovery{
			Pending: true,
			Missing: []string{"todo", "verification"},
		},
	})

	reloaded := New(nil, evidenceRegistry(), session, Options{}, event.Discard)
	reloaded.SetSession(session)
	pending := 0
	for _, message := range session.Snapshot() {
		if message.FinalReadinessRecovery != nil && message.FinalReadinessRecovery.Pending {
			pending++
		}
	}
	if pending != 1 {
		t.Fatalf("pending markers = %d, want 1: a marker listing non-todo gaps must survive", pending)
	}
}

func TestTodoOnlyReadinessMarkerSurvivesReloadWhileTodosIncomplete(t *testing.T) {
	session := NewSession("sys")
	session.Add(provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{
		{ID: "todo-open", Name: "todo_write", Arguments: `{"todos":[{"content":"Write verification notes","status":"in_progress"}]}`},
	}})
	session.Add(provider.Message{Role: provider.RoleTool, ToolCallID: "todo-open", Content: "task list updated"})
	session.Add(provider.Message{
		Role:       provider.RoleTool,
		ToolCallID: provider.LocalOnlyToolID,
		Name:       provider.LocalOnlyToolName,
		LocalOnly:  true,
		FinalReadinessRecovery: &provider.FinalReadinessRecovery{
			Pending: true,
			Missing: []string{"todo"},
		},
	})

	reloaded := New(nil, evidenceRegistry(), session, Options{}, event.Discard)
	reloaded.SetSession(session)
	pending := 0
	for _, message := range session.Snapshot() {
		if message.FinalReadinessRecovery != nil && message.FinalReadinessRecovery.Pending {
			pending++
		}
	}
	if pending != 1 {
		t.Fatalf("pending markers = %d, want 1: incomplete todos keep the marker actionable", pending)
	}
}
