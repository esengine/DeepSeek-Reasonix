package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/provider"
	"reasonix/internal/store"
)

func saveSnapshotTurns(t *testing.T, path string, turns int) *agent.Session {
	t.Helper()
	s := agent.NewSession("sys")
	for i := 0; i < turns; i++ {
		s.Add(provider.Message{Role: provider.RoleUser, Content: "prompt " + string(rune('a'+i))})
		s.Add(provider.Message{Role: provider.RoleAssistant, Content: "reply"})
		if err := s.SaveSnapshot(path); err != nil {
			t.Fatalf("SaveSnapshot turn %d: %v", i, err)
		}
	}
	return s
}

func forkDesktopRecoveryBranch(t *testing.T, dir, name string) (parentPath, branchPath string, branchMsgs []provider.Message) {
	t.Helper()
	parentPath = filepath.Join(dir, name+".jsonl")
	parent := agent.NewSession("sys")
	parent.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	parent.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	parent.Add(provider.Message{Role: provider.RoleUser, Content: "disk " + name})
	if err := parent.Save(parentPath); err != nil {
		t.Fatalf("Save recovery parent: %v", err)
	}
	branch := agent.NewSession("sys")
	branch.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	branch.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	branch.Add(provider.Message{Role: provider.RoleUser, Content: "local " + name})
	info, err := branch.SaveRecoveryBranch(agent.RecoveryBranchOptions{OriginalPath: parentPath})
	if err != nil {
		t.Fatalf("SaveRecoveryBranch: %v", err)
	}
	return parentPath, info.Path, branch.Snapshot()
}

func coverDesktopRecoveryParent(t *testing.T, parentPath string, branchMsgs []provider.Message) {
	t.Helper()
	parent := agent.NewSession("")
	parent.Messages = append([]provider.Message(nil), branchMsgs...)
	parent.Add(provider.Message{Role: provider.RoleAssistant, Content: "parent kept the recovery content"})
	if err := parent.Save(parentPath); err != nil {
		t.Fatalf("Save covering recovery parent: %v", err)
	}
}

func TestMergeSessionInfosCountsRecoveryActivity(t *testing.T) {
	dir := t.TempDir()
	parentPath, branchPath, branchMsgs := forkDesktopRecoveryBranch(t, dir, "covered")
	coverDesktopRecoveryParent(t, parentPath, branchMsgs)
	summaries := map[string]topicSummary{}
	now := time.Now()
	infos := []agent.SessionInfo{
		{
			Path:           parentPath,
			Turns:          3,
			LastActivityAt: now.Add(-time.Hour),
			Scope:          "global",
			TopicID:        "topic-1",
		},
		{
			Path:           branchPath,
			Turns:          5,
			LastActivityAt: now,
			Scope:          "global",
			TopicID:        "topic-1",
			Recovered:      true,
		},
	}
	mergeSessionInfos(dir, infos, map[string]string{}, map[string]agent.SessionInfo{}, map[string]string{}, summaries)
	summary := summaries[topicSummaryKey("global", "", "topic-1")]
	if !summary.hasNormalSession || !summary.hasRecoveryOnly {
		t.Fatalf("summary flags = %+v, want both normal and recovery seen", summary)
	}
	if summary.turns != 3 {
		t.Fatalf("turns = %d, want 3 (recovery copies must not double-count)", summary.turns)
	}
	// The copy is the live transcript after recovery: its newer activity must
	// drive topic recency, unread state, and time filters.
	if summary.lastActivityAt != now.UnixMilli() {
		t.Fatalf("lastActivityAt = %d, want recovery activity %d", summary.lastActivityAt, now.UnixMilli())
	}
}

func TestMergeSessionInfosKeepsContinuedRecoveryVisible(t *testing.T) {
	dir := t.TempDir()
	_, branchPath, _ := forkDesktopRecoveryBranch(t, dir, "diverged")
	summaries := map[string]topicSummary{}
	now := time.Now()
	infos := []agent.SessionInfo{{
		Path:           branchPath,
		Turns:          5,
		LastActivityAt: now,
		Scope:          "global",
		TopicID:        "topic-continued",
		Recovered:      true,
	}}

	mergeSessionInfos(dir, infos, map[string]string{}, map[string]agent.SessionInfo{}, map[string]string{}, summaries)
	summary := summaries[topicSummaryKey("global", "", "topic-continued")]
	if !summary.hasAdoptedRecovery || summary.hasRecoveryOnly {
		t.Fatalf("summary flags = %+v, want adopted recovery only", summary)
	}
	if topicHiddenAsRecoveryOnly(summary, false, nil) {
		t.Fatal("continued recovery was hidden after its tab closed")
	}
	if got := summary.displayTurns(); got != 5 {
		t.Fatalf("display turns = %d, want 5", got)
	}
}

