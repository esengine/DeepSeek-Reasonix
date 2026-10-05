package control

import (
	"strings"
)

func normalizeBudgetClass(goal, class string, legacyMode GoalResearchMode) string {
	switch class {
	case budgetClassSimple, budgetClassWrite, budgetClassResearch:
		return class
	default:
		if strings.TrimSpace(goal) == "" && legacyMode != GoalResearchOn {
			return ""
		}
		return budgetClassForLegacyMode(goal, legacyMode)
	}
}

func goalStateNeedsMigration(state goalState, normalizedBudgetClass string) bool {
	expectedMode := GoalResearchOff
	if strings.TrimSpace(state.AutoResearchTaskID) != "" {
		expectedMode = GoalResearchOn
	}
	return state.ResearchMode != expectedMode ||
		(state.BudgetClass != "" && state.BudgetClass != normalizedBudgetClass) ||
		(strings.TrimSpace(state.Goal) != "" && state.TurnsLimit != unlimitedGoalTurns) ||
		state.NoProgressLimit != 0 || state.BudgetExtensions != 0
}
