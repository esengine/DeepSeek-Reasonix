package control

import "reasonix/internal/provider"

type interruptedTailEvidence struct {
	turnID string
	states map[string]provider.ToolRunState
}

func (c *Controller) ledgerTailEvidence() *interruptedTailEvidence {
	if c == nil || c.turnEventLedger() == nil {
		return nil
	}
	o := c.turnEventLedger().OrphanRecovery()
	if o == nil {
		return nil
	}
	e := &interruptedTailEvidence{turnID: o.TurnID, states: map[string]provider.ToolRunState{}}
	for _, t := range o.Tools {
		if t.Started {
			e.states[t.ID] = provider.ToolRunUnknown
		} else {
			e.states[t.ID] = provider.ToolRunCancelled
		}
	}
	return e
}

func recordInterruptedAssistantRecovery(r *provider.InterruptedTurnRecovery, msgs []provider.Message, i int, evidence ...*interruptedTailEvidence) {
	results := make(map[string]provider.Message)
	for j := i + 1; j < len(msgs) && msgs[j].Role == provider.RoleTool && !msgs[j].LocalOnly; j++ {
		result := msgs[j]
		results[result.ToolCallID+"\x00"+result.Name] = result
	}
	for _, call := range msgs[i].ToolCalls {
		state := provider.ToolRunUnknown
		if result, ok := results[call.ID+"\x00"+call.Name]; ok && !provider.IsInterruptedPlaceholder(result) {
			state = provider.ToolResultRunState(result)
		} else if len(evidence) > 0 && evidence[0] != nil {
			if proven, ok := evidence[0].states[call.ID]; ok {
				state = proven
			}
		}
		provider.RecordToolRecovery(r, interruptedToolSummary(call), state)
		r.ToolCalls = append(r.ToolCalls, provider.ToolCallRecord{Identity: provider.ActionIdentity{CallID: call.ID, CanonicalTool: call.Name}, Arguments: []byte(call.Arguments), State: state})
		if state == provider.ToolRunUnknown {
			r.RequiresUserDecision = true
		}
	}
}
