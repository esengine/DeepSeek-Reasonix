package agent

import (
	"os"
	"path/filepath"
	"reasonix/internal/state/sessionstore"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/provider"
	"reasonix/internal/state/store"
)

func TestSaveRecoveryBranchInheritsValidProjection(t *testing.T) {
	dir := testenv.TempDir(t)
	path := filepath.Join(dir, "session.jsonl")
	parent := sessionstore.NewSession("sys")
	parent.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	parent.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	parent.Add(provider.Message{Role: provider.RoleUser, Content: "second"})
	if err := parent.Save(path); err != nil {
		t.Fatalf("Save parent: %v", err)
	}
	parentMsgs, parentVersion := parent.SnapshotMessagesVersion()
	st := sessionstore.CompactionState{
		SchemaVersion:     sessionstore.CompactionStateSchemaCurrent,
		TranscriptVersion: parentVersion,
		PromptCacheKey:    promptCacheKey("ws", sessionstore.BranchID(path), "test/model"),
		Projection: sessionstore.ContextProjection{
			ProjectionVersion: 1,
			CoveredCount:      2,
			CoveredPrefixHash: sessionstore.CoveredPrefixHash(parentMsgs, 2),
			Messages: []provider.Message{
				{Role: provider.RoleUser, Content: "summary"},
				{Role: provider.RoleAssistant, Content: "one"},
			},
		},
	}
	if err := sessionstore.SaveCompactionState(path, st); err != nil {
		t.Fatalf("SaveCompactionState: %v", err)
	}
	stale := sessionstore.NewSession("sys")
	stale.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	stale.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	stale.Add(provider.Message{Role: provider.RoleUser, Content: "second"})
	stale.Add(provider.Message{Role: provider.RoleUser, Content: "local only"})
	info, err := stale.SaveRecoveryBranch(sessionstore.RecoveryBranchOptions{OriginalPath: path})
	if err != nil {
		t.Fatalf("SaveRecoveryBranch: %v", err)
	}
	got, ok, err := sessionstore.LoadCompactionState(info.Path)
	if err != nil || !ok {
		t.Fatalf("LoadCompactionState recovery ok=%v err=%v, want inherited sidecar", ok, err)
	}
	if got.Projection.CoveredCount != 2 || got.Projection.CoveredPrefixHash != st.Projection.CoveredPrefixHash {
		t.Fatalf("recovery projection = %+v, want parent projection inherited", got.Projection)
	}
	recovered, err := sessionstore.LoadSession(info.Path)
	if err != nil {
		t.Fatalf("LoadSession recovery: %v", err)
	}
	recoveredMsgs, _ := recovered.SnapshotMessagesVersion()
	if n := got.Projection.CoveredCount; n <= 0 || n > len(recoveredMsgs) ||
		sessionstore.CoveredPrefixHash(recoveredMsgs, n) != got.Projection.CoveredPrefixHash {
		t.Fatal("inherited projection does not match the recovery transcript")
	}
}

func TestSaveRecoveryBranchSkipsInvalidProjection(t *testing.T) {
	dir := testenv.TempDir(t)
	path := filepath.Join(dir, "session.jsonl")
	parent := sessionstore.NewSession("sys")
	parent.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	parent.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	parent.Add(provider.Message{Role: provider.RoleUser, Content: "second"})
	if err := parent.Save(path); err != nil {
		t.Fatalf("Save parent: %v", err)
	}
	parentMsgs, parentVersion := parent.SnapshotMessagesVersion()
	st := sessionstore.CompactionState{
		SchemaVersion:     sessionstore.CompactionStateSchemaCurrent,
		TranscriptVersion: parentVersion,
		PromptCacheKey:    promptCacheKey("ws", sessionstore.BranchID(path), "test/model"),
		Projection: sessionstore.ContextProjection{
			ProjectionVersion: 1,
			CoveredCount:      2,
			CoveredPrefixHash: sessionstore.CoveredPrefixHash(parentMsgs, 2),
			Messages: []provider.Message{
				{Role: provider.RoleUser, Content: "summary"},
				{Role: provider.RoleAssistant, Content: "one"},
			},
		},
	}
	if err := sessionstore.SaveCompactionState(path, st); err != nil {
		t.Fatalf("SaveCompactionState: %v", err)
	}
	rewritten := parent.Snapshot()
	rewritten[0] = provider.Message{Role: provider.RoleUser, Content: "edited"}
	parent.Rewrite(rewritten, "test rewrite")
	if err := parent.Save(path); err != nil {
		t.Fatalf("Save rewritten parent: %v", err)
	}
	stale := sessionstore.NewSession("sys")
	stale.Add(provider.Message{Role: provider.RoleUser, Content: "edited"})
	stale.Add(provider.Message{Role: provider.RoleUser, Content: "local only"})
	info, err := stale.SaveRecoveryBranch(sessionstore.RecoveryBranchOptions{OriginalPath: path})
	if err != nil {
		t.Fatalf("SaveRecoveryBranch: %v", err)
	}
	if _, ok, err := sessionstore.LoadCompactionState(info.Path); err != nil || ok {
		t.Fatalf("LoadCompactionState recovery ok=%v err=%v, want no inherited sidecar", ok, err)
	}
}

func TestSnapshotUpToDateFastPathSkipsWALProbe(t *testing.T) {
	path := filepath.Join(testenv.TempDir(t), "session.jsonl")
	s := sessionstore.NewSession("sys")
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
