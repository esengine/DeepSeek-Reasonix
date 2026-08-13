package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

func TestListSessionsLeavesLegacyCountsUnknownWithoutDecodingOrWriting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "legacy.jsonl")
	if err := os.WriteFile(path, []byte("not valid jsonl\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	updated := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	if err := SaveBranchMetaPreserveUpdated(path, BranchMeta{
		ID: BranchID(path), CreatedAt: updated, UpdatedAt: updated, Scope: "global",
	}); err != nil {
		t.Fatal(err)
	}

	infos, err := ListSessions(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 1 || infos[0].CountsKnown || infos[0].Turns != 0 || !strings.Contains(infos[0].Preview, "being indexed") {
		t.Fatalf("legacy listing = %#v", infos)
	}
	meta, ok, err := LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta: ok=%v err=%v", ok, err)
	}
	if meta.SchemaVersion != 0 || meta.Turns != 0 || !meta.UpdatedAt.Equal(updated) {
		t.Fatalf("listing mutated legacy sidecar: %+v", meta)
	}
}

// A legacy session whose sidecar has no recorded turn count remains visible
// without a synchronous transcript decode. The catalog repair worker owns the
// eventual backfill.
func TestListSessionsLeavesLegacySessionForBackgroundRepair(t *testing.T) {
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
	if len(infos) != 1 || infos[0].Turns != 0 || infos[0].CountsKnown {
		t.Fatalf("expected one unknown-count session, got %+v", infos)
	}

	// Listing must not mutate the sidecar or bump activity time.
	meta, ok, err := LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta: ok=%v err=%v", ok, err)
	}
	if meta.Turns != 0 || meta.Preview != "" || meta.SchemaVersion != 0 {
		t.Fatalf("listing mutated counts: %+v", meta)
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

// A legacy artifact that might be empty is shown as unknown until the catalog
// repair worker validates it; listing cannot know without decoding.
func TestListSessionsShowsPotentiallyEmptyLegacySessionAsUnknown(t *testing.T) {
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
	if len(infos) != 1 || infos[0].CountsKnown {
		t.Fatalf("legacy session should be visible as unknown; got %+v", infos)
	}

	if _, ok, err := LoadBranchMeta(path); err != nil || ok {
		t.Fatalf("listing unexpectedly created metadata: ok=%v err=%v", ok, err)
	}
}

func TestListSessionsDefersPreviouslyCachedZeroRepair(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "20260101-000000-deepseek-chat.jsonl")
	writeSessionFile(t, path, []provider.Message{
		{Role: provider.RoleUser, Content: "recovered question"},
		{Role: provider.RoleAssistant, Content: "recovered answer"},
	})
	if err := SaveBranchMeta(path, BranchMeta{
		ID:            BranchID(path),
		Turns:         0,
		SchemaVersion: branchMetaCountsInitialVersion,
	}); err != nil {
		t.Fatalf("SaveBranchMeta: %v", err)
	}

	infos, err := ListSessions(dir)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(infos) != 1 || infos[0].Turns != 0 || infos[0].CountsKnown || !strings.Contains(infos[0].Preview, "being indexed") {
		t.Fatalf("legacy zero was not exposed as unknown: %+v", infos)
	}
	meta, ok, err := LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta: ok=%v err=%v", ok, err)
	}
	if meta.SchemaVersion != branchMetaCountsInitialVersion || meta.Turns != 0 || meta.Preview != "" {
		t.Fatalf("listing unexpectedly repaired metadata: %+v", meta)
	}
}

func TestListSessionsDefersOldRecordedEmptyValidation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "20260101-000000-deepseek-chat.jsonl")
	writeSessionFile(t, path, []provider.Message{
		{Role: provider.RoleSystem, Content: "system prompt"},
		{Role: provider.RoleAssistant, Content: "a greeting with no user turn"},
	})
	if err := SaveBranchMeta(path, BranchMeta{
		ID:            BranchID(path),
		Turns:         0,
		SchemaVersion: branchMetaCountsInitialVersion,
	}); err != nil {
		t.Fatalf("SaveBranchMeta: %v", err)
	}

	infos, err := ListSessions(dir)
	if err != nil {
		t.Fatalf("ListSessions (migration): %v", err)
	}
	if len(infos) != 1 || infos[0].CountsKnown {
		t.Fatalf("old zero should remain visible as unknown; got %+v", infos)
	}
	meta, ok, err := LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta: ok=%v err=%v", ok, err)
	}
	if meta.SchemaVersion != branchMetaCountsInitialVersion || meta.Turns != 0 {
		t.Fatalf("listing changed old zero metadata: %+v", meta)
	}

	// Current-version zero counts are authoritative. Making the artifact invalid
	// after migration must not cause the listing path to decode it again.
	if err := os.WriteFile(path, []byte("not valid json\n"), 0o600); err != nil {
		t.Fatalf("replace session with corrupt content: %v", err)
	}
	infos, err = ListSessions(dir)
	if err != nil {
		t.Fatalf("ListSessions (steady state): %v", err)
	}
	if len(infos) != 1 || infos[0].CountsKnown {
		t.Fatalf("unknown session should stay visible without re-probe: %+v", infos)
	}
}

func TestListSessionsKeepsUnreadableNonEmptySessionsVisible(t *testing.T) {
	for _, tc := range []struct {
		name       string
		cachedZero bool
	}{
		{name: "legacy"},
		{name: "previously cached as empty", cachedZero: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "20260101-000000-deepseek-chat.jsonl")
			if err := os.WriteFile(path, []byte("not valid json\n"), 0o600); err != nil {
				t.Fatalf("write corrupt session: %v", err)
			}
			if tc.cachedZero {
				if err := SaveBranchMeta(path, BranchMeta{
					ID:            BranchID(path),
					Turns:         0,
					SchemaVersion: branchMetaCountsInitialVersion,
				}); err != nil {
					t.Fatalf("SaveBranchMeta: %v", err)
				}
			}

			infos, err := ListSessions(dir)
			if err != nil {
				t.Fatalf("ListSessions: %v", err)
			}
			if len(infos) != 1 {
				t.Fatalf("unreadable non-empty session must remain visible; got %d entries", len(infos))
			}
			if infos[0].Turns != 0 || infos[0].CountsKnown || !strings.Contains(infos[0].Preview, "being indexed") {
				t.Fatalf("unexpected corrupt-session listing: %+v", infos[0])
			}

			meta, ok, err := LoadBranchMeta(path)
			if err != nil {
				t.Fatalf("LoadBranchMeta: %v", err)
			}
			if ok && meta.SchemaVersion >= BranchMetaCountsVersion {
				t.Fatalf("unreadable session was incorrectly stamped authoritative: %+v", meta)
			}
		})
	}
}