func TestSessionMetaSeparatesRecoveryProvenanceFromCleanupCopy(t *testing.T) {
	dir := t.TempDir()
	coveredParent, coveredBranch, coveredMsgs := forkDesktopRecoveryBranch(t, dir, "meta-covered")
	coverDesktopRecoveryParent(t, coveredParent, coveredMsgs)
	info := agent.SessionInfo{
		Path:      coveredBranch,
		Recovered: true,
	}
	meta := sessionMetaFromInfo(info, "", false, false, 0, dir)
	if !meta.Recovered || !meta.RecoveryCopy {
		t.Fatalf("covered recovery meta = %+v, want provenance and cleanup-copy flags", meta)
	}

	_, divergedBranch, _ := forkDesktopRecoveryBranch(t, dir, "meta-diverged")
	info.Path = divergedBranch
	meta = sessionMetaFromInfo(info, "", false, false, 0, dir)
	if !meta.Recovered || meta.RecoveryCopy {
		t.Fatalf("diverged recovery meta = %+v, want provenance without cleanup-copy flag", meta)
	}
}

func TestRecoveryCopyCleanupRevalidatesInBackend(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	app := NewApp()

	parentPath, branchPath, branchMsgs := forkDesktopRecoveryBranch(t, dir, "delete-guard")
	if err := app.DeleteRecoveryCopy(branchPath); err == nil {
		t.Fatal("DeleteRecoveryCopy accepted a branch with unique content")
	}
	if _, err := os.Stat(branchPath); err != nil {
		t.Fatalf("rejected recovery branch was not preserved: %v", err)
	}
	coverDesktopRecoveryParent(t, parentPath, branchMsgs)
	if err := app.DeleteRecoveryCopy(branchPath); err != nil {
		t.Fatalf("DeleteRecoveryCopy covered branch: %v", err)
	}
	trashPath := filepath.Join(dir, sessionTrashDir, filepath.Base(branchPath), filepath.Base(branchPath))
	if _, err := os.Stat(trashPath); err != nil {
		t.Fatalf("covered recovery branch was not moved to trash: %v", err)
	}

	purgeParent, purgeBranch, purgeMsgs := forkDesktopRecoveryBranch(t, dir, "purge-guard")
	if err := app.DeleteSession(purgeBranch); err != nil {
		t.Fatalf("DeleteSession divergent branch: %v", err)
	}
	purgeTrashPath := filepath.Join(dir, sessionTrashDir, filepath.Base(purgeBranch), filepath.Base(purgeBranch))
	if err := app.PurgeRecoveryCopy(purgeTrashPath); err == nil {
		t.Fatal("PurgeRecoveryCopy accepted a trashed branch with unique content")
	}
	if _, err := os.Stat(purgeTrashPath); err != nil {
		t.Fatalf("rejected trashed recovery branch was not preserved: %v", err)
	}
	coverDesktopRecoveryParent(t, purgeParent, purgeMsgs)
	parentLease, err := agent.TryAcquireSessionLease(purgeParent)
	if err != nil {
		t.Fatalf("TryAcquireSessionLease parent: %v", err)
	}
	if err := app.PurgeRecoveryCopy(purgeTrashPath); !errors.Is(err, errSessionBusyElsewhere) {
		parentLease.Release()
		t.Fatalf("PurgeRecoveryCopy while parent is live err = %v, want errSessionBusyElsewhere", err)
	}
	if _, err := os.Stat(purgeTrashPath); err != nil {
		parentLease.Release()
		t.Fatalf("busy-parent purge did not preserve recovery branch: %v", err)
	}
	parentLease.Release()
	if err := app.PurgeRecoveryCopy(purgeTrashPath); err != nil {
		t.Fatalf("PurgeRecoveryCopy covered branch: %v", err)
	}
	if _, err := os.Stat(purgeTrashPath); !os.IsNotExist(err) {
		t.Fatalf("covered recovery branch survived permanent purge: %v", err)
	}
}

