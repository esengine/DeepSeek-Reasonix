package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/provider"
)

func TestStableRecoveryPathPeelsNestedNames(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "chat.jsonl")
	nested := filepath.Join(dir, "chat-recovery-aaaaaaaaaaaaaaaa-recovery-bbbbbbbbbbbbbbbb.jsonl")
	gen := "gen-7"
	if got, want := stableRecoverySessionPath(nested, gen), stableRecoverySessionPath(root, gen); got != want {
		t.Fatalf("nested path %q, want %q", got, want)
	}
}

func TestSaveRecoveryBranchStampsStableRootDepth(t *testing.T) {
	dir := t.TempDir()

	path, stale := divergedSessionPair(t, dir, "session.jsonl")
	info, err := stale.SaveRecoveryBranch(RecoveryBranchOptions{OriginalPath: path})
	if err != nil {
		t.Fatalf("SaveRecoveryBranch: %v", err)
	}
	if info.Meta.RecoveryDepth != 1 {
		t.Fatalf("first fork depth = %d, want 1", info.Meta.RecoveryDepth)
	}
	if info.Meta.ParentID != BranchID(path) {
		t.Fatalf("parent = %q, want root %q", info.Meta.ParentID, BranchID(path))
	}

	deeper, staleDeeper := divergedSessionPair(t, dir, "deeper.jsonl")
	stampRecoveryMeta(t, deeper, 1)
	info, err = staleDeeper.SaveRecoveryBranch(RecoveryBranchOptions{OriginalPath: deeper})
	if err != nil {
		t.Fatalf("SaveRecoveryBranch from recovered file: %v", err)
	}
	if info.Meta.RecoveryDepth != 1 {
		t.Fatalf("stable fork depth = %d, want 1", info.Meta.RecoveryDepth)
	}
	if info.Meta.ParentID != recoveryRootID(deeper) {
		t.Fatalf("parent = %q, want root %q", info.Meta.ParentID, recoveryRootID(deeper))
	}

	capped, staleCapped := divergedSessionPair(t, dir, "capped.jsonl")
	stampRecoveryMeta(t, capped, SessionRecoveryMaxDepth)
	info, err = staleCapped.SaveRecoveryBranch(RecoveryBranchOptions{OriginalPath: capped})
	if err != nil {
		t.Fatalf("SaveRecoveryBranch at historical cap: %v", err)
	}
	if info.Path == "" || info.Path == capped {
		t.Fatalf("capped parent did not write a stable recovery file: %q", info.Path)
	}
	forks, err := filepath.Glob(filepath.Join(dir, "capped-recovery-*.jsonl"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	var recovered []string
	for _, fork := range forks {
		if !strings.HasSuffix(fork, ".events.jsonl") {
			recovered = append(recovered, fork)
		}
	}
	if len(recovered) != 1 {
		t.Fatalf("stable recovery copies = %v, want 1", recovered)
	}
}

func TestRepeatedDivergenceRewritesOneRecoveryBranch(t *testing.T) {
	dir := t.TempDir()
	path, stale := divergedSessionPair(t, dir, "session.jsonl")
	first, err := stale.SaveRecoveryBranch(RecoveryBranchOptions{OriginalPath: path})
	if err != nil {
		t.Fatalf("first SaveRecoveryBranch: %v", err)
	}
	stale.Add(provider.Message{Role: provider.RoleAssistant, Content: "more local"})
	second, err := stale.SaveRecoveryBranch(RecoveryBranchOptions{OriginalPath: first.Path})
	if err != nil {
		t.Fatalf("second SaveRecoveryBranch: %v", err)
	}
	if second.Path != first.Path {
		t.Fatalf("recovery path rotated %q -> %q", first.Path, second.Path)
	}
	matches, err := filepath.Glob(filepath.Join(dir, "*-recovery-*.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	for _, m := range matches {
		if !strings.HasSuffix(m, ".events.jsonl") {
			files = append(files, m)
		}
	}
	if len(files) != 1 {
		t.Fatalf("recovery files = %v, want 1", files)
	}
}

func TestRecoveryGenerationDoesNotReusePreviousProcessPath(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "session.jsonl")
	oldPath := stableRecoverySessionPath(root, "gen-1")
	if err := os.WriteFile(oldPath, []byte(`{"role":"user","content":"previous process"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	current := NewSession("sys")
	current.Add(provider.Message{Role: provider.RoleUser, Content: "current process"})
	writer, err := AcquireSessionWriter(root)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Release()
	if err := writer.Bind(current, 1); err != nil {
		t.Fatal(err)
	}

	info, err := current.SaveConflictRecoveryBranch(RecoveryBranchOptions{OriginalPath: root})
	if err != nil {
		t.Fatalf("SaveConflictRecoveryBranch: %v", err)
	}
	if info.Path == oldPath {
		t.Fatalf("recovery reused previous-process path %q", oldPath)
	}
}

func TestRecoveryGenerationRotatesAfterUnexpectedCollision(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "session.jsonl")
	current := NewSession("sys")
	current.Add(provider.Message{Role: provider.RoleUser, Content: "current process"})
	writer, err := AcquireSessionWriter(root)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Release()
	if err := writer.Bind(current, 7); err != nil {
		t.Fatal(err)
	}

	collidingPath := stableRecoverySessionPath(root, current.recoveryGenerationKey())
	if err := os.WriteFile(collidingPath, []byte(`{"role":"user","content":"independent recovery"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := current.SaveConflictRecoveryBranch(RecoveryBranchOptions{OriginalPath: root})
	if err != nil {
		t.Fatalf("SaveConflictRecoveryBranch: %v", err)
	}
	if info.Path == collidingPath {
		t.Fatalf("collision did not rotate recovery path %q", collidingPath)
	}
}

func TestRecoveryGenerationStaysStableAfterRecoveryLeaseRebind(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "session.jsonl")
	sess := NewSession("sys")
	firstWriter, err := AcquireSessionWriter(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := firstWriter.Bind(sess, NextSessionWriteGeneration()); err != nil {
		t.Fatal(err)
	}
	lane := sess.recoveryGenerationKey()
	firstPath := stableRecoverySessionPath(root, lane)
	firstWriter.Release()

	secondWriter, err := AcquireSessionWriter(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	defer secondWriter.Release()
	if err := secondWriter.Bind(sess, NextSessionWriteGeneration()); err != nil {
		t.Fatal(err)
	}
	if got := sess.recoveryGenerationKey(); got != lane {
		t.Fatalf("recovery lane changed across lease rebind: %q -> %q", lane, got)
	}
	if got := stableRecoverySessionPath(firstPath, sess.recoveryGenerationKey()); got != firstPath {
		t.Fatalf("second recovery target = %q, want stable %q", got, firstPath)
	}
}
