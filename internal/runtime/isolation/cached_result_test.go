package isolation

import (
	"path/filepath"
	"slices"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestCachedResultCannotResurrectAfterAnotherStoreSettles(t *testing.T) {
	for _, settlement := range []string{"apply", "discard"} {
		for _, staleOperation := range []string{"paths", "apply", "discard"} {
			t.Run(settlement+"/"+staleOperation, func(t *testing.T) {
				repo, managed := gitRepo(t), testenv.TempDir(t)
				producer := NewStore(managed, repo, opened(t, repo))
				report, err := producer.Execute(t.Context(), writes(map[string]string{"a.txt": "candidate\n"}), repo, "change a", "")
				if err != nil {
					t.Fatal(err)
				}
				id := idOf(t, report)
				cached := NewStore(managed, repo, opened(t, repo))
				if paths, err := cached.Paths(id); err != nil || len(paths) != 1 {
					t.Fatalf("prime result: %v, %v", paths, err)
				}
				if settlement == "apply" {
					_, err = producer.Apply(t.Context(), id)
				} else {
					_, err = producer.Discard(t.Context(), id)
				}
				if err != nil {
					t.Fatal(err)
				}
				if slices.Contains(cached.Pending(), id) {
					t.Error("settled result offered before another cache lookup")
				}
				// Return the source to the original baseline after settlement. A
				// stale cached tree must not authorize writing the result again.
				write(t, repo, "a.txt", "one\n")
				switch staleOperation {
				case "paths":
					_, err = cached.Paths(id)
				case "apply":
					_, err = cached.Apply(t.Context(), id)
				case "discard":
					_, err = cached.Discard(t.Context(), id)
				}
				if err == nil {
					t.Errorf("cached %s still authorized after %s", staleOperation, settlement)
				}
				if got := read(t, repo, "a.txt"); got != "one\n" {
					t.Errorf("settled candidate resurrected into source: %q", got)
				}
				if slices.Contains(cached.Pending(), id) {
					t.Error("settled result still offered from stale cache")
				}
			})
		}
	}
}

func TestCachedResultRechecksPublishedIdentity(t *testing.T) {
	repo, managed := gitRepo(t), testenv.TempDir(t)
	store := NewStore(managed, repo, opened(t, repo))
	report, err := store.Execute(t.Context(), writes(map[string]string{"a.txt": "candidate\n"}), repo, "change a", "")
	if err != nil {
		t.Fatal(err)
	}
	id := idOf(t, report)
	entry, err := store.lookup(id)
	if err != nil {
		t.Fatal(err)
	}
	// Model external record replacement explicitly; normal writers do not
	// produce this deliberately malformed identity.
	write(t, filepath.Dir(entry.Cand.WorktreeRoot), entryFile, `{"id":"another-result"}`)
	if _, err := store.Apply(t.Context(), id); err == nil {
		t.Error("cached result bypassed changed durable identity")
	}
	if got := read(t, repo, "a.txt"); got != "one\n" {
		t.Errorf("unbound cached identity wrote source: %q", got)
	}
	if got := read(t, entry.Cand.WorkspaceRoot, "a.txt"); got != "candidate\n" {
		t.Errorf("refused cached identity lost candidate bytes: %q", got)
	}
}

func TestCachedResultChangeRequiresFreshPathAdmission(t *testing.T) {
	repo, managed := gitRepo(t), testenv.TempDir(t)
	store := NewStore(managed, repo, opened(t, repo))
	report, err := store.Execute(t.Context(), writes(map[string]string{"a.txt": "candidate\n"}), repo, "change a", "")
	if err != nil {
		t.Fatal(err)
	}
	id := idOf(t, report)
	apply := NewApplyTool(store)
	if paths, err := apply.DeclaredWritePaths(t.Context(), args(id)); err != nil || len(paths) != 1 {
		t.Fatalf("initial path admission: %v, %v", paths, err)
	}
	entry, err := store.lookup(id)
	if err != nil {
		t.Fatal(err)
	}
	write(t, entry.Cand.WorkspaceRoot, "extra.txt", "added after admission\n")
	if _, err := apply.Execute(t.Context(), args(id)); err == nil {
		t.Fatal("changed candidate applied under its previous path admission")
	}
	if got := read(t, repo, "a.txt"); got != "one\n" {
		t.Errorf("refused candidate changed source: %q", got)
	}
	if paths, err := apply.DeclaredWritePaths(t.Context(), args(id)); err != nil || len(paths) != 2 {
		t.Fatalf("fresh path admission: %v, %v", paths, err)
	}
	if _, err := apply.Execute(t.Context(), args(id)); err != nil {
		t.Fatal(err)
	}
	if got := read(t, repo, "extra.txt"); got != "added after admission\n" {
		t.Errorf("readmitted result lost new content: %q", got)
	}
}