func TestTopicHiddenAsRecoveryOnly(t *testing.T) {
	recoveryOnly := topicSummary{hasRecoveryOnly: true}
	cases := []struct {
		name     string
		summary  topicSummary
		pinned   bool
		sessions []runtimeSessionStatus
		want     bool
	}{
		{"recovery-only idle", recoveryOnly, false, nil, true},
		{"normal session present", topicSummary{hasRecoveryOnly: true, hasNormalSession: true}, false, nil, false},
		{"continued recovery present", topicSummary{hasRecoveryOnly: true, hasAdoptedRecovery: true}, false, nil, false},
		{"pinned stays visible", recoveryOnly, true, nil, false},
		{"single open runtime", recoveryOnly, false, []runtimeSessionStatus{{open: true}}, false},
		// topicRuntimeStatus reports open/running only for single-session
		// topics; the hide rule must still see a two-session topic as live.
		{"two runtime sessions one open", recoveryOnly, false, []runtimeSessionStatus{{open: true}, {running: false}}, false},
		{"detached running runtime", recoveryOnly, false, []runtimeSessionStatus{{running: true}, {}}, false},
		{"idle runtime entries only", recoveryOnly, false, []runtimeSessionStatus{{}, {}}, true},
	}
	for _, c := range cases {
		if got := topicHiddenAsRecoveryOnly(c.summary, c.pinned, c.sessions); got != c.want {
			t.Errorf("%s: hidden = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestTrashSessionMatchesLiveSeesEventLogDivergence(t *testing.T) {
	dir := t.TempDir()
	live := filepath.Join(dir, "session.jsonl")
	s := saveSnapshotTurns(t, live, 1)

	// Simulate an old trash copy taken at checkpoint time: same anchor bytes,
	// same event log state.
	trashDir := filepath.Join(dir, "trash")
	if err := os.MkdirAll(trashDir, 0o755); err != nil {
		t.Fatal(err)
	}
	trashPath := filepath.Join(trashDir, "session.jsonl")
	for _, pair := range [][2]string{
		{live, trashPath},
		{store.SessionEventLog(live), store.SessionEventLog(trashPath)},
	} {
		b, err := os.ReadFile(pair[0])
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(pair[1], b, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	same, err := trashSessionMatchesLive(live, trashPath)
	if err != nil {
		t.Fatalf("trashSessionMatchesLive identical: %v", err)
	}
	if !same {
		t.Fatal("identical live/trash reported as different")
	}

	// The live session keeps chatting: growth lands in the event log only, so
	// the two .jsonl checkpoints stay byte-identical. Byte comparison would
	// call this a duplicate and delete the live session's newer history.
	s.Add(provider.Message{Role: provider.RoleUser, Content: "newer work"})
	if err := s.SaveSnapshot(live); err != nil {
		t.Fatalf("SaveSnapshot diverge: %v", err)
	}
	liveAnchor, _ := os.ReadFile(live)
	trashAnchor, _ := os.ReadFile(trashPath)
	if string(liveAnchor) != string(trashAnchor) {
		t.Skip("checkpoints diverged on disk; byte-compare trap not reproducible here")
	}
	same, err = trashSessionMatchesLive(live, trashPath)
	if err != nil {
		t.Fatalf("trashSessionMatchesLive diverged: %v", err)
	}
	if same {
		t.Fatal("live session with newer event log reported as duplicate of trash copy")
	}
}

func TestTrashPathsBlockedWhileLeaseHeld(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	saveSnapshotTurns(t, path, 1)

	// A live owner (any runtime — this process or another) holds the lease
	// lock on an open handle for its whole hold. Every destructive path must
	// refuse while it is held: probing once and deleting later would let the
	// owner's freshly locked lease file be unlinked out from under it.
	lease, err := agent.TryAcquireSessionLease(path)
	if err != nil {
		t.Fatalf("TryAcquireSessionLease: %v", err)
	}
	released := false
	defer func() {
		if !released {
			lease.Release()
		}
	}()

	if err := trashSessionArtifactsBeforeMove(dir, path, "session.jsonl", nil); !errors.Is(err, errSessionBusyElsewhere) {
		t.Fatalf("trashSessionArtifactsBeforeMove err = %v, want errSessionBusyElsewhere", err)
	}
	if err := reconcileDesktopTrashSessionArtifacts(dir, path, "session.jsonl"); !errors.Is(err, errSessionBusyElsewhere) {
		t.Fatalf("reconcileDesktopTrashSessionArtifacts err = %v, want errSessionBusyElsewhere", err)
	}
	if err := removeDesktopSessionArtifacts(path); !errors.Is(err, errSessionBusyElsewhere) {
		t.Fatalf("removeDesktopSessionArtifacts err = %v, want errSessionBusyElsewhere", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("session file touched despite live owner: %v", err)
	}
	if _, err := os.Stat(store.SessionEventLog(path)); err != nil {
		t.Fatalf("event log touched despite live owner: %v", err)
	}
	if _, err := os.Stat(store.SessionLeaseLock(path)); err != nil {
		t.Fatalf("lease lock deleted while held: %v", err)
	}

	// Once the owner releases, the same trash call succeeds and the lock
	// sidecars are gone with it.
	lease.Release()
	released = true
	if err := trashSessionArtifactsBeforeMove(dir, path, "session.jsonl", nil); err != nil {
		t.Fatalf("trashSessionArtifactsBeforeMove after release: %v", err)
	}
	for _, p := range []string{
		path,
		store.SessionLockFile(path),
		store.SessionLeaseLock(path),
		store.SessionLeaseInfo(path),
	} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("artifact survived trash: %s (err=%v)", p, err)
		}
	}
}

func TestPromptHistorySeesEventLogPrompts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	s := agent.NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "first prompt"})
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot: %v", err)
	}
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "reply"})
	s.Add(provider.Message{Role: provider.RoleUser, Content: "second prompt"})
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot append: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := collectPromptHistoryEntries(path, info, func(s string) string { return s })
	if err != nil {
		t.Fatalf("collectPromptHistoryEntries: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("prompt history entries = %d, want 2 (event-log prompts must appear)", len(entries))
	}
	if entries[0].Text != "first prompt" || entries[1].Text != "second prompt" {
		t.Fatalf("prompt history texts = %q, %q", entries[0].Text, entries[1].Text)
	}
	if entries[1].At == 0 {
		t.Fatal("appended prompt lost its timestamp")
	}
}

