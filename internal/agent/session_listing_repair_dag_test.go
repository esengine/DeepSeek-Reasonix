package agent

import (
	"context"
	"os"
	"testing"

	"reasonix/internal/store"
)

// A listing repair replays the transcript and rewrites the event index; on a
// schema-2 log that index is the head index and must stay schema 2.
func TestListingRepairKeepsSchemaTwoHeadIndex(t *testing.T) {
	path := dagTestSession(t)
	dagSavedSession(t, path, "q1", "a1")
	if err := os.Remove(store.SessionEventIndex(path)); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(store.SessionDisplayIndex(path)); err != nil {
		t.Fatal(err)
	}
	if err := UpdateBranchMeta(path, false, func(meta *BranchMeta) error {
		meta.ListingRevision, meta.ListingContentDigest = 0, ""
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	result, err := RepairSessionListingProjection(context.Background(), path)
	if err != nil || result.Status != SessionListingRepairApplied {
		t.Fatalf("repair = %+v err=%v", result, err)
	}
	idx, err := ReadSessionHeadIndex(path)
	if err != nil || idx == nil || !idx.Current(path) || idx.SelectedHead != SessionMainHead || len(idx.Heads) != 1 {
		t.Fatalf("head index after repair = %+v err=%v, want a current schema-2 index", idx, err)
	}
	if _, err := readSessionEventIndex(path); err == nil {
		t.Fatal("repair must not write a schema-1 index over a schema-2 log")
	}
}
