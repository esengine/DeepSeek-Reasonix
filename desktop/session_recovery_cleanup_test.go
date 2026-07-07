package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/agent"
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

func TestMergeSessionInfosCountsRecoveryActivity(t *testing.T) {
	summaries := map[string]topicSummary{}
	now := time.Now()
	infos := []agent.SessionInfo{
		{
			Path:           "/tmp/original.jsonl",
			Turns:          3,
			LastActivityAt: now.Add(-time.Hour),
			Scope:          "global",
			TopicID:        "topic-1",
		},
		{
			Path:           "/tmp/original-recovery-abc.jsonl",
			Turns:          5,
			LastActivityAt: now,
			Scope:          "global",
			TopicID:        "topic-1",
			Recovered:      true,
		},
	}
	mergeSessionInfos("/tmp", infos, map[string]string{}, map[string]agent.SessionInfo{}, map[string]string{}, summaries)
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
