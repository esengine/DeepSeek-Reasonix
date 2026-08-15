package agent

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/provider"
)

func TestSaveConflictRecoveryBranchAtCapStaysBounded(t *testing.T) {
	dir := t.TempDir()
	path, stale := divergedSessionPair(t, dir, "session.jsonl")
	stampRecoveryMeta(t, path, SessionRecoveryMaxDepth)

	current := path
	var firstPath string
	for i := range 6 {
		stale.Add(provider.Message{Role: provider.RoleUser, Content: fmt.Sprintf("tick %d", i)})
		info, err := stale.SaveRecoveryBranch(RecoveryBranchOptions{OriginalPath: current})
		if err != nil {
			t.Fatalf("tick %d: SaveRecoveryBranch: %v", i, err)
		}
		if firstPath == "" {
			firstPath = info.Path
			if err := SetSessionInFlightTurn(firstPath, InFlightTurnMeta{StartMessageIndex: 2}); err != nil {
				t.Fatalf("SetSessionInFlightTurn: %v", err)
			}
		} else if info.Path != firstPath {
			t.Fatalf("tick %d rotated recovery path %q -> %q", i, firstPath, info.Path)
		}
		current = info.Path
	}

	meta, ok, err := LoadBranchMeta(current)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta ok=%v err=%v", ok, err)
	}
	if meta.InFlightTurn == nil || meta.InFlightTurn.StartMessageIndex != 2 {
		t.Fatalf("in-flight turn marker = %+v, want StartMessageIndex 2", meta.InFlightTurn)
	}

	matches, err := filepath.Glob(filepath.Join(dir, "*-recovery-*.jsonl"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	var isolated []string
	for _, m := range matches {
		if !strings.HasSuffix(m, ".events.jsonl") {
			isolated = append(isolated, m)
		}
	}
	if len(isolated) != 1 {
		t.Fatalf("isolated copies = %d, want 1: %v", len(isolated), isolated)
	}
}

func TestFixedWriterRecoverySessionPathUsesRootStem(t *testing.T) {
	dir := t.TempDir()
	root := fixedWriterRecoverySessionPath(filepath.Join(dir, "session.jsonl"))
	fork := fixedWriterRecoverySessionPath(filepath.Join(dir, "session-recovery-abcd1234abcd1234.jsonl"))
	if root != fork {
		t.Fatalf("nested recovery name mapped to %q, want root path %q", fork, root)
	}
	if again := fixedWriterRecoverySessionPath(root); again != root {
		t.Fatalf("isolated copy is not a fixed point: %s -> %s", root, again)
	}
	suffix := strings.TrimPrefix(BranchID(root), "session-recovery-")
	if len(suffix) != 16 {
		t.Fatalf("recovery suffix length = %d, want 16 for legacy discovery: %q", len(suffix), suffix)
	}
}

func TestRecoveryLaneCollisionPreservesIndependentTranscript(t *testing.T) {
	dir := t.TempDir()
	original := filepath.Join(dir, "session.jsonl")
	first := NewSession("sys")
	first.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	first.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	firstInfo, err := first.SaveConflictRecoveryBranch(RecoveryBranchOptions{OriginalPath: original})
	if err != nil {
		t.Fatal(err)
	}

	second := NewSession("sys")
	second.Add(provider.Message{Role: provider.RoleUser, Content: "second"})
	second.Add(provider.Message{Role: provider.RoleAssistant, Content: "two"})
	// Force the allocator onto the first live session's lane. The save must
	// rotate instead of replacing that independent history.
	second.recoveryLane = first.recoveryLane
	secondInfo, err := second.SaveConflictRecoveryBranch(RecoveryBranchOptions{OriginalPath: original})
	if err != nil {
		t.Fatal(err)
	}
	if secondInfo.Path == firstInfo.Path {
		t.Fatalf("independent sessions shared recovery lane %q", firstInfo.Path)
	}
	kept, err := LoadSession(firstInfo.Path)
	if err != nil {
		t.Fatal(err)
	}
	if got := kept.Snapshot()[2].Content; got != "one" {
		t.Fatalf("first recovery content = %q, want preserved", got)
	}

	second.Add(provider.Message{Role: provider.RoleUser, Content: "continued"})
	second.Add(provider.Message{Role: provider.RoleAssistant, Content: "again"})
	again, err := second.SaveConflictRecoveryBranch(RecoveryBranchOptions{OriginalPath: secondInfo.Path})
	if err != nil {
		t.Fatal(err)
	}
	if again.Path != secondInfo.Path {
		t.Fatalf("owned recovery lane rotated: %q -> %q", secondInfo.Path, again.Path)
	}
}
