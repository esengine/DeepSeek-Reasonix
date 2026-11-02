package control

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestUnknownPersistedBudgetClassFallsBackToGoalClassification(t *testing.T) {
	dir := testenv.TempDir(t)
	path := filepath.Join(dir, "session.jsonl")
	raw, err := json.Marshal(goalState{Goal: "fix the crash in settings", Status: GoalStatusRunning, BudgetClass: "future-budget-class", TurnsLimit: 99})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(goalStatePath(path), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	g := &goalMachine{}
	g.setStatePath(goalStatePath(path))
	_, _, migrated, _ := g.restoreFromState(path)
	if !migrated || g.budgetClass != "" || g.turnsLimit != unlimitedGoalTurns {
		t.Fatalf("unknown budget restore = migrated:%v class:%q turns:%d", migrated, g.budgetClass, g.turnsLimit)
	}
}
