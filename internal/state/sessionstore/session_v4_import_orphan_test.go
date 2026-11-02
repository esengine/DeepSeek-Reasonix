package sessionstore

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/state/store"
)

// A rebuildable index left behind after its transcript was removed describes a
// conversation that is not there; it must not make the import think the copy is
// already begun and then fail every run on the missing transcript.
func TestImportV4FromIgnoresIndexesWhoseTranscriptIsGone(t *testing.T) {
	s, _, _ := v4Project(t)
	root, err := os.OpenRoot(s.Root)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	dest := filepath.Join(t.TempDir(), "ws")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	path := v4ImportPath(dest, v4TestID)
	for _, orphan := range []string{store.SessionEventIndex(path), store.SessionDisplayIndex(path)} {
		if err := os.WriteFile(orphan, []byte(`{"stale":true}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	n, skipped, err := ImportV4From(root.FS(), dest, nil, nil)
	if n != 1 || len(skipped) != 0 || err != nil {
		t.Fatalf("import over orphan indexes = %d, skipped %v, err %v; want 1, none, nil", n, skipped, err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the transcript was not written: %v", err)
	}
}

// The list of unimported 1.x conversations hides one whose copy was begun; an
// index with no transcript beside it is not a copy.
func TestV4ListingShowsAConversationWhoseOnlyLeftoverIsAnIndex(t *testing.T) {
	_, _, sessions := v4Project(t)
	path := filepath.Join(sessions, "v4-"+v4TestID+".jsonl")
	if err := os.WriteFile(store.SessionEventIndex(path), []byte(`{"stale":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	list, err := ListSessions(sessions)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Path != path {
		t.Fatalf("list = %+v, want the 1.x conversation at %s", list, path)
	}
}

// An index that sits beside a real transcript is part of that copy and stays.
func TestImportV4FromKeepsIndexesBesideAnExistingTranscript(t *testing.T) {
	s, _, _ := v4Project(t)
	root, err := os.OpenRoot(s.Root)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	dest := filepath.Join(t.TempDir(), "ws")
	if n, _, err := ImportV4From(root.FS(), dest, nil, nil); n != 1 || err != nil {
		t.Fatalf("first import = %d, %v", n, err)
	}
	index := store.SessionEventIndex(v4ImportPath(dest, v4TestID))
	if err := os.WriteFile(index, []byte(`{"kept":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ImportV4From(root.FS(), dest, nil, nil); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(index); err != nil || string(b) != `{"kept":true}` {
		t.Fatalf("the index beside a live transcript was touched: %q, %v", b, err)
	}
}
