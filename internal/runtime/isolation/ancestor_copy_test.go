package isolation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/contract/tool"
	"reasonix/internal/state/mutation"
)

func copySource(e *Entry) CopySource {
	return CopySource{ID: e.ID, Generation: e.RunGeneration, Owner: e.ResumeOwner, Workspace: e.Cand.WorkspaceRoot}
}

func TestAncestorCopyRetainsComparisonBaselineAndSurvivesSourceDiscard(t *testing.T) {
	store, original := integrationFixture(t)
	before, err := os.ReadFile(entryRecordPath(original))
	if err != nil {
		t.Fatal(err)
	}
	copy, err := store.CopyOwned(t.Context(), copySource(original), "descendant")
	if err != nil {
		t.Fatal(err)
	}
	if copy.ID == original.ID || copy.RunGeneration == original.RunGeneration || copy.Snap != original.Snap || copy.Evidence != nil || copy.Origin.Kind != "ancestor_copy" {
		t.Fatal("copy reused source ownership, evidence, or comparison identity")
	}
	write(t, copy.Cand.WorkspaceRoot, "b.txt", "new descendant work\n")
	if _, err := store.Complete(t.Context(), copy, Outcome{}, nil); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(entryRecordPath(original))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("copy rewrote source result", err)
	}
	if _, err := discardReviewed(t, store, original.ID); err != nil {
		t.Fatal(err)
	}
	reopened := NewStore(store.managedRoot, store.workspace, store.repo).WithMutationCoordinator(store.mutations)
	resumed, err := reopened.Resume(t.Context(), copy.ID, "descendant")
	if err != nil {
		t.Fatal(err)
	}
	if resumed.RunGeneration == copy.RunGeneration || read(t, resumed.Cand.WorkspaceRoot, "a.txt") != "candidate\n" {
		t.Fatal("copy cannot continue independently after source cleanup")
	}
	if _, err := reopened.Complete(t.Context(), resumed, Outcome{}, nil); err != nil {
		t.Fatal(err)
	}
	integration, err := reopened.PrepareIntegration(t.Context(), copy.ID, "descendant")
	if err != nil {
		t.Fatal(err)
	}
	defer integration.Close()
	for name, want := range map[string]string{"a.txt": "candidate\n", "b.txt": "new descendant work\n"} {
		if got := read(t, integration.WorkspaceRoot(), name); got != want {
			t.Fatalf("copy lost original comparison delta: %s=%q", name, got)
		}
	}
	d, err := reopened.managedResultMetadata(copy.ID)
	if err != nil || d.Version != 6 {
		t.Fatalf("copy did not retain previous-reader barrier: %+v %v", d, err)
	}
}

