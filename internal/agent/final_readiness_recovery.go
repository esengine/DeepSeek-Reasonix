package agent

import (
	"encoding/json"
	"slices"

	"reasonix/internal/evidence"
	"reasonix/internal/provider"
)

// persistFinalReadinessRecovery records a provider-excluded, backward-safe
// checkpoint for plain turns. Goal turns already persist and restore their
// broader delivery checkpoint and auto-continue under the Goal FSM.
func (a *Agent) persistFinalReadinessRecovery(missing []string) {
	if a == nil || a.subagentDepth > 0 || a.turn.deliveryScopeActive || a.task.ledger == nil || a.sess.conversation == nil {
		return
	}
	checkpoint, err := json.Marshal(a.task.ledger.FinalReadinessCheckpoint())
	if err != nil {
		return
	}
	a.sess.conversation.Add(provider.Message{
		Role:       provider.RoleTool,
		ToolCallID: provider.LocalOnlyToolID,
		Name:       provider.LocalOnlyToolName,
		LocalOnly:  true,
		FinalReadinessRecovery: &provider.FinalReadinessRecovery{
			Pending:    true,
			Missing:    append([]string(nil), missing...),
			Checkpoint: checkpoint,
		},
	})
}

// pendingFinalReadinessRecovery returns only the newest unconsumed checkpoint.
// Any later real user turn makes an old card stale and prevents its evidence
// from being inherited by an unrelated task.
func (a *Agent) pendingFinalReadinessRecovery() *provider.FinalReadinessRecovery {
	if a == nil || a.sess.conversation == nil {
		return nil
	}
	for _, message := range slices.Backward(a.sess.conversation.Snapshot()) {
		if message.LocalOnly && message.FinalReadinessRecovery != nil && message.FinalReadinessRecovery.Pending {
			copy := *message.FinalReadinessRecovery
			copy.Missing = append([]string(nil), copy.Missing...)
			copy.Checkpoint = append(json.RawMessage(nil), copy.Checkpoint...)
			return &copy
		}
		if message.Role == provider.RoleUser && IsUserAuthoredTurn(message.Content) {
			return nil
		}
	}
	return nil
}

// PrepareFinalReadinessRecovery preserves the exhausted turn's evidence for
// exactly one explicit continuation. Live agents consume the in-memory bit;
// rebuilt agents restore the same ledger from the durable local-only marker.
func (a *Agent) PrepareFinalReadinessRecovery() bool {
	if a == nil || a.pending.finalReadinessRecoveryPrepared {
		return false
	}
	if !a.pending.finalReadinessRecovery {
		marker := a.pendingFinalReadinessRecovery()
		if marker == nil || a.task.ledger == nil {
			return false
		}
		var checkpoint evidence.FinalReadinessCheckpoint
		if json.Unmarshal(marker.Checkpoint, &checkpoint) != nil || !a.task.ledger.RestoreFinalReadinessCheckpoint(checkpoint) {
			return false
		}
	}
	a.pending.preserveEvidence = true
	a.pending.finalReadinessRecovery = false
	a.pending.finalReadinessRecoveryPrepared = true
	return true
}

// beginFinalReadinessRecovery consumes a pending card when a user turn truly
// begins and returns the evidence and audit state for that turn.
func (a *Agent) beginFinalReadinessRecovery() (preserveEvidence, recovered bool) {
	preserveEvidence = a.pending.preserveEvidence
	if a.sess.conversation != nil {
		a.sess.conversation.ConsumeFinalReadinessRecovery()
	}
	recovered = preserveEvidence || a.pending.finalReadinessRecovery
	a.pending.preserveEvidence = false
	a.pending.finalReadinessRecoveryPrepared = false
	if !preserveEvidence {
		a.pending.finalReadinessRecovery = false
	}
	return preserveEvidence, recovered
}

// PrepareDeliveryRecovery is the v1.25 compatibility name retained for older
// desktop bindings and external integrations. Final readiness also applies to
// targeted standard turns, so new code should use the generic method.
func (a *Agent) PrepareDeliveryRecovery() bool {
	return a.PrepareFinalReadinessRecovery()
}
