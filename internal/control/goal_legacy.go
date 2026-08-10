package control

import (
	"strings"

	"reasonix/internal/evidence"
)

type legacyGoalRestore struct {
	taskID   string
	todos    []evidence.TodoItem
	epoch    uint64
	explicit bool
}

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
	return state.TokensLimit != 0 || state.ResearchMode != expectedMode ||
		(state.BudgetClass != "" && state.BudgetClass != normalizedBudgetClass)
}

// blockLegacyRestore fails closed only while the decoded sidecar still owns the
// active Goal epoch. The archive identity is held by Controller's legacy-only
// recovery boundary, never by the Goal FSM.
func (g *goalMachine) blockLegacyRestore(expectedEpoch uint64, reason string) (uint64, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.continuationEpoch != expectedEpoch {
		return 0, false
	}
	g.status = GoalStatusBlocked
	g.stopCause = stopCauseLegacyArchive
	g.block = clipGoalReason(reason)
	g.continuationEpoch++
	return g.continuationEpoch, true
}

// failLegacyRestorePersistence keeps a recovered archive retryable when the
// sidecar replacement fails. The recovered Goal text may remain in memory, but
// the Goal stays fail-closed while Controller retains the legacy identity until
// a later resume commits the migration durably.
func (g *goalMachine) failLegacyRestorePersistence(expectedEpoch uint64, reason string) (uint64, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.continuationEpoch != expectedEpoch {
		return 0, false
	}
	g.status = GoalStatusBlocked
	g.stopCause = stopCauseLegacyArchive
	g.block = clipGoalReason(reason)
	g.continuationEpoch++
	return g.continuationEpoch, true
}

// clearLegacyTaskID completes the sidecar migration after the Goal-only state
// has been durably written. The epoch check prevents a late completion from
// clearing the identity of a newer migration.
func (g *goalMachine) clearLegacyTaskID(expectedEpoch uint64) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.continuationEpoch != expectedEpoch {
		return false
	}
	g.legacyTaskID = ""
	return true
}

func (g *goalMachine) legacyArchiveBlockedState() (goal string, epoch uint64, ok bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.status != GoalStatusBlocked || g.stopCause != stopCauseLegacyArchive {
		return "", 0, false
	}
	return g.goal, g.continuationEpoch, true
}

func (c *Controller) replaceLegacyRestore(legacy legacyGoalRestore) {
	c.legacyRestoreMu.Lock()
	c.legacyRestore = legacy
	c.legacyRestoreMu.Unlock()
}

func (c *Controller) legacyRestoreSnapshot() (legacyGoalRestore, bool) {
	c.legacyRestoreMu.Lock()
	defer c.legacyRestoreMu.Unlock()
	legacy := c.legacyRestore
	return legacy, legacy.explicit || strings.TrimSpace(legacy.taskID) != ""
}

func (c *Controller) advanceLegacyRestoreEpoch(taskID string, from, to uint64) {
	c.legacyRestoreMu.Lock()
	defer c.legacyRestoreMu.Unlock()
	if c.legacyRestore.taskID == taskID && c.legacyRestore.epoch == from {
		c.legacyRestore.epoch = to
	}
}

func (c *Controller) clearLegacyRestore(taskID string, epoch uint64) {
	c.legacyRestoreMu.Lock()
	defer c.legacyRestoreMu.Unlock()
	if c.legacyRestore.taskID == taskID && c.legacyRestore.epoch == epoch {
		c.legacyRestore = legacyGoalRestore{}
	}
}

// fillGoalTextIfEmpty installs archive-recovered goal text without resetting counters.
func (g *goalMachine) fillGoalTextIfEmpty(expectedEpoch uint64, goal string) (uint64, bool) {
	goal = strings.TrimSpace(goal)
	if goal == "" {
		return 0, false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.continuationEpoch != expectedEpoch || strings.TrimSpace(g.goal) != "" {
		return 0, false
	}
	g.goal = goal
	if g.status == "" || g.stopCause == stopCauseLegacyArchive {
		g.status = GoalStatusRunning
	}
	if g.stopCause == stopCauseLegacyArchive {
		g.stopCause, g.block = "", ""
	}
	g.budgetClass = budgetClassResearch
	if g.turnsLimit < budgetQuota(g.budgetClass) {
		g.turnsLimit = budgetQuota(g.budgetClass)
	}
	if g.noProgressLimit == 0 {
		g.noProgressLimit = defaultNoProgressLimit
	}
	if g.scopeID == "" {
		g.scopeID = newGoalScopeID()
		g.deliveryCheckpoint = evidence.DeliveryCheckpoint{ScopeID: g.scopeID}
	}
	g.continuationEpoch++
	return g.continuationEpoch, true
}

// resumeLegacyArchive applies an archive recovery only while the same blocked
// Goal lifecycle is still current. Archive reads happen off-lock, so the epoch
// check prevents a stale recovery from replacing a concurrently installed Goal.
func (g *goalMachine) resumeLegacyArchive(expectedEpoch uint64, goal string) (uint64, bool) {
	goal = strings.TrimSpace(goal)
	if goal == "" {
		return 0, false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.continuationEpoch != expectedEpoch || g.status != GoalStatusBlocked || g.stopCause != stopCauseLegacyArchive {
		return 0, false
	}
	g.goal = goal
	g.status = GoalStatusRunning
	g.stopCause, g.block = "", ""
	g.budgetClass = budgetClassResearch
	if g.turnsLimit < budgetQuota(g.budgetClass) {
		g.turnsLimit = budgetQuota(g.budgetClass)
	}
	if g.noProgressLimit == 0 {
		g.noProgressLimit = defaultNoProgressLimit
	}
	if g.scopeID == "" {
		g.scopeID = newGoalScopeID()
		g.deliveryCheckpoint = evidence.DeliveryCheckpoint{ScopeID: g.scopeID}
	}
	g.continuationEpoch++
	return g.continuationEpoch, true
}
