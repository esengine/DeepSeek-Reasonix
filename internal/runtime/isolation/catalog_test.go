package isolation

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestIsolationCatalogRetainsUnsupportedAndUnreadableRecords(t *testing.T) {
	s, e := discardFixture(t)
	path := entryPath(e.Cand)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	fields["version"] = json.RawMessage(`99`)
	raw, _ = json.Marshal(fields)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	broken := filepath.Join(s.managedRoot, "candidate-broken", entryFile)
	write(t, filepath.Dir(broken), filepath.Base(broken), "{broken")
	catalog, err := s.Catalog(t.Context())
	if err != nil || len(catalog.Results) != 1 || len(catalog.Diagnostics) != 1 || catalog.Results[0].Reason == "" || catalog.Results[0].Verification != "unknown" {
		t.Fatalf("retained records disappeared or acquired verification: %+v %v", catalog, err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, raw) {
		t.Fatal("listing rewrote an unsupported result")
	}
	if after, err := os.ReadFile(broken); err != nil || string(after) != "{broken" {
		t.Fatal("listing removed an unreadable record")
	}
}

func TestIsolationCatalogReportsLiveRunsWithoutChangingOwnership(t *testing.T) {
	root := gitRepo(t)
	s := NewStore(t.TempDir(), root, opened(t, root))
	e, err := s.BeginOwned(t.Context(), root, "owner")
	if err != nil {
		t.Fatal(err)
	}
	defer s.endRun(e)
	catalog, err := s.Catalog(t.Context())
	if err != nil || len(catalog.Results) != 1 || catalog.Results[0].Activity != "active" || catalog.Results[0].State != Running {
		t.Fatalf("live run lost ownership or disappeared: %+v %v", catalog, err)
	}
	if _, err := s.Resume(t.Context(), e.ID, "owner"); err == nil {
		t.Fatal("catalog probe released the live run's OS ownership")
	}
}

func TestIsolationCatalogEmptyArraysAndWorkspaceScope(t *testing.T) {
	root := gitRepo(t)
	s := NewStore(t.TempDir(), root, opened(t, root))
	other := gitRepo(t)
	foreign := NewStore(s.managedRoot, other, opened(t, other))
	e, err := foreign.BeginOwned(t.Context(), other, "foreign-owner")
	if err != nil {
		t.Fatal(err)
	}
	defer foreign.endRun(e)
	catalog, err := s.Catalog(t.Context())
	if err != nil || len(catalog.Results) != 0 || len(catalog.Diagnostics) != 0 {
		t.Fatalf("another workspace's results were offered: %+v %v", catalog, err)
	}
	raw, err := json.Marshal(catalog)
	if err != nil || bytes.Contains(raw, []byte(`:null`)) {
		t.Fatalf("empty catalog contains null arrays: %s %v", raw, err)
	}
}
