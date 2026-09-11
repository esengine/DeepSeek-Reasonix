package agent

import (
	"path/filepath"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func restartProjectionState(path string, msgs []provider.Message, version uint64, covered int) CompactionState {
	key := promptCacheKey("workspace", BranchID(path), "provider/model")
	return CompactionState{
		SchemaVersion:     compactionStateSchemaCurrent,
		TranscriptVersion: version,
		PromptCacheKey:    key,
		Projection: ContextProjection{
			Messages: []provider.Message{
				{Role: provider.RoleSystem, Content: "system"},
				{Role: provider.RoleUser, Content: "[compacted context]"},
			},
			TranscriptVersion: version,
			ProjectionVersion: 1,
			CoveredCount:      covered,
			CoveredPrefixHash: coveredPrefixHash(msgs, covered),
		},
	}
}

func reopenProjectionAgent(t *testing.T, path string) *Agent {
	t.Helper()
	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if got := loaded.TranscriptVersion(); got != 0 {
		t.Fatalf("loaded transcript version = %d, want process-local reset to 0", got)
	}
	return New(nil, tool.NewRegistry(), loaded, Options{
		SessionPath: path,
		WorkspaceID: "workspace",
		ModelRef:    "provider/model",
	}, event.Discard)
}

func TestProjectionRestoresAfterRestartWithResetTranscriptVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	sess := NewSession("system")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "task"})
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "done"})
	msgs, version := sess.snapshotMessagesVersion()
	if version == 0 {
		t.Fatal("fixture must compact at a non-zero transcript version")
	}
	if err := sess.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot: %v", err)
	}
	if err := SaveCompactionState(path, restartProjectionState(path, msgs, version, len(msgs))); err != nil {
		t.Fatalf("SaveCompactionState: %v", err)
	}

	reopened := reopenProjectionAgent(t, path)
	if got := reopened.ContextMaintenanceSnapshot().CheckpointState; got != "restored" {
		t.Fatalf("checkpoint state = %q, want restored", got)
	}
	if got := len(reopened.modelVisibleMessages()); got != 2 {
		t.Fatalf("visible messages = %d, want compacted projection", got)
	}
}

func TestProjectionRestoresAfterRestartNormalization(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	sess := NewSession("system")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "inspect"})
	sess.Add(provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
		ID: "call-1", Arguments: `{"path":"main.go"}`,
	}}})
	sess.Add(provider.Message{
		Role: provider.RoleTool, ToolCallID: "call-1", Name: "read_file", Content: "package main",
	})
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "first result"})
	covered, version := sess.snapshotMessagesVersion()
	if version == 0 {
		t.Fatal("fixture must compact at a non-zero transcript version")
	}
	state := restartProjectionState(path, covered, version, len(covered))
	state.Projection.CoveredPrefixHash = legacyCoveredPrefixHash(covered, len(covered))

	// The covered prefix contains an empty tool-call name that LoadSession repairs
	// from its result. Projection identity must normalize it on both sides of the
	// restart even when the live transcript grew after compaction.
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "continue"})
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "second result"})
	if err := sess.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot: %v", err)
	}
	if err := SaveCompactionState(path, state); err != nil {
		t.Fatalf("SaveCompactionState: %v", err)
	}

	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if got := loaded.Snapshot()[2].ToolCalls[0].Name; got != "read_file" {
		t.Fatalf("load-time tool-call normalization = %q, want read_file", got)
	}
	// Desktop resume wraps the loaded session to refresh the system prompt.
	// The exact pre-repair disk view must survive that wrapper for safe sidecar
	// migration.
	loaded = loaded.CloneWithMessages(loaded.Snapshot())
	reopened := New(nil, tool.NewRegistry(), loaded, Options{
		SessionPath: path,
		WorkspaceID: "workspace",
		ModelRef:    "provider/model",
	}, event.Discard)
	if got := reopened.ContextMaintenanceSnapshot().CheckpointState; got != "restored" {
		t.Fatalf("checkpoint state = %q, want restored", got)
	}
	if got := len(reopened.modelVisibleMessages()); got != 4 {
		t.Fatalf("visible messages = %d, want projection plus two-message live tail", got)
	}
	persisted, ok, err := LoadCompactionState(path)
	if err != nil || !ok {
		t.Fatalf("LoadCompactionState after migration: ok=%v err=%v", ok, err)
	}
	if got, want := persisted.Projection.CoveredPrefixHash, coveredPrefixHash(loaded.Snapshot(), len(covered)); got != want {
		t.Fatalf("migrated covered prefix hash = %q, want %q", got, want)
	}
}

func TestLegacyProjectionHashMigrationRejectsRealPrefixChange(t *testing.T) {
	preRepair := []provider.Message{
		{Role: provider.RoleSystem, Content: "system-v1"},
		{Role: provider.RoleUser, Content: "inspect"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "call-1", Arguments: `{}`}}},
		{Role: provider.RoleTool, ToolCallID: "call-1", Name: "read_file", Content: "result"},
	}
	current := NormalizeSession(preRepair)
	current = append([]provider.Message(nil), current...)
	current[0].Content = "system-v2"
	state := CompactionState{Projection: ContextProjection{
		Messages:          []provider.Message{{Role: provider.RoleUser, Content: "summary"}},
		CoveredCount:      len(preRepair),
		CoveredPrefixHash: legacyCoveredPrefixHash(preRepair, len(preRepair)),
	}}

	if migrateLegacyCoveredPrefixHash(&state, current, preRepair) {
		t.Fatal("legacy hash migration accepted a changed provider-visible system prompt")
	}
	if projectionContentValid(state, current) {
		t.Fatal("changed prefix unexpectedly validated the legacy projection")
	}
}
