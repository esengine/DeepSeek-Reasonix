package isolation

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDeclaredEnvironmentSurvivesRestartAndChangedDependencyIsNotSwept(t *testing.T) {
	root := gitRepo(t)
	write(t, root, ".gitignore", "deps/\n")
	write(t, root, "deps/package", "input\n")
	repo := opened(t, root)
	indexPath := filepath.Join(repo.GitDir, "index")
	indexBefore, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	store := NewStore(t.TempDir(), root, repo).WithEnvironmentCopies([]string{"deps"})
	entry, err := store.Begin(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	if entry.Environment == nil || entry.Environment.ContentDigest == "" {
		t.Fatalf("environment not prepared: %+v", entry.Environment)
	}
	write(t, entry.Cand.WorktreeRoot, "deps/package", "isolated dependency edit\n")
	if _, err := store.Complete(t.Context(), entry, Outcome{}, nil); err != nil {
		t.Fatal(err)
	}
	reopened := NewStore(store.managedRoot, root, repo)
	loaded, err := reopened.operationSnapshot(t.Context(), entry.ID)
	if err != nil || loaded.Environment == nil || loaded.State != Pending || len(loaded.Changes) != 0 {
		t.Fatalf("dependency-only result disappeared: %+v %v", loaded, err)
	}
	reopened.SweepExpired(t.Context(), -time.Second)
	if got := read(t, entry.Cand.WorktreeRoot, "deps/package"); got != "isolated dependency edit\n" {
		t.Fatalf("changed dependency was swept: %q", got)
	}
	if got := read(t, root, "deps/package"); got != "input\n" {
		t.Fatal("source dependency changed")
	}
	indexAfter, err := os.ReadFile(indexPath)
	if err != nil || !bytes.Equal(indexBefore, indexAfter) {
		t.Fatalf("environment preparation changed user index: %v", err)
	}
	if _, err := reopened.Discard(t.Context(), entry.ID); err != nil {
		t.Fatal(err)
	}
}

func TestIntegrationCandidatePreparesDeclaredInputsAndPinsReceipt(t *testing.T) {
	store, entry := integrationFixture(t)
	write(t, store.workspace, ".gitignore", "deps/\n")
	write(t, store.workspace, "deps/package", "current dependency\n")
	store.WithEnvironmentCopies([]string{"deps"})
	i, err := store.PrepareIntegration(t.Context(), entry.ID, "owner")
	if err != nil {
		t.Fatal(err)
	}
	defer i.Close()
	if i.Status().Environment == nil || i.Status().Environment.ContentDigest == "" {
		t.Fatal("integration omitted declared source inputs")
	}
	if got := read(t, i.WorkspaceRoot(), "deps/package"); got != "current dependency\n" {
		t.Fatalf("integration dependency: %q", got)
	}
	before, err := os.ReadFile(entryRecordPath(entry))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.operationSnapshot(t.Context(), entry.ID); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(entryRecordPath(entry))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("preparation changed original manifest: %v", err)
	}
	path, err := store.integrationPath(i.ID())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || len(fields["prepared_environment"]) == 0 {
		t.Fatalf("preparation receipt not durable: %v", err)
	}
}
