package memory

import (
	"strings"
	"testing"
)

func TestResolveActivationLegacyDefaults(t *testing.T) {
	legacyGlobalUser := Memory{Scope: FactScopeGlobal, Type: TypeUser}
	if ResolveActivation(legacyGlobalUser) != ActivationPinned {
		t.Fatal("legacy global user facts must stay pinned for compatibility")
	}
	if ResolveActivation(Memory{Scope: FactScopeGlobal, Type: TypeReference}) != ActivationRelevant {
		t.Fatal("global references were never guidance; they resolve relevant")
	}
	if ResolveActivation(Memory{Scope: FactScopeProject, Type: TypeFeedback}) != ActivationRelevant {
		t.Fatal("project facts without an explicit choice resolve relevant")
	}
	explicit := Memory{Scope: FactScopeGlobal, Type: TypeUser, Activation: ActivationRelevant}
	if ResolveActivation(explicit) != ActivationRelevant {
		t.Fatal("an explicit relevant choice beats the legacy default")
	}
}

// The coupling both #7995-adjacent layers must agree on: a fact is either in
// the stable prefix (pinned) or in the retrieval pool (relevant) — never in
// both, never in neither.
func TestRelevantGlobalGuidanceLeavesPrefixAndEntersRecall(t *testing.T) {
	store := recallTestStore(t)
	recallTestWrite(t, store.GlobalDir, Memory{
		ID: "mem-vue", Name: "vue2-preference", Title: "Vue 2 preference",
		Description: "Historic frontend preference", Type: TypeUser,
		Scope: FactScopeGlobal, Activation: ActivationRelevant,
		Body: "I mainly write Vue 2 with the options API and webpack tooling.",
	})

	if pinned := store.pinnedGuidanceForProject(); len(pinned) != 0 {
		t.Fatalf("relevant global guidance must not ride the prefix: %+v", pinned)
	}
	result := AutoRecall(store, "帮我看看这个 Vue 2 options API webpack 配置", RecallOptions{})
	if len(result.Hits) != 1 || result.Hits[0].Memory.ID != "mem-vue" {
		t.Fatalf("relevant global user facts must be auto-recallable, got %+v", result.Hits)
	}
}

func TestPinnedProjectFactRidesPrefixWithoutSelfShadowing(t *testing.T) {
	store := recallTestStore(t)
	recallTestWrite(t, store.Dir, Memory{
		ID: "mem-proj-pin", Name: "review-checklist", Title: "Review checklist",
		Description: "Always-on review checklist", Type: TypeProject,
		Scope: FactScopeProject, Activation: ActivationPinned,
		Body: "Always run the repolint before pushing.",
	})

	pinned := store.pinnedGuidanceForProject()
	if len(pinned) != 1 || pinned[0].ID != "mem-proj-pin" {
		t.Fatalf("project pinned fact must survive the project-shadow filter: %+v", pinned)
	}
	if hits := AutoRecall(store, "run the repolint review checklist", RecallOptions{}); len(hits.Hits) != 0 {
		t.Fatalf("pinned bodies must not be duplicated by recall: %+v", hits.Hits)
	}
}

func TestPinnedBudgetRejectsOverflowAndAllowsUpdates(t *testing.T) {
	store := recallTestStore(t)
	big := strings.Repeat("规", PinnedGuidanceBudgetChars-100)
	saved, err := store.SaveWithOptions(Memory{
		Name: "big-pin", Description: "large pinned fact",
		Activation: ActivationPinned, Body: big,
	}, SaveOptions{})
	if err != nil {
		t.Fatalf("within-budget pin rejected: %v", err)
	}

	if _, err := store.SaveWithOptions(Memory{
		Name: "second-pin", Description: "overflowing pinned fact",
		Activation: ActivationPinned, Body: strings.Repeat("x", 200),
	}, SaveOptions{}); err == nil || !strings.Contains(err.Error(), "REASONIX.md") {
		t.Fatalf("overflow must be rejected with the instructions hint, got %v", err)
	}

	// Updating the existing pinned fact must not double-count itself.
	if _, err := store.SaveWithOptions(Memory{
		ID: saved.Memory.ID, Description: "large pinned fact v2", Body: big + "改",
	}, SaveOptions{}); err != nil {
		t.Fatalf("self-update within budget rejected: %v", err)
	}
	updated, _ := store.Read(saved.Memory.ID)
	if ResolveActivation(updated) != ActivationPinned {
		t.Fatalf("update omitting activation must inherit pinned, got %+v", updated)
	}
}

func TestUnpinPersistsExplicitRelevantForLegacyGuidance(t *testing.T) {
	store := recallTestStore(t)
	recallTestWrite(t, store.GlobalDir, Memory{
		ID: "mem-legacy", Name: "legacy-pref", Title: "Legacy preference",
		Description: "Legacy global preference", Type: TypeUser,
		Scope: FactScopeGlobal, Body: "Old habit body.",
	})
	loaded, ok := store.Read("mem-legacy")
	if !ok || ResolveActivation(loaded) != ActivationPinned {
		t.Fatalf("legacy guidance should load virtually pinned, got %+v", loaded)
	}

	loaded.Activation = ActivationRelevant
	if _, err := store.SaveWithOptions(loaded, SaveOptions{}); err != nil {
		t.Fatal(err)
	}
	after, _ := store.Read("mem-legacy")
	if ResolveActivation(after) != ActivationRelevant {
		t.Fatalf("unpin must stick across reloads (explicit activation persisted), got %+v", after)
	}
}

func TestIndexMarksPinnedFacts(t *testing.T) {
	store := recallTestStore(t)
	if _, err := store.SaveWithOptions(Memory{
		Name: "pinned-fact", Description: "always on",
		Activation: ActivationPinned, Body: "short",
	}, SaveOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveWithOptions(Memory{
		Name: "plain-fact", Description: "retrieval only", Body: "short",
	}, SaveOptions{}); err != nil {
		t.Fatal(err)
	}
	index := store.Index()
	if !strings.Contains(index, "[project/project pinned]") {
		t.Fatalf("index must mark pinned facts:\n%s", index)
	}
	if strings.Contains(index, "plain-fact.md) — [project/project pinned]") {
		t.Fatalf("relevant facts must not carry the pinned marker:\n%s", index)
	}
}
