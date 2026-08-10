package agent

import (
	"path/filepath"
	"testing"

	"reasonix/internal/provider"
)

func TestSessionListingBackfillDoesNotOverwriteNewerCounts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "20260101-000000-deepseek-chat.jsonl")
	writeSessionFile(t, path, []provider.Message{
		{Role: provider.RoleSystem, Content: "system"},
		{Role: provider.RoleAssistant, Content: "empty greeting"},
	})
	if err := SaveBranchMeta(path, BranchMeta{
		ID:            BranchID(path),
		SchemaVersion: branchMetaCountsInitialVersion,
	}); err != nil {
		t.Fatalf("SaveBranchMeta: %v", err)
	}

	ordered, err := ListSessionOrder(dir)
	if err != nil || len(ordered) != 1 {
		t.Fatalf("ListSessionOrder: infos=%+v err=%v", ordered, err)
	}
	stale := ordered[0]
	stalePreview, staleTurns, err := previewSessionWithError(path)
	if err != nil || staleTurns != 0 {
		t.Fatalf("stale preview: turns=%d err=%v", staleTurns, err)
	}

	// Force the turn-end side of the interleaving between listing decode and
	// listing backfill. The compare-and-apply must return these newer counts.
	writeSessionFile(t, path, []provider.Message{
		{Role: provider.RoleUser, Content: "new question"},
		{Role: provider.RoleAssistant, Content: "new answer"},
	})
	if err := UpdateSessionMeta(path, "", "new question", 1, false); err != nil {
		t.Fatalf("UpdateSessionMeta: %v", err)
	}

	preview, turns, err := updateSessionListingCountsIfCurrent(stale, stalePreview, staleTurns)
	if err != nil {
		t.Fatalf("updateSessionListingCountsIfCurrent: %v", err)
	}
	if preview != "new question" || turns != 1 {
		t.Fatalf("stale backfill replaced newer counts: preview=%q turns=%d", preview, turns)
	}
	infos, err := ListSessions(dir)
	if err != nil || len(infos) != 1 || infos[0].Turns != 1 {
		t.Fatalf("newly saved session was hidden: infos=%+v err=%v", infos, err)
	}
}

func TestSessionListingBackfillRejectsChangedTranscriptGeneration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "20260101-000000-deepseek-chat.jsonl")
	writeSessionFile(t, path, []provider.Message{{Role: provider.RoleSystem, Content: "system"}})
	if err := SaveBranchMeta(path, BranchMeta{
		ID:            BranchID(path),
		SchemaVersion: branchMetaCountsInitialVersion,
		Revision:      1,
		ContentDigest: "old",
	}); err != nil {
		t.Fatalf("SaveBranchMeta: %v", err)
	}
	ordered, err := ListSessionOrder(dir)
	if err != nil || len(ordered) != 1 {
		t.Fatalf("ListSessionOrder: infos=%+v err=%v", ordered, err)
	}

	if err := UpdateBranchMeta(path, false, func(meta *BranchMeta) error {
		meta.Revision = 2
		meta.ContentDigest = "new"
		return nil
	}); err != nil {
		t.Fatalf("advance transcript generation: %v", err)
	}
	_, _, err = updateSessionListingCountsIfCurrent(ordered[0], "", 0)
	if err != nil {
		t.Fatalf("updateSessionListingCountsIfCurrent: %v", err)
	}
	meta, ok, err := LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta: ok=%v err=%v", ok, err)
	}
	if meta.SchemaVersion != branchMetaCountsInitialVersion || meta.Revision != 2 || meta.ContentDigest != "new" {
		t.Fatalf("stale backfill crossed transcript generation: %+v", meta)
	}
}
