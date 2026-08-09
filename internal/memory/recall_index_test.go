package memory

import "testing"

// The index and the direct store scan must be indistinguishable: same hits,
// same shadow, same suppression reasons.
func TestSetAutoRecallMatchesStoreScan(t *testing.T) {
	store := recallTestStore(t)
	recallTestWrite(t, store.Dir, Memory{
		ID: "mem-a", Name: "fast-check", Title: "Fast check command",
		Description: "The canonical fast check", Type: TypeProject, Scope: FactScopeProject,
		Body: "Run make check-fast before pushing.",
	})
	set := &Set{Store: store, recall: BuildRecallIndex(store)}
	for _, query := range []string{"what is the fast check command", "continue", "总结一下"} {
		direct := AutoRecall(store, query, RecallOptions{})
		indexed := set.AutoRecall(query, RecallOptions{})
		if len(direct.Hits) != len(indexed.Hits) || direct.Suppressed != indexed.Suppressed ||
			len(direct.ShadowHits) != len(indexed.ShadowHits) {
			t.Fatalf("index diverged from scan for %q: direct=%+v indexed=%+v", query, direct, indexed)
		}
	}
	if hits := set.AutoRecall("fast check command", RecallOptions{}).Hits; len(hits) != 1 {
		t.Fatalf("indexed recall should hit: %+v", hits)
	}

	var nilSet *Set
	if r := nilSet.AutoRecall("anything relevant", RecallOptions{}); r.Suppressed == "" {
		t.Fatalf("nil set must suppress, got %+v", r)
	}
}

// Writes rebuild the snapshot (and with it the index) through memory.Load —
// the invalidation contract that keeps Markdown the source of truth.
func TestLoadRebuildsIndexAfterWrite(t *testing.T) {
	root := t.TempDir()
	user, proj := root+"/user", root+"/project"
	mustMkdir(t, proj+"/.git")
	set := Load(Options{CWD: proj, UserDir: user})
	if r := set.AutoRecall("release branch for hotfixes", RecallOptions{}); len(r.Hits) != 0 {
		t.Fatalf("empty store recalled: %+v", r)
	}
	if _, err := set.Store.Save(Memory{Name: "release-branch", Description: "current release branch",
		Body: "Hotfixes target the release branch release/1.21."}); err != nil {
		t.Fatal(err)
	}
	reloaded := Load(Options{CWD: proj, UserDir: user})
	if r := reloaded.AutoRecall("release branch for hotfixes", RecallOptions{}); len(r.Hits) != 1 {
		t.Fatalf("reloaded index missed the new fact: %+v", r)
	}
}
