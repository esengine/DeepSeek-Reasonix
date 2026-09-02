package memory

import (
	"strings"
	"testing"
	"time"

	"reasonix/internal/testenv"
)

// An expired fact is already refused by automatic recall. Leaving its line in
// the cached prefix charged every session for something the model could never
// act on.
func TestIndexDropsExpiredFacts(t *testing.T) {
	store := recallTestStore(t)
	now := time.Now().UTC()
	if _, err := store.SaveWithOptions(Memory{
		Name: "live-fact", Description: "still true", Type: TypeProject,
		Body: "the build runs with make",
	}, SaveOptions{}); err != nil {
		t.Fatalf("save live fact: %v", err)
	}
	if _, err := store.SaveWithOptions(Memory{
		Name: "dead-fact", Description: "long past", Type: TypeProject,
		Body: "the old release branch", ExpiresAt: now.Add(-24 * time.Hour),
	}, SaveOptions{}); err != nil {
		t.Fatalf("save expired fact: %v", err)
	}

	index := store.indexAt(now)
	if !strings.Contains(index, "live-fact") {
		t.Fatalf("live fact missing from the index:\n%s", index)
	}
	if strings.Contains(index, "dead-fact") {
		t.Fatalf("expired fact still charged to the prefix:\n%s", index)
	}
}

// The store's prefix footprint has to be reportable, because the budgets that
// would bound it are off by default: a cost nobody can see is not a decision
// anyone can make.
func TestPrefixCostCountsPinnedBodiesOnly(t *testing.T) {
	dir, global := testenv.TempDir(t), testenv.TempDir(t)
	store := StoreFor("", "")
	store.Dir, store.GlobalDir = dir, global
	const body = "always run repolint before pushing"
	if _, err := store.SaveWithOptions(Memory{
		Name: "pin-me", Description: "checklist", Type: TypeProject,
		Activation: ActivationPinned, Body: body,
	}, SaveOptions{}); err != nil {
		t.Fatalf("save pinned fact: %v", err)
	}
	set := &Set{Store: store, PinnedGuidance: store.pinnedGuidanceForProject()}

	cost := set.PrefixCost()
	if cost.Pinned != 1 || cost.PinnedChars != len([]rune(body)) {
		t.Fatalf("pinned cost = %+v, want the body counted verbatim", cost)
	}
	// The saved-fact index is not in the prefix, so it is not in this number.
	// Counting it here is what made the name a claim the code did not keep.
	if cost.Total() != cost.PinnedChars {
		t.Fatalf("total %d counts something other than pinned bodies: %+v", cost.Total(), cost)
	}
}

// Recall budgets are the user's: configuring more must yield more, not be
// clamped back to a ceiling the host picked.
func TestConfiguredRecallBudgetsAreNotClamped(t *testing.T) {
	if got := recallLimit(15); got != 15 {
		t.Errorf("recallLimit(15) = %d, want the configured value", got)
	}
	if got := recallCharBudget(120); got != 120 {
		t.Errorf("recallCharBudget(120) = %d, want the configured value", got)
	}
	if got := recallLimit(0); got != defaultAutoRecallLimit {
		t.Errorf("recallLimit(0) = %d, want the default", got)
	}
	if got := recallCharBudget(0); got != defaultAutoRecallChars {
		t.Errorf("recallCharBudget(0) = %d, want the default", got)
	}
	// A hit's snippet is a share of the budget, so it moves with it instead of
	// standing as an independent ceiling.
	if got := recallSnippetRunes(RecallOptions{Limit: 4, MaxChars: 8000}); got != 2000 {
		t.Errorf("snippet runes = %d, want each of four hits an equal share", got)
	}
}

// A reload must not silently revert the user's budgets to the defaults.
func TestLoadOptionsSurviveForReload(t *testing.T) {
	set := Load(Options{CWD: testenv.TempDir(t), UserDir: testenv.TempDir(t),
		PinnedBudgetChars: 4000, RecallLimit: 12, RecallMaxChars: 9000})
	opts := set.LoadOptions()
	if opts.PinnedBudgetChars != 4000 || opts.RecallLimit != 12 || opts.RecallMaxChars != 9000 {
		t.Fatalf("reload options = %+v, want the configured budgets carried", opts)
	}
	if set.Store.PinnedBudgetChars != 4000 {
		t.Fatalf("store budget = %d, want the configured ceiling", set.Store.PinnedBudgetChars)
	}
	if got := set.withConfiguredBudgets(RecallOptions{}); got.Limit != 12 || got.MaxChars != 9000 {
		t.Fatalf("recall defaults = %+v, want the user's budgets for an unset caller", got)
	}
}
