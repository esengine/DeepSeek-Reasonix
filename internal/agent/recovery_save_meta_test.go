package agent

import (
	"path/filepath"
	"testing"

	"reasonix/internal/provider"
	"reasonix/internal/store"
)

func TestRecoveryLaneRewriteAdvancesItsOwnLedgerAndDerivedIndexes(t *testing.T) {
	dir := t.TempDir()
	originalPath := filepath.Join(dir, "original.jsonl")
	original := NewSession("sys")
	original.Add(provider.Message{Role: provider.RoleUser, Content: "disk question"})
	if err := original.SaveSnapshot(originalPath); err != nil {
		t.Fatal(err)
	}

	recovery := NewSession("sys")
	recovery.Add(provider.Message{Role: provider.RoleUser, Content: "local question"})
	first, err := recovery.SaveShutdownRecoveryBranch(RecoveryBranchOptions{OriginalPath: originalPath})
	if err != nil {
		t.Fatal(err)
	}
	if err := UpdateBranchMeta(first.Path, false, func(meta *BranchMeta) error {
		meta.Revision = 7
		stampSessionListingProjection(meta)
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	recovery.Add(provider.Message{Role: provider.RoleAssistant, Content: "answer"})
	recovery.Add(provider.Message{Role: provider.RoleUser, Content: "follow up"})
	second, err := recovery.SaveShutdownRecoveryBranch(RecoveryBranchOptions{OriginalPath: originalPath})
	if err != nil {
		t.Fatal(err)
	}
	if second.Path != first.Path {
		t.Fatalf("recovery lane changed from %q to %q", first.Path, second.Path)
	}
	msgs := recovery.Snapshot()
	digest, err := ContentDigestForMessages(msgs)
	if err != nil {
		t.Fatal(err)
	}
	meta, ok, err := LoadBranchMeta(second.Path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta ok=%v err=%v", ok, err)
	}
	if meta.Revision != 8 || meta.ContentDigest != digest || meta.RecoveryDigest != digest ||
		meta.ListingRevision != 8 || meta.ListingContentDigest != digest || meta.Turns != 2 {
		t.Fatalf("rewritten recovery meta = %+v", meta)
	}
	display, err := LoadSessionDisplayIndex(store.SessionDisplayIndex(second.Path))
	if err != nil || display.Revision != 8 || display.ContentDigest != digest || display.AuthoredTurns != 2 {
		t.Fatalf("display index = %+v err=%v", display, err)
	}
	event, err := readSessionEventIndex(second.Path)
	if err != nil || event == nil || event.Revision != 8 || event.ContentDigest != digest {
		t.Fatalf("event index = %+v err=%v", event, err)
	}
}