func TestAncestorCopyRejectsStaleActiveDriftAndCancelledSources(t *testing.T) {
	store, source := integrationFixture(t)
	identity := copySource(source)
	stale := identity
	stale.Generation = "old"
	if _, err := store.CopyOwned(t.Context(), stale, "descendant"); err == nil {
		t.Fatal("stale generation copied")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := store.CopyOwned(ctx, identity, "descendant"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled copy: %v", err)
	}
	running, err := store.Resume(t.Context(), source.ID, "owner")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CopyOwned(t.Context(), copySource(running), "descendant"); err == nil {
		t.Fatal("active source copied")
	}
	if _, err := store.Complete(t.Context(), running, Outcome{}, nil); err != nil {
		t.Fatal(err)
	}
	write(t, running.Cand.WorkspaceRoot, "a.txt", "external drift\n")
	if _, err := store.CopyOwned(t.Context(), copySource(running), "descendant"); err == nil {
		t.Fatal("unsealed external write copied")
	}
	if got := read(t, store.workspace, "a.txt"); got != "one\n" {
		t.Fatal("rejected copy changed source")
	}
}

func TestAncestorCopyKeepsDeclaredInputsIndependent(t *testing.T) {
	root := gitRepo(t)
	write(t, root, ".gitignore", "deps/\n")
	write(t, root, "deps/input", "declared dependency\n")
	coordinator, err := mutation.New(root, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	store := NewStore(t.TempDir(), root, opened(t, root)).WithMutationCoordinator(coordinator).WithEnvironmentCopies([]string{"deps"})
	original, err := store.BeginOwned(t.Context(), root, "owner")
	if err != nil {
		t.Fatal(err)
	}
	write(t, original.Cand.WorkspaceRoot, "a.txt", "original delta\n")
	if _, err := store.Complete(t.Context(), original, Outcome{}, nil); err != nil {
		t.Fatal(err)
	}
	copy, err := store.CopyOwned(t.Context(), copySource(original), "descendant")
	if err != nil {
		t.Fatal(err)
	}
	if read(t, copy.Cand.WorktreeRoot, "deps/input") != "declared dependency\n" {
		t.Fatal("declared input omitted")
	}
	write(t, copy.Cand.WorktreeRoot, "deps/input", "copy edit\n")
	if read(t, original.Cand.WorktreeRoot, "deps/input") != "declared dependency\n" || read(t, root, "deps/input") != "declared dependency\n" {
		t.Fatal("declared inputs share writable storage")
	}
	if _, err := store.Complete(t.Context(), copy, Outcome{}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CopyOwned(t.Context(), copySource(copy), "grandchild"); err == nil {
		t.Fatal("copy silently omitted unpublished dependency changes")
	}
}

func TestAncestorCopyPreparationCrashRetainsVersionedRecovery(t *testing.T) {
	store, original := integrationFixture(t)
	var retained *Entry
	store.preparationProbe = func(state State, e *Entry) {
		if state == Preparing {
			retained = e
			panic("simulated preparation loss")
		}
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("preparation did not stop")
			}
		}()
		_, _ = store.CopyOwned(t.Context(), copySource(original), "descendant")
	}()
	if retained == nil {
		t.Fatal("copy was not durably registered")
	}
	store.endRun(retained)
	store.preparationProbe = nil
	loaded, err := store.lookup(retained.ID)
	if err != nil || loaded.State != RecoveryRequired {
		t.Fatalf("interrupted preparation was lost: %+v %v", loaded, err)
	}
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(retained.Cand.WorktreeRoot), entryFile))
	if err != nil {
		t.Fatal(err)
	}
	var d durableEntry
	if json.Unmarshal(raw, &d) != nil || d.Version != 6 {
		t.Fatal("recovery lost protocol barrier")
	}
	if _, err := store.Resume(t.Context(), retained.ID, "descendant"); err == nil {
		t.Fatal("incomplete scene resumed")
	} else {
		var refusal tool.Refusal
		if !errors.As(err, &refusal) || refusal.Code != CodeRecoveryRequired {
			t.Fatalf("unexpected recovery refusal: %v", err)
		}
	}
}

func TestAncestorCopyUsesDirectParentRegistration(t *testing.T) {
	parentStore, parent, children, child := descendantFixture(t)
	coordinator, err := mutation.New(children.workspace, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	children.WithMutationCoordinator(coordinator)
	write(t, child.Cand.WorkspaceRoot, "child.txt", "retained child\n")
	if _, err := children.Finish(t.Context(), child); err != nil {
		t.Fatal(err)
	}
	copy, err := parentStore.CopyChild(t.Context(), parent, children, copySource(child), "descendant")
	if err != nil {
		t.Fatal(err)
	}
	if copy.Lineage == nil || copy.Lineage.Parent == nil || copy.Lineage.Parent.ID != parent.ID || copy.Lineage.Parent.Generation != parent.RunGeneration {
		t.Fatal("copy inherited the ancestor instead of its executing direct parent")
	}
	if _, err := children.Finish(t.Context(), copy); err != nil {
		t.Fatal(err)
	}
	if _, err := parentStore.Finish(t.Context(), parent); refusalCodeOf(err) != CodeDependentResults {
		t.Fatalf("parent forgot its retained copy: %v", err)
	}
}

func TestAncestorCopyRechecksCurrentDeclaredInputReadPolicy(t *testing.T) {
	root := gitRepo(t)
	write(t, root, ".gitignore", "deps/\n")
	write(t, root, "deps/input", "dependency\n")
	coordinator, err := mutation.New(root, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	store := NewStore(t.TempDir(), root, opened(t, root)).WithMutationCoordinator(coordinator).WithEnvironmentCopies([]string{"deps"})
	original, err := store.BeginOwned(t.Context(), root, "owner")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Complete(t.Context(), original, Outcome{}, nil); err != nil {
		t.Fatal(err)
	}
	store.WithEnvironmentCopies([]string{"deps"}, filepath.Join(root, "deps/input"))
	if _, err := store.CopyOwned(t.Context(), copySource(original), "descendant"); err == nil {
		t.Fatal("copy bypassed a new forbidden-read policy using the candidate path")
	}
}
