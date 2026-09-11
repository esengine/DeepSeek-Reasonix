package agent

import (
	"path/filepath"
	"testing"
)

func TestSessionModelSelectionRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := SetBranchModelPreserveUpdated(path, "go/model"); err != nil {
		t.Fatal(err)
	}
	before, _, _ := LoadBranchMeta(path)
	if model, identity, ok := LoadSessionModelSelection(path); !ok || model != "go/model" || identity != "" {
		t.Fatal("legacy model must have no acknowledgement")
	}
	if err := SetBranchModelSelectionPreserveUpdated(path, "go/model", "acknowledged"); err != nil {
		t.Fatal(err)
	}
	if err := UpdateSessionMeta(path, "go/model", "preview", 1, false); err != nil {
		t.Fatal(err)
	}
	after, _, _ := LoadBranchMeta(path)
	if after.ModelIdentity != "acknowledged" || !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Fatal("listing update lost identity or changed activity")
	}
	if err := SetBranchModelPreserveUpdated(path, "go/other"); err != nil {
		t.Fatal(err)
	}
	if _, identity, _ := LoadSessionModelSelection(path); identity != "" {
		t.Fatal("old acknowledgement must not attach to another model")
	}
}
