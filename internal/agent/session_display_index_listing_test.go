package agent

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/provider"
	"reasonix/internal/store"
)

func TestScanSessionDisplayIndexListingMatchesBuilderAfterEmptyUserTurn(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleUser, Images: []string{"data:image/png;base64,iVBORw0KGgo="}},
		{Role: provider.RoleAssistant, Content: "image response"},
		{Role: provider.RoleUser, Content: "later question"},
	}
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := writeSessionMessages(path, msgs); err != nil {
		t.Fatal(err)
	}
	digest, err := digestSessionMessages(msgs)
	if err != nil {
		t.Fatal(err)
	}
	built := BuildSessionDisplayIndex(msgs, 1, true, digest)
	scanned, err := ScanSessionDisplayIndex(path)
	if err != nil {
		t.Fatal(err)
	}
	if !scanned.ListingPreviewKnown || scanned.ListingPreview != built.ListingPreview ||
		scanned.AuthoredTurns != built.AuthoredTurns {
		t.Fatalf("scan listing = %q/%d known=%v, build = %q/%d", scanned.ListingPreview,
			scanned.AuthoredTurns, scanned.ListingPreviewKnown, built.ListingPreview, built.AuthoredTurns)
	}
}

func TestIndexedSessionListingRejectsEqualDisplayIndexMtime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	writeSessionFile(t, path, []provider.Message{{Role: provider.RoleUser, Content: "question"}})
	if _, err := RepairSessionListingProjection(t.Context(), path); err != nil {
		t.Fatal(err)
	}
	meta, ok, err := LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta ok=%v err=%v", ok, err)
	}
	contentTime := SessionContentModTime(path)
	indexPath := store.SessionDisplayIndex(path)
	if err := os.Chtimes(indexPath, contentTime, contentTime); err != nil {
		t.Fatal(err)
	}
	if _, _, fast, _, err := indexedSessionListing(path, meta); err != nil || fast {
		t.Fatalf("equal-mtime display index fast=%v err=%v, want fallback", fast, err)
	}
}

func TestIndexedSessionListingRejectsEqualEventIndexMtime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	session := NewSession("system")
	session.Add(provider.Message{Role: provider.RoleUser, Content: "question"})
	if err := session.SaveSnapshot(path); err != nil {
		t.Fatal(err)
	}
	meta, ok, err := LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta ok=%v err=%v", ok, err)
	}
	displayInfo, err := os.Stat(store.SessionDisplayIndex(path))
	if err != nil {
		t.Fatal(err)
	}
	contentTime := SessionContentModTime(path)
	if !displayInfo.ModTime().After(contentTime) {
		newer := contentTime.AddDate(0, 0, 1)
		if err := os.Chtimes(store.SessionDisplayIndex(path), newer, newer); err != nil {
			t.Fatal(err)
		}
	}
	logInfo, err := os.Stat(store.SessionEventLog(path))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(store.SessionEventIndex(path), logInfo.ModTime(), logInfo.ModTime()); err != nil {
		t.Fatal(err)
	}
	if _, _, fast, _, err := indexedSessionListing(path, meta); err != nil || fast {
		t.Fatalf("equal-mtime event index fast=%v err=%v, want fallback", fast, err)
	}
}
