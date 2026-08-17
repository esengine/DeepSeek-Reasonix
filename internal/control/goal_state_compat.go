package control

import "encoding/json"

var goalStateKnownFields = map[string]struct{}{
	"goal": {}, "status": {}, "researchMode": {}, "autoResearchTaskID": {},
	"scopeID": {}, "deliveryCheckpoint": {}, "turns": {}, "blocks": {},
	"block": {}, "strict": {}, "todos": {}, "budgetClass": {},
	"turnsUsed": {}, "turnsLimit": {}, "tokensUsed": {}, "requestsUsed": {},
	"workDurationMs": {},
	"tokensLimit":    {}, "noProgressTurns": {}, "noProgressLimit": {},
	"lastContinuationReason": {}, "lastEvaluatorReason": {}, "stopCause": {},
	"budgetExtensions": {}, "progressEvidence": {},
}

func goalStateUnknownFields(raw []byte) map[string]json.RawMessage {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return nil
	}
	for key := range goalStateKnownFields {
		delete(fields, key)
	}
	return cloneGoalStateExtra(fields)
}

func marshalGoalState(state goalState, extra map[string]json.RawMessage) ([]byte, error) {
	known, err := json.Marshal(state)
	if err != nil || len(extra) == 0 {
		return known, err
	}
	var merged map[string]json.RawMessage
	if err := json.Unmarshal(known, &merged); err != nil {
		return nil, err
	}
	for key, value := range extra {
		if _, current := goalStateKnownFields[key]; current {
			continue
		}
		merged[key] = append(json.RawMessage(nil), value...)
	}
	return json.Marshal(merged)
}

func cloneGoalStateExtra(in map[string]json.RawMessage) map[string]json.RawMessage {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]json.RawMessage, len(in))
	for key, value := range in {
		out[key] = append(json.RawMessage(nil), value...)
	}
	return out
}

func (g *goalMachine) grantSpendSliceLocked(fresh bool) {
	switch {
	case g.tokenBudget <= 0:
		g.tokensLimit = 0
	case fresh || g.tokensLimit <= g.tokensUsed:
		g.tokensLimit = g.tokensUsed + g.tokenBudget
	}
}

// migrateRemovedGoalPause clears pauses produced by gates no longer enforced.
func (g *goalMachine) migrateRemovedGoalPause() bool {
	if g.status != GoalStatusBlocked {
		return false
	}
	switch g.stopCause {
	case stopCauseBudgetTurns, stopCauseBudgetTokens, stopCauseGoalRunBudget, stopCauseGoalStuck, stopCauseNoProgress:
	default:
		return false
	}
	g.status = GoalStatusRunning
	g.stopCause = ""
	g.block = ""
	return true
}

// normalizeContinuousState runs under g.mu while loading an active sidecar.
func (g *goalMachine) normalizeContinuousState(legacyMode GoalResearchMode, legacyTaskID string) bool {
	if g.goal == "" {
		return false
	}
	migrated := false
	if g.budgetClass == "" {
		g.budgetClass = budgetClassForLegacyMode(g.goal, legacyMode)
	}
	if legacyTaskID != "" {
		g.budgetClass = budgetClassResearch
	}
	if g.turnsLimit != unlimitedGoalTurns {
		g.turnsLimit, migrated = unlimitedGoalTurns, true
	}
	if g.noProgressLimit != 0 {
		g.noProgressLimit, migrated = 0, true
	}
	if g.budgetExtensions != 0 {
		g.budgetExtensions, migrated = 0, true
	}
	legacyNumericPause := false
	switch g.stopCause {
	case stopCauseBudgetTurns, stopCauseBudgetTokens, stopCauseGoalRunBudget, stopCauseGoalStuck, stopCauseNoProgress:
		legacyNumericPause = true
	}
	if g.tokenBudget <= 0 {
		if g.tokensLimit != 0 {
			g.tokensLimit, migrated = 0, true
		}
	} else if legacyNumericPause || g.tokensLimit <= 0 {
		g.tokensLimit, migrated = g.tokensUsed+g.tokenBudget, true
	}
	return g.migrateRemovedGoalPause() || migrated
}
