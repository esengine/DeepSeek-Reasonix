package control

import (
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

func (c *Controller) cancelledTurnWasSilent(completion *guardedTurnCompletion) bool {
	if c == nil || c.executor == nil || completion == nil || completion.checkpoint == nil {
		return false
	}
	msgs := c.executor.Session().Snapshot()
	start := completion.checkpoint.messageIndex
	if start < 0 || start > len(msgs) {
		return false
	}
	for _, m := range msgs[start:] {
		if m.LocalOnly || m.Role == provider.RoleUser || m.Role == provider.RoleSystem {
			continue
		}
		if m.Role == provider.RoleAssistant && (m.Content != "" || m.ReasoningContent != "" || len(m.ToolCalls) > 0) {
			return false
		}
		if m.Role == provider.RoleTool && m.Content != "" {
			return false
		}
	}
	return true
}

func (c *Controller) applyToolRecoveryTurnStatus(done *event.Event, completion *guardedTurnCompletion) {
	if c == nil || c.executor == nil {
		return
	}
	if len(c.executor.PendingToolRecovery()) > 0 {
		done.Recovery = &event.RecoveryStatus{State: "recovery_required", Reason: "tool_effect_unconfirmed", RequiresUserDecision: true}
		return
	}
	if c.executor.SilentToolRecovery() || c.cancelledTurnWasSilent(completion) {
		done.Recovery = &event.RecoveryStatus{State: "recovery_required", Reason: "silent_interruption"}
	}
}

func (c *Controller) applyLedgerRecoveryFacts(r *provider.InterruptedTurnRecovery) {
	if c == nil || r == nil {
		return
	}
	e := c.ledgerTailEvidence()
	if e == nil {
		return
	}
	r.Cause = "runtime_restart"
	r.TurnID = e.turnID
	if len(r.ToolCalls) == 0 && len(r.CompletedTools) == 0 && !r.DroppedPartialText && !r.DroppedPartialReasoning {
		r.SilentInterruption = true
	}
}
