package agent

import (
	"path/filepath"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

func TestCoveredPrefixHashTracksModelVisibleToolRawContent(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "call-1", Name: "read", Arguments: `{}`}}},
		{Role: provider.RoleTool, ToolCallID: "call-1", Name: "read", Content: "bounded", RawContent: "full result A"},
	}
	first := coveredPrefixHash(msgs, len(msgs))
	msgs[1].RawContent = "full result B"
	if second := coveredPrefixHash(msgs, len(msgs)); second == first {
		t.Fatal("tool RawContent edit did not invalidate the covered-prefix hash")
	}
}

func TestLoadProjectionSidecarMigratesBoundedV3ToolHash(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "system"},
		{Role: provider.RoleUser, Content: "task"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "call-1", Name: "read", Arguments: `{}`}}},
		{Role: provider.RoleTool, ToolCallID: "call-1", Name: "read", Content: "bounded", RawContent: "full result"},
	}
	legacyHash := boundedCoveredPrefixHash(msgs, len(msgs))
	if legacyHash == coveredPrefixHash(msgs, len(msgs)) {
		t.Fatal("fixture does not distinguish bounded and model-visible hashes")
	}
	if err := SaveCompactionState(path, CompactionState{
		SchemaVersion:  compactionStateSchemaV3,
		PromptCacheKey: promptCacheKey("ws", BranchID(path), "model"),
		Projection: ContextProjection{
			Messages:          []provider.Message{{Role: provider.RoleSystem, Content: "system"}, formatSummaryMessage("summary")},
			CoveredCount:      len(msgs),
			CoveredPrefixHash: legacyHash,
		},
	}); err != nil {
		t.Fatal(err)
	}
	sess := &Session{Messages: msgs}
	a := New(nil, nil, sess, Options{SessionPath: path, WorkspaceID: "ws", ModelRef: "model"}, event.Discard)
	want := coveredPrefixHash(msgs, len(msgs))
	if got := a.sess.compactionState.Projection.CoveredPrefixHash; got != want {
		t.Fatalf("loaded hash = %q, want migrated %q", got, want)
	}
	disk, ok, err := LoadCompactionState(path)
	if err != nil || !ok {
		t.Fatalf("load migrated sidecar: ok=%v err=%v", ok, err)
	}
	if got := disk.Projection.CoveredPrefixHash; got != want {
		t.Fatalf("persisted hash = %q, want %q", got, want)
	}
	mutated := append([]provider.Message(nil), msgs...)
	mutated[len(mutated)-1].RawContent = "changed after migration"
	if projectionValid(a.sess.compactionState, mutated, a.currentPromptCacheKey()) {
		t.Fatal("migrated projection accepted changed RawContent")
	}
}
