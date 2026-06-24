package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/provider"
)

func writeSessionFile(t *testing.T, path string, msgs []provider.Message) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, m := range msgs {
		if err := enc.Encode(m); err != nil {
			t.Fatalf("encode: %v", err)
		}
	}
}

// SessionPreviewFromMessages must match a from-disk decode byte-for-byte, since
// Session.Save persists exactly the messages it is handed.
func TestSessionPreviewFromMessagesMatchesDecode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "20260101-000000-deepseek-chat.jsonl")
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "you are helpful"},
		{Role: provider.RoleUser, Content: "first question about the bug"},
		{Role: provider.RoleAssistant, Content: "here is an answer", ReasoningContent: "thinking"},
		{Role: provider.RoleUser, Content: "follow up"},
		{Role: provider.RoleAssistant, Content: "more"},
	}
	writeSessionFile(t, path, msgs)

	filePreview, fileTurns := previewSession(path)
	memPreview, memTurns := SessionPreviewFromMessages(msgs)
	if fileTurns != memTurns || filePreview != memPreview {
		t.Fatalf("mismatch: file=(%q,%d) mem=(%q,%d)", filePreview, fileTurns, memPreview, memTurns)
	}
	if memTurns != 2 {
		t.Fatalf("expected 2 user turns, got %d", memTurns)
	}
}

// When the sidecar records Turns/Preview, ListSessions must trust them and not
// re-derive from the .jsonl. We prove that by planting counts that disagree with
// the file: if ListSessions returns the planted values, it used the sidecar.
func TestListSessionsUsesSidecarWithoutDecoding(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "20260101-000000-deepseek-chat.jsonl")
	writeSessionFile(t, path, []provider.Message{
		{Role: provider.RoleUser, Content: "real content in the file"},
		{Role: provider.RoleAssistant, Content: "a"},
	})
	// Sidecar deliberately disagrees with the file (3 turns, custom preview).
	if err := UpdateSessionMeta(path, "", "cached preview line", 3, true); err != nil {
		t.Fatalf("UpdateSessionMeta: %v", err)
	}

	infos, err := ListSessions(dir)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(infos) != 1 {
		t.Fatalf("expected 1 session, got %d", len(infos))
	}
	if infos[0].Turns != 3 || infos[0].Preview != "cached preview line" {
		t.Fatalf("expected sidecar values (3, %q), got (%d, %q)", "cached preview line", infos[0].Turns, infos[0].Preview)
	}
}

// A legacy session whose sidecar has no recorded turn count must be decoded once
// and then backfilled, so the second listing reads the sidecar.
func TestListSessionsBackfillsLegacySession(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "20260101-000000-deepseek-chat.jsonl")
	writeSessionFile(t, path, []provider.Message{
		{Role: provider.RoleUser, Content: "legacy question"},
		{Role: provider.RoleAssistant, Content: "answer"},
		{Role: provider.RoleUser, Content: "again"},
		{Role: provider.RoleAssistant, Content: "ok"},
	})
	// Sidecar exists but predates the counts (no Turns/Preview), with a fixed
	// UpdatedAt we expect backfill to preserve.
	updated := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := SaveBranchMetaPreserveUpdated(path, BranchMeta{ID: BranchID(path), CreatedAt: updated, UpdatedAt: updated, Scope: "global"}); err != nil {
		t.Fatalf("SaveBranchMetaPreserveUpdated: %v", err)
	}

	infos, err := ListSessions(dir)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(infos) != 1 || infos[0].Turns != 2 {
		t.Fatalf("expected 1 session with 2 turns, got %+v", infos)
	}

	// Backfill must have written the counts into the sidecar without bumping
	// activity time (ordering must stay stable).
	meta, ok, err := LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta: ok=%v err=%v", ok, err)
	}
	if meta.Turns != 2 || meta.Preview != "legacy question" {
		t.Fatalf("backfill missing: turns=%d preview=%q", meta.Turns, meta.Preview)
	}
	if !meta.UpdatedAt.Equal(updated) {
		t.Fatalf("backfill bumped UpdatedAt: got %v want %v", meta.UpdatedAt, updated)
	}
}

// A counts-authoritative sidecar (SchemaVersion stamped) that records Turns == 0
// must be trusted as empty and skipped WITHOUT decoding the .jsonl. We prove the
// version gate by planting a file that actually has content but a meta claiming
// it is empty: if the session is decoded it would be listed; trusting the meta
// skips it.
func TestListSessionsTrustsRecordedEmptyWithoutDecoding(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "20260101-000000-deepseek-chat.jsonl")
	writeSessionFile(t, path, []provider.Message{
		{Role: provider.RoleUser, Content: "real content the meta will lie about"},
		{Role: provider.RoleAssistant, Content: "a"},
	})
	if err := SaveBranchMeta(path, BranchMeta{ID: BranchID(path), Turns: 0, SchemaVersion: BranchMetaCountsVersion}); err != nil {
		t.Fatalf("SaveBranchMeta: %v", err)
	}

	infos, err := ListSessions(dir)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(infos) != 0 {
		t.Fatalf("a counts-authoritative empty session should be skipped without decoding; got %d", len(infos))
	}
}

// A genuinely-empty legacy session (no counts-aware meta) must be decoded once
// and then recorded as authoritative-empty, so later listings trust it instead
// of re-decoding the .jsonl on every refresh (the Turns == 0 overload bug).
func TestListSessionsRecordsEmptyLegacySessionOnce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "20260101-000000-deepseek-chat.jsonl")
	writeSessionFile(t, path, []provider.Message{
		{Role: provider.RoleSystem, Content: "system prompt"},
		{Role: provider.RoleAssistant, Content: "a greeting with no user turn"},
	})

	infos, err := ListSessions(dir)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(infos) != 0 {
		t.Fatalf("empty session must not be listed; got %d", len(infos))
	}

	meta, ok, err := LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("empty session should have been stamped: ok=%v err=%v", ok, err)
	}
	if meta.SchemaVersion != BranchMetaCountsVersion || meta.Turns != 0 {
		t.Fatalf("empty session not recorded as authoritative-empty: version=%d turns=%d", meta.SchemaVersion, meta.Turns)
	}
}
