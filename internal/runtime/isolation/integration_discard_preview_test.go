package isolation

import (
	"errors"
	"os"
	"testing"

	"reasonix/internal/contract/tool"
)

func TestHostCanPreviewAndDiscardIntegrationAttemptWithoutRemovingOriginal(t *testing.T) {
	store, original := integrationFixture(t)
	i, err := store.PrepareIntegration(t.Context(), original.ID, "owner")
	if err != nil {
		t.Fatal(err)
	}
	id, root := i.ID(), i.WorkspaceRoot()
	_, err = store.PreviewDiscard(t.Context(), id)
	var refused tool.Refusal
	if !errors.As(err, &refused) || refused.Code != CodeBusy {
		t.Fatalf("active attempt was not refused: %v", err)
	}
	i.Close()
	preview, err := store.PreviewDiscard(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	write(t, root, "later.txt", "new work\n")
	if err := store.DiscardResultExpected(t.Context(), id, preview.Revision); !errors.As(err, &refused) || refused.Code != CodeStaleDiscard {
		t.Fatalf("stale integration preview accepted: %v", err)
	}
	if got := read(t, root, "later.txt"); got != "new work\n" {
		t.Fatal(got)
	}
	preview, err = store.PreviewDiscard(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.DiscardResultExpected(t.Context(), id, preview.Revision); err != nil {
		t.Fatal(err)
	}
	if err := store.DiscardResultExpected(t.Context(), id, preview.Revision); err != nil {
		t.Fatalf("repeat explicit decision: %v", err)
	}
	resumed, err := store.Resume(t.Context(), original.ID, "owner")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Abort(t.Context(), resumed)
	if err := store.DiscardResultExpected(t.Context(), id, preview.Revision); err != nil {
		t.Fatalf("terminal attempt borrowed original run liveness: %v", err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("attempt retained: %v", err)
	}
	if got := read(t, original.Cand.WorkspaceRoot, "a.txt"); got != "candidate\n" {
		t.Fatal("original result removed", got)
	}
}

func TestHostCannotManageAnIntegrationFromAnotherWorkspaceInTheRepository(t *testing.T) {
	store, original := integrationFixture(t)
	i, err := store.PrepareIntegration(t.Context(), original.ID, "owner")
	if err != nil {
		t.Fatal(err)
	}
	id := i.ID()
	i.Close()
	other := NewStore(store.managedRoot, t.TempDir(), store.repo).WithMutationCoordinator(store.mutations)
	catalog, err := other.Catalog(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range catalog.Results {
		if row.ID == id {
			t.Fatal("foreign workspace received integration result or actions")
		}
	}
	if _, err := other.PreviewDiscard(t.Context(), id); err == nil {
		t.Fatal("foreign workspace may discard this attempt")
	}
	if got := read(t, original.Cand.WorkspaceRoot, "a.txt"); got != "candidate\n" {
		t.Fatal(got)
	}
}
