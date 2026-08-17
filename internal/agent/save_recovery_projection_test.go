package agent

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/provider"
	"reasonix/internal/store"
)

func TestSaveRecoveryBranchInheritsValidProjection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	parent := NewSession("sys")
	parent.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	parent.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	parent.Add(provider.Message{Role: provider.RoleUser, Content: "second"})
	if err := parent.Save(path); err != nil {
		t.Fatalf("Save parent: %v", err)
	}
	parentMsgs, parentVersion := parent.snapshotMessagesVersion()
	st := CompactionState{
		SchemaVersion:     compactionStateSchemaCurrent,
		TranscriptVersion: parentVersion,
		PromptCacheKey:    promptCacheKey("ws", BranchID(path), "test/model"),
		Projection: ContextProjection{
			ProjectionVersion: 1,
			CoveredCount:      2,
			CoveredPrefixHash: coveredPrefixHash(parentMsgs, 2),
			Messages: []provider.Message{
				{Role: provider.RoleUser, Content: "summary"},
				{Role: provider.RoleAssistant, Content: "one"},
			},
		},
	}
	if err := SaveCompactionState(path, st); err != nil {
		t.Fatalf("SaveCompactionState: %v", err)
	}
	stale := NewSession("sys")
	stale.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	stale.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	stale.Add(provider.Message{Role: provider.RoleUser, Content: "second"})
	stale.Add(provider.Message{Role: provider.RoleUser, Content: "local only"})
	info, err := stale.SaveRecoveryBranch(RecoveryBranchOptions{OriginalPath: path})
	if err != nil {
		t.Fatalf("SaveRecoveryBranch: %v", err)
	}
	got, ok, err := LoadCompactionState(info.Path)
	if err != nil || !ok {
		t.Fatalf("LoadCompactionState recovery ok=%v err=%v, want inherited sidecar", ok, err)
	}
	if got.Projection.CoveredCount != 2 || got.Projection.CoveredPrefixHash != st.Projection.CoveredPrefixHash {
		t.Fatalf("recovery projection = %+v, want parent projection inherited", got.Projection)
	}
	recovered, err := LoadSession(info.Path)
	if err != nil {
		t.Fatalf("LoadSession recovery: %v", err)
	}
	recoveredMsgs, _ := recovered.snapshotMessagesVersion()
	if n := got.Projection.CoveredCount; n <= 0 || n > len(recoveredMsgs) ||
		coveredPrefixHash(recoveredMsgs, n) != got.Projection.CoveredPrefixHash {
		t.Fatal("inherited projection does not match the recovery transcript")
	}
}

func TestSaveRecoveryBranchSkipsInvalidProjection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	parent := NewSession("sys")
	parent.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	parent.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	parent.Add(provider.Message{Role: provider.RoleUser, Content: "second"})
	if err := parent.Save(path); err != nil {
		t.Fatalf("Save parent: %v", err)
	}
	parentMsgs, parentVersion := parent.snapshotMessagesVersion()
	st := CompactionState{
		SchemaVersion:     compactionStateSchemaCurrent,
		TranscriptVersion: parentVersion,
		PromptCacheKey:    promptCacheKey("ws", BranchID(path), "test/model"),
		Projection: ContextProjection{
			ProjectionVersion: 1,
			CoveredCount:      2,
			CoveredPrefixHash: coveredPrefixHash(parentMsgs, 2),
			Messages: []provider.Message{
				{Role: provider.RoleUser, Content: "summary"},
				{Role: provider.RoleAssistant, Content: "one"},
			},
		},
	}
	if err := SaveCompactionState(path, st); err != nil {
		t.Fatalf("SaveCompactionState: %v", err)
	}
	rewritten := parent.Snapshot()
	rewritten[0] = provider.Message{Role: provider.RoleUser, Content: "edited"}
	parent.Rewrite(rewritten, "test rewrite")
	if err := parent.Save(path); err != nil {
		t.Fatalf("Save rewritten parent: %v", err)
	}
	stale := NewSession("sys")
	stale.Add(provider.Message{Role: provider.RoleUser, Content: "edited"})
	stale.Add(provider.Message{Role: provider.RoleUser, Content: "local only"})
	info, err := stale.SaveRecoveryBranch(RecoveryBranchOptions{OriginalPath: path})
	if err != nil {
		t.Fatalf("SaveRecoveryBranch: %v", err)
	}
	if _, ok, err := LoadCompactionState(info.Path); err != nil || ok {
		t.Fatalf("LoadCompactionState recovery ok=%v err=%v, want no inherited sidecar", ok, err)
	}
}

func TestSnapshotUpToDateFastPathSkipsWALProbe(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	s := NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "hello"})
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatal(err)
	}
	logPath := store.SessionEventLog(path)
	if err := os.Remove(logPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(logPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("unchanged healthy checkpoint probed unusable WAL path: %v", err)
	}
}
