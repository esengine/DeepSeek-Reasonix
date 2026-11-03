package memory

import (
	"testing"
	"time"
)

func TestHigherImportanceRanksAheadOfAnEquallyRelevantFact(t *testing.T) {
	store := recallTestStore(t)
	for _, tc := range []struct {
		id         string
		importance int
	}{{"mem-low", 1}, {"mem-high", 3}} {
		recallTestWrite(t, store.Dir, Memory{
			ID: tc.id, Name: tc.id, Title: "deploy window", Description: "the deploy window runs on thursday evenings",
			Type: TypeProject, Scope: FactScopeProject, Importance: tc.importance,
			Body: "the deploy window runs on thursday evenings",
		})
	}
	result := AutoRecall(store, "when is the deploy window on thursday evenings", RecallOptions{})
	if len(result.Hits) < 2 || result.Hits[0].Memory.ID != "mem-high" {
		t.Fatalf("hits = %+v, want the high importance fact first", result.Hits)
	}
}

func TestASynonymFieldFindsAFactTheBodyWordsWouldMiss(t *testing.T) {
	store := recallTestStore(t)
	recallTestWrite(t, store.Dir, Memory{
		ID: "mem-alias", Name: "ship-cadence", Title: "Release cadence",
		Description: "how often a version goes out", Type: TypeProject, Scope: FactScopeProject,
		Keywords: "发版节奏 一天一版", Body: "At most one release per calendar day.",
	})
	result := AutoRecall(store, "发版节奏 怎么安排", RecallOptions{})
	if len(result.Hits) != 1 || result.Hits[0].Memory.ID != "mem-alias" {
		t.Fatalf("hits = %+v, want the fact found through its synonyms", result.Hits)
	}
}

func TestARecentlyUsedFactGetsAModestBoost(t *testing.T) {
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	if got := recentUseWeight(Memory{LastUsedAt: now.Add(-3 * 24 * time.Hour)}, now); got != recentUseBoost {
		t.Fatalf("recent weight = %v", got)
	}
	if got := recentUseWeight(Memory{LastUsedAt: now.Add(-90 * 24 * time.Hour)}, now); got != 1 {
		t.Fatalf("old weight = %v, want 1", got)
	}
	if importanceWeight(Memory{Importance: 1}) >= 1 || importanceWeight(Memory{Importance: 3}) <= 1 {
		t.Fatal("importance weights must straddle 1")
	}
}

func TestAConstraintInATouchedGalaxyIsBroughtEvenWithoutAWordMatch(t *testing.T) {
	store := recallTestStore(t)
	recallTestWrite(t, store.Dir, Memory{
		ID: "mem-rule", Name: "no-friday-release", Title: "No Friday release",
		Description: "never cut a release on a friday", Type: TypeConstraint, Scope: FactScopeProject,
		Galaxy: "release process", Body: "Never cut a release on a Friday.",
	})
	recallTestWrite(t, store.Dir, Memory{
		ID: "mem-cadence", Name: "release-cadence", Title: "Release cadence",
		Description: "versions go out at most once per day", Type: TypeProject, Scope: FactScopeProject,
		Galaxy: "release process", Body: "At most one release per calendar day for the studio line.",
	})
	result := AutoRecall(store, "how often may the studio line cut a release per day", RecallOptions{})
	found := map[string]bool{}
	for _, hit := range result.Hits {
		found[hit.Memory.ID] = true
	}
	if !found["mem-cadence"] || !found["mem-rule"] {
		t.Fatalf("hits = %+v, want the cadence fact and the constraint of the same galaxy", result.Hits)
	}
	if result.Hits[0].Memory.ID != "mem-rule" || result.Hits[0].Reason != "constraint" {
		t.Fatalf("the constraint must lead the block: %+v", result.Hits[0])
	}
}

func TestAConstraintOutsideTheTouchedGalaxyStaysOut(t *testing.T) {
	store := recallTestStore(t)
	recallTestWrite(t, store.Dir, Memory{
		ID: "mem-rule", Name: "no-prod-migration", Title: "No production migration",
		Description: "never run a migration against production", Type: TypeConstraint, Scope: FactScopeProject,
		Galaxy: "database", Body: "Never run a migration against production.",
	})
	recallTestWrite(t, store.Dir, Memory{
		ID: "mem-cadence", Name: "release-cadence", Title: "Release cadence",
		Description: "versions go out at most once per day", Type: TypeProject, Scope: FactScopeProject,
		Galaxy: "release process", Body: "At most one release per calendar day for the studio line.",
	})
	result := AutoRecall(store, "how often may the studio line cut a release per day", RecallOptions{})
	for _, hit := range result.Hits {
		if hit.Memory.ID == "mem-rule" {
			t.Fatalf("an unrelated constraint was recalled: %+v", result.Hits)
		}
	}
}

func TestAnExpiredConstraintIsNeverRecalled(t *testing.T) {
	store := recallTestStore(t)
	recallTestWrite(t, store.Dir, Memory{
		ID: "mem-rule", Name: "old-rule", Title: "Old release rule", Description: "an old release rule",
		Type: TypeConstraint, Scope: FactScopeProject, Galaxy: "release process",
		ExpiresAt: time.Now().Add(-24 * time.Hour), Body: "Release only on tuesdays for the studio line.",
	})
	result := AutoRecall(store, "release rule for the studio line on tuesdays", RecallOptions{})
	for _, hit := range result.Hits {
		if hit.Memory.ID == "mem-rule" {
			t.Fatalf("an expired constraint reached the block: %+v", result.Hits)
		}
	}
}

func TestAOneLetterGalaxyNameDoesNotTouchEveryQuery(t *testing.T) {
	docs := []autoRecallDoc{{memory: Memory{ID: "m", Galaxy: "a", Type: TypeConstraint}}}
	if got := touchedGalaxies("a long question about anything", nil, docs); len(got) != 0 {
		t.Fatalf("touched = %v, want none for a one-letter galaxy", got)
	}
}