func TestTopicTitleUserTurnsSeesEventLogTurns(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	saveSnapshotTurns(t, path, 3)

	users := topicTitleUserTurnsFromSession(path)
	if len(users) != 3 {
		t.Fatalf("user turns = %d, want 3 (≥3-turn title upgrade depends on this)", len(users))
	}
}

func TestTopicTitleUserTurnsSkipHostFraming(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	s := agent.NewSession("sys")
	// Delivery-mode first turn: user text with the trailing runtime marker.
	// Built from the exported constant — the preview strip is byte-exact, so a
	// paraphrased marker would (correctly) not be stripped.
	s.Add(provider.Message{Role: provider.RoleUser, Content: "你是谁？\n\n" + agent.DeliveryRuntimeMarker})
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "reply"})
	// Host-injected readiness nudge, persisted as role user.
	s.Add(provider.Message{Role: provider.RoleUser, Content: "Host final-answer readiness check failed. Before giving a final answer, address the missing host-observable receipts: x"})
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "reply"})
	s.Add(provider.Message{Role: provider.RoleUser, Content: "帮我写一个魂斗罗游戏"})
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot: %v", err)
	}

	users := topicTitleUserTurnsFromSession(path)
	if len(users) != 2 {
		t.Fatalf("user turns = %d, want 2 (readiness nudge must not count)", len(users))
	}
	if users[0] != "你是谁？" {
		t.Fatalf("first turn = %q, want the marker stripped", users[0])
	}
	if title := topicTitleFromText(users[0]); strings.Contains(title, "<delivery") || strings.Contains(title, "delivery-run") {
		t.Fatalf("title = %q, delivery marker leaked", title)
	}
}
