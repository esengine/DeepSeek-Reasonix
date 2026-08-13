package agent

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/provider"
)

func recoveryJSONL(dir string) []string {
	matches, err := filepath.Glob(filepath.Join(dir, "*-recovery-*.jsonl"))
	if err != nil {
		return nil
	}
	var out []string
	for _, m := range matches {
		if strings.HasSuffix(m, ".events.jsonl") {
			continue
		}
		out = append(out, m)
	}
	return out
}

// #8294 growth shape: pure append autosaves must never create recovery files.
func TestSaveSnapshotStreamingAppendDoesNotDiverge(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	s := NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "hello"})
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "hi", ReasoningContent: "think"})
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("initial save: %v", err)
	}
	for i := range 20 {
		s.Add(provider.Message{Role: provider.RoleUser, Content: fmt.Sprintf("u%d", i)})
		s.Add(provider.Message{
			Role: provider.RoleAssistant, Content: fmt.Sprintf("a%d", i),
			ReasoningContent: fmt.Sprintf("r%d", i),
			ToolCalls:        []provider.ToolCall{{ID: fmt.Sprintf("c%d", i), Name: "bash", Arguments: `{"cmd":"true"}`}},
		})
		s.Add(provider.Message{Role: provider.RoleTool, ToolCallID: fmt.Sprintf("c%d", i), Name: "bash", Content: "ok", WorkDurationMs: int64(i)})
		s.Add(provider.Message{Role: provider.RoleAssistant, Content: fmt.Sprintf("done%d", i), ReasoningContent: "more"})
		if err := s.SaveSnapshot(path); err != nil {
			t.Fatalf("SaveSnapshot turn %d: %v", i, err)
		}
	}
	if got := recoveryJSONL(dir); len(got) != 0 {
		t.Fatalf("recovery branches during pure append: %v", got)
	}
}

// Authority + same revision authorizes rewrite when the shared prefix was reshaped (#8294).
func TestSaveSnapshotLeaseHeldSameRevisionAllowsReshapedPrefix(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	s := NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "edit"})
	s.Add(provider.Message{
		Role: provider.RoleAssistant, Content: "editing",
		ToolCalls: []provider.ToolCall{{ID: "call_1", Name: "edit", Arguments: `{"path":"f"}`}},
	})
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("mid-turn: %v", err)
	}

	lease, err := TryAcquireSessionLease(path)
	if err != nil {
		t.Fatalf("TryAcquireSessionLease: %v", err)
	}
	defer lease.Release()
	auth, err := lease.IssueWriteAuthority(NextSessionWriteGeneration())
	if err != nil {
		t.Fatalf("IssueWriteAuthority: %v", err)
	}
	s.BindWriteAuthority(auth)

	// Disk reshape at same revision (digest ownership fails; lease authorizes).
	foreign := NewSession("sys")
	foreign.Add(provider.Message{Role: provider.RoleUser, Content: "edit"})
	foreign.Add(provider.Message{
		Role: provider.RoleAssistant, Content: "editing",
		ToolCalls: []provider.ToolCall{{
			ID: "call_1", Name: "edit", Arguments: `{"path":"f"}`,
			Diff: "@@ -1 +1 @@\n-a\n+b\n", Added: 1, Removed: 1,
		}},
	})
	foreignMsgs := foreign.Snapshot()
	foreignDigest, err := digestSessionMessages(foreignMsgs)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	revision, _, err := sessionContentRevision(path)
	if err != nil {
		t.Fatalf("sessionContentRevision: %v", err)
	}
	if err := appendSessionReplaceEvent(path, foreignMsgs, foreignDigest, revision, "snapshot"); err != nil {
		t.Fatalf("append foreign reshape: %v", err)
	}
	if err := writeSessionMessages(path, foreignMsgs); err != nil {
		t.Fatalf("write foreign reshape: %v", err)
	}
	// Intentionally leave the revision ledger at the original value.

	if !s.UpdateToolCallPreview(provider.ToolCall{ID: "call_1", Diff: "@@ -1 +1 @@\n-a\n+b\n", Added: 1, Removed: 1}) {
		t.Fatal("preview update failed")
	}
	s.Add(provider.Message{Role: provider.RoleTool, ToolCallID: "call_1", Name: "edit", Content: "ok"})
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "done", ReasoningContent: "r"})
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot with lease after reshape: %v", err)
	}
	if got := recoveryJSONL(dir); len(got) != 0 {
		t.Fatalf("unexpected recovery branches: %v", got)
	}

	// Keep the 30s autosave cadence growing the transcript on the same path.
	for i := range 10 {
		s.Add(provider.Message{Role: provider.RoleUser, Content: fmt.Sprintf("n%d", i)})
		s.Add(provider.Message{Role: provider.RoleAssistant, Content: fmt.Sprintf("r%d", i)})
		if err := s.SaveSnapshot(path); err != nil {
			t.Fatalf("continue %d: %v", i, err)
		}
	}
	if got := recoveryJSONL(dir); len(got) != 0 {
		t.Fatalf("recovery branches after continued autosaves: %v", got)
	}
}

