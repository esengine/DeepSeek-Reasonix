package agent

import (
	"crypto/sha256"
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

func TestPersistSessionListingProjectionRejectsAdvancedTranscriptGeneration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "20260101-000000-deepseek-chat.jsonl")
	writeSessionFile(t, path, []provider.Message{
		{Role: provider.RoleUser, Content: "new question"},
		{Role: provider.RoleAssistant, Content: "new answer"},
	})
	newDigest := sha256.Sum256([]byte("new transcript"))
	if err := SaveBranchMeta(path, BranchMeta{
		ID:                   BranchID(path),
		Revision:             2,
		ContentDigest:        digestString(newDigest),
		SchemaVersion:        BranchMetaCountsVersion,
		Preview:              "new question",
		Turns:                1,
		ListingRevision:      2,
		ListingContentDigest: digestString(newDigest),
	}); err != nil {
		t.Fatalf("SaveBranchMeta: %v", err)
	}

	oldDigest := sha256.Sum256([]byte("old transcript"))
	persistSessionListingProjection(path, []provider.Message{
		{Role: provider.RoleUser, Content: "stale question"},
		{Role: provider.RoleAssistant, Content: "stale answer"},
	}, 1, digestString(oldDigest))

	meta, ok, err := LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta: ok=%v err=%v", ok, err)
	}
	if meta.Revision != 2 || meta.ContentDigest != digestString(newDigest) ||
		meta.Preview != "new question" || meta.Turns != 1 ||
		meta.ListingRevision != 2 || meta.ListingContentDigest != digestString(newDigest) {
		t.Fatalf("stale projection crossed transcript generation: %+v", meta)
	}
}

func TestPersistSessionListingProjectionStampsCommittedTranscriptGeneration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "20260101-000000-deepseek-chat.jsonl")
	digest := sha256.Sum256([]byte("committed transcript"))
	if err := SaveBranchMeta(path, BranchMeta{
		ID:            BranchID(path),
		Revision:      3,
		ContentDigest: digestString(digest),
	}); err != nil {
		t.Fatalf("SaveBranchMeta: %v", err)
	}

	persistSessionListingProjection(path, []provider.Message{
		{Role: provider.RoleUser, Content: "committed question"},
		{Role: provider.RoleAssistant, Content: "committed answer"},
	}, 3, digestString(digest))

	meta, ok, err := LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta: ok=%v err=%v", ok, err)
	}
	if meta.Preview != "committed question" || meta.Turns != 1 ||
		meta.SchemaVersion != BranchMetaCountsVersion ||
		meta.ListingRevision != 3 || meta.ListingContentDigest != digestString(digest) {
		t.Fatalf("matching projection was not stamped: %+v", meta)
	}
}

func TestSessionListingInvalidationSurvivesStaleWholeMetadataWriter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	session := NewSession("system")
	session.Add(provider.Message{Role: provider.RoleUser, Content: "old question"})
	if err := session.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot: %v", err)
	}
	stale, ok, err := LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta stale: ok=%v err=%v", ok, err)
	}
	if !sessionListingProjectionFresh(stale.SchemaVersion, stale.Turns, stale.Revision, stale.ListingRevision, stale.ContentDigest, stale.ListingContentDigest) {
		t.Fatalf("initial projection is not fresh: %+v", stale)
	}

	reservedRevision, err := invalidateSessionListingProjection(path)
	if err != nil {
		t.Fatalf("invalidateSessionListingProjection: %v", err)
	}
	if reservedRevision != stale.Revision+1 {
		t.Fatalf("reserved revision = %d, want %d", reservedRevision, stale.Revision+1)
	}
	stale.ParentID = "renamed-parent"
	if err := SaveBranchMetaPreserveUpdated(path, stale); err != nil {
		t.Fatalf("stale whole metadata write: %v", err)
	}

	current, ok, err := LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta current: ok=%v err=%v", ok, err)
	}
	if current.Revision != reservedRevision || current.ContentDigest != stale.ContentDigest {
		t.Fatalf("invalidation generation/digest = %d/%q, want %d/%q", current.Revision, current.ContentDigest, reservedRevision, stale.ContentDigest)
	}
	if current.SchemaVersion != 0 || sessionListingProjectionFresh(current.SchemaVersion, current.Turns, current.Revision, current.ListingRevision, current.ContentDigest, current.ListingContentDigest) {
		t.Fatalf("stale writer restored certified projection: %+v", current)
	}
	if current.ParentID != "renamed-parent" {
		t.Fatalf("unrelated metadata update was lost: ParentID=%q", current.ParentID)
	}
}
