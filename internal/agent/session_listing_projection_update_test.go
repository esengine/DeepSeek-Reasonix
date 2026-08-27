package agent

import (
	"path/filepath"
	"testing"

	"reasonix/internal/provider"
)

func TestUpdateSessionListingProjectionIfCurrentRejectsAdvancedGeneration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "20260101-000000-deepseek-chat.jsonl")
	writeSessionFile(t, path, []provider.Message{{Role: provider.RoleUser, Content: "old question"}})
	_, oldState, _, err := LoadSessionDisplayMessages(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveBranchMeta(path, BranchMeta{ID: BranchID(path), Revision: 1, ContentDigest: oldState.DigestHex}); err != nil {
		t.Fatal(err)
	}
	_, oldState, _, err = LoadSessionDisplayMessages(path)
	if err != nil || !oldState.RevisionKnown {
		t.Fatalf("load old state = %+v err=%v", oldState, err)
	}

	writeSessionFile(t, path, []provider.Message{{Role: provider.RoleUser, Content: "new question"}})
	_, newState, _, err := LoadSessionDisplayMessages(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := UpdateBranchMeta(path, false, func(meta *BranchMeta) error {
		meta.Revision = 2
		meta.ContentDigest = newState.DigestHex
		meta.Preview = "new question"
		meta.Turns = 1
		meta.SchemaVersion = BranchMetaCountsVersion
		stampSessionListingProjection(meta)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	applied, err := UpdateSessionListingProjectionIfCurrent(path, "", "old question", 1, false, oldState)
	if err != nil || applied {
		t.Fatalf("stale projection applied=%v err=%v", applied, err)
	}
	meta, ok, err := LoadBranchMeta(path)
	if err != nil || !ok || meta.Revision != 2 || meta.Preview != "new question" || meta.ContentDigest != newState.DigestHex {
		t.Fatalf("advanced metadata changed: ok=%v err=%v meta=%+v", ok, err, meta)
	}
}

func TestUpdateSessionListingProjectionIfCurrentRepairsMatchingLegacySession(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "20260101-000000-deepseek-chat.jsonl")
	writeSessionFile(t, path, []provider.Message{{Role: provider.RoleUser, Content: "legacy question"}})
	if err := SaveBranchMeta(path, BranchMeta{ID: BranchID(path)}); err != nil {
		t.Fatal(err)
	}
	_, state, _, err := LoadSessionDisplayMessages(path)
	if err != nil || state.RevisionKnown {
		t.Fatalf("load legacy state = %+v err=%v", state, err)
	}
	applied, err := UpdateSessionListingProjectionIfCurrent(path, "", "legacy question", 1, false, state)
	if err != nil || !applied {
		t.Fatalf("legacy projection applied=%v err=%v", applied, err)
	}
	meta, ok, err := LoadBranchMeta(path)
	if err != nil || !ok || meta.Preview != "legacy question" || meta.Turns != 1 || meta.SchemaVersion != BranchMetaCountsVersion {
		t.Fatalf("legacy projection missing: ok=%v err=%v meta=%+v", ok, err, meta)
	}
}

func TestUpdateSessionListingProjectionIfCurrentStampsMatchingGeneration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "20260101-000000-deepseek-chat.jsonl")
	writeSessionFile(t, path, []provider.Message{{Role: provider.RoleUser, Content: "current question"}})
	_, state, _, err := LoadSessionDisplayMessages(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveBranchMeta(path, BranchMeta{ID: BranchID(path), Revision: 3, ContentDigest: state.DigestHex}); err != nil {
		t.Fatal(err)
	}
	_, state, _, err = LoadSessionDisplayMessages(path)
	if err != nil || !state.RevisionKnown {
		t.Fatalf("load current state = %+v err=%v", state, err)
	}
	applied, err := UpdateSessionListingProjectionIfCurrent(path, "", "current question", 1, false, state)
	if err != nil || !applied {
		t.Fatalf("matching projection applied=%v err=%v", applied, err)
	}
	meta, ok, err := LoadBranchMeta(path)
	if err != nil || !ok || meta.ListingRevision != 3 || meta.ListingContentDigest != state.DigestHex || meta.Preview != "current question" {
		t.Fatalf("matching projection not stamped: ok=%v err=%v meta=%+v", ok, err, meta)
	}
}