// After an intentional recovery retarget, further appends must not cascade.
func TestSaveSnapshotChainAfterRecoveryForkDoesNotCascade(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	s := NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "start"})
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "ok"})
	if err := s.Save(path); err != nil {
		t.Fatalf("base: %v", err)
	}

	s.Add(provider.Message{Role: provider.RoleUser, Content: "unsaved"})
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "local only tail"})
	info, err := s.SaveRecoveryBranch(RecoveryBranchOptions{OriginalPath: path, Reason: "snapshot conflict"})
	if err != nil {
		t.Fatalf("SaveRecoveryBranch: %v", err)
	}
	for i := range 15 {
		s.Add(provider.Message{Role: provider.RoleUser, Content: fmt.Sprintf("u%d", i)})
		s.Add(provider.Message{Role: provider.RoleAssistant, Content: fmt.Sprintf("a%d", i), ReasoningContent: "x"})
		if err := s.SaveSnapshot(info.Path); err != nil {
			var conflict *SessionSnapshotConflictError
			if errors.As(err, &conflict) {
				t.Fatalf("SaveSnapshot on recovery path turn %d diverged: %+v", i, conflict)
			}
			t.Fatalf("SaveSnapshot turn %d: %v", i, err)
		}
	}
	if got := recoveryJSONL(dir); len(got) != 1 {
		t.Fatalf("recovery files = %v (want only the intentional fork)", got)
	}
}

// Without a lease, foreign bytes at the same revision still conflict.
func TestSaveSnapshotRejectsInterruptedForeignWriteWithoutLease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	base := NewSession("sys")
	base.Add(provider.Message{Role: provider.RoleUser, Content: "base"})
	if err := base.Save(path); err != nil {
		t.Fatal(err)
	}
	stale, err := LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	revision, _, err := sessionContentRevision(path)
	if err != nil {
		t.Fatal(err)
	}
	foreignMessages := append(stale.Snapshot(),
		provider.Message{Role: provider.RoleAssistant, Content: "foreign writer tail"})
	foreignDigest, err := digestSessionMessages(foreignMessages)
	if err != nil {
		t.Fatal(err)
	}
	if err := appendSessionReplaceEvent(path, foreignMessages, foreignDigest, revision, "snapshot"); err != nil {
		t.Fatal(err)
	}
	if err := writeSessionMessages(path, foreignMessages); err != nil {
		t.Fatal(err)
	}
	// Crash before recordSessionContentRevision: revision still equals baseline.

	stale.Add(provider.Message{Role: provider.RoleAssistant, Content: "stale writer tail"})
	err = stale.SaveSnapshot(path)
	if !errors.Is(err, ErrSessionSnapshotConflict) {
		t.Fatalf("SaveSnapshot err = %v, want ErrSessionSnapshotConflict", err)
	}
}

// Authority missing after bind: typed error, zero recovery.
func TestSaveSnapshotAuthorityMissingReturnsTypedError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	s := NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "u"})
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "a"})
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatal(err)
	}
	// Simulate a bound-then-cleared controller without rebind.
	s.BindWriteAuthority(&SessionWriteAuthority{}) // forces authRequired
	s.ClearWriteAuthority()
	s.Add(provider.Message{Role: provider.RoleUser, Content: "more"})
	err := s.SaveSnapshot(path)
	if !errors.Is(err, ErrSessionWriteAuthorityMissing) {
		t.Fatalf("err = %v, want ErrSessionWriteAuthorityMissing", err)
	}
	if got := recoveryJSONL(dir); len(got) != 0 {
		t.Fatalf("recovery files = %v, want none", got)
	}
}

// Stale generation after rebind refuses save without forking recovery.
func TestSaveSnapshotStaleAuthorityRefused(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	s := NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "u"})
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "a"})
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatal(err)
	}
	lease, err := TryAcquireSessionLease(path)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	oldAuth, err := lease.IssueWriteAuthority(NextSessionWriteGeneration())
	if err != nil {
		t.Fatal(err)
	}
	s.BindWriteAuthority(oldAuth)
	// New generation supersedes the old token without releasing the lease.
	newAuth, err := lease.IssueWriteAuthority(NextSessionWriteGeneration())
	if err != nil {
		t.Fatal(err)
	}
	if !newAuth.Valid() {
		t.Fatal("replacement authority should be valid")
	}
	s.Add(provider.Message{Role: provider.RoleUser, Content: "more"})
	s.BindWriteAuthority(oldAuth)
	err = s.SaveSnapshot(path)
	if !errors.Is(err, ErrSessionWriteAuthorityStale) {
		t.Fatalf("err = %v, want stale authority", err)
	}
	if got := recoveryJSONL(dir); len(got) != 0 {
		t.Fatalf("recovery files = %v, want none", got)
	}
}

// 0-byte checkpoint + valid WAL: continuous autosave heals and never recovery-forks.
func TestSaveSnapshotZeroByteCheckpointHealsFromWAL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	s := NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "u0"})
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "a0"})
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatal(err)
	}
	leas, err := TryAcquireSessionLease(path)
	if err != nil {
		t.Fatal(err)
	}
	defer leas.Release()
	auth, err := leas.IssueWriteAuthority(NextSessionWriteGeneration())
	if err != nil {
		t.Fatal(err)
	}
	s.BindWriteAuthority(auth)
	// Truncate the checkpoint while leaving the event log intact.
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	for i := range 100 {
		s.Add(provider.Message{Role: provider.RoleUser, Content: fmt.Sprintf("u%d", i+1)})
		s.Add(provider.Message{Role: provider.RoleAssistant, Content: fmt.Sprintf("a%d", i+1)})
		if err := s.SaveSnapshot(path); err != nil {
			t.Fatalf("autosave %d: %v", i, err)
		}
	}
	if got := recoveryJSONL(dir); len(got) != 0 {
		t.Fatalf("recovery files = %v, want none", got)
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() == 0 {
		t.Fatalf("checkpoint size = %v err=%v, want healed non-empty", info, err)
	}
}
