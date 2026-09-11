package turnevent

import (
	"errors"
	"fmt"
	"reasonix/internal/event"
	"reasonix/internal/eventwire"
	"reasonix/internal/provider"
)

type OrphanTool struct {
	ID, Name string
	Started  bool
}
type OrphanRecovery struct {
	TurnID string
	Tools  []OrphanTool
}

func (l *Ledger) OrphanRecovery() *OrphanRecovery {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.active == "" {
		return nil
	}
	o := &OrphanRecovery{TurnID: l.active}
	for _, r := range l.records {
		if r.TurnID == l.active && r.Source == "ledger_reopen" && r.Kind == "tool_result" && r.Event.Tool != nil {
			o.Tools = append(o.Tools, OrphanTool{ID: r.Event.Tool.ID, Name: r.Event.Tool.Name, Started: r.Event.Tool.RunState == provider.ToolRunUnknown})
		}
	}
	if len(o.Tools) == 0 {
		return nil
	}
	return o
}

func (l *Ledger) recoverToolEffects(pendingTools map[string]eventwire.Tool, pendingToolOrder []string) error {
	if l.active != "" && !l.terminal {
		requiresRecovery := false
		for _, id := range pendingToolOrder {
			tool, ok := pendingTools[id]
			if !ok {
				continue
			}
			state := provider.ToolRunUnknown
			if tool.RunState == provider.ToolRunPending {
				state = provider.ToolRunCancelled
			}
			if state == provider.ToolRunUnknown && !tool.ReadOnly {
				requiresRecovery = true
			}
			result := event.Event{Kind: event.ToolResult, TurnID: l.active, Source: "ledger_reopen", Tool: event.Tool{
				RunState: state, AttemptID: tool.AttemptID,
				ID: tool.ID, Name: tool.Name, ResolvedName: tool.ResolvedName,
				CapabilityID: tool.CapabilityID, ReadOnly: tool.ReadOnly, ParentID: tool.ParentID,
				Err: "interrupted: runtime restarted before the tool completed",
			}}
			if _, ok, appendErr := l.appendLocked(result, l.status); appendErr != nil || !ok {
				return fmt.Errorf("recover orphaned tool %s in turn %s: %w", id, l.active, appendErr)
			}
		}
		status := event.TurnInterrupted
		e := event.Event{Kind: event.TurnDone, TurnID: l.active, Source: "ledger_reopen", Err: errors.New("runtime restarted before the turn reached a terminal event")}
		if requiresRecovery {
			status = event.TurnRecoveryRequired
			e.Recovery = &event.RecoveryStatus{State: "recovery_required", Reason: "runtime_restart", RequiresUserDecision: true}
		}
		e.Status = status
		if _, ok, appendErr := l.appendLocked(e, status); appendErr != nil || !ok {
			return fmt.Errorf("recover orphaned turn %s: %w", l.active, appendErr)
		}
	}
	return nil
}
