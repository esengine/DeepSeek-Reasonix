package control

import "encoding/json"

var goalStateKnownFields = map[string]struct{}{
	"goal": {}, "status": {}, "researchMode": {}, "autoResearchTaskID": {},
	"scopeID": {}, "deliveryCheckpoint": {}, "turns": {}, "blocks": {},
	"block": {}, "strict": {}, "todos": {}, "budgetClass": {},
	"turnsUsed": {}, "turnsLimit": {}, "tokensUsed": {}, "requestsUsed": {},
	"tokensLimit": {}, "noProgressTurns": {}, "noProgressLimit": {},
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

// migrateRemovedGoalPause clears pauses produced by gates no longer enforced.
func (g *goalMachine) migrateRemovedGoalPause() bool {
	if g.status != GoalStatusBlocked {
		return false
	}
	if g.stopCause != stopCauseBudgetTokens && g.stopCause != stopCauseNoProgress {
		return false
	}
	g.status = GoalStatusRunning
	g.stopCause = ""
	g.block = ""
	return true
}
