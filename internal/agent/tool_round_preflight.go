package agent

import (
	"context"
	"fmt"
	"strings"

	"reasonix/internal/provider"
)

// preflightToolRound handles host-only blocks before executeBatch can dispatch
// anything. The returned unavailable tools feed the first-repair path later in
// handleToolRound; handled means this round has already reached a terminal or
// continuation decision.
func (a *Agent) preflightToolRound(ctx context.Context, state *runLoopState, text, reasoning string, calls []provider.ToolCall, usage *provider.Usage) (unavailable []string, handled, cont bool, err error) {
	if state.visibleFinalRepair {
		cont, err = a.handleVisibleFinalRepairToolRound(ctx, state, calls, usage, len(reasoning))
		return nil, true, cont, err
	}
	state.emptyFinalBlocks = 0
	state.usedAnyTool = true
	unavailable = a.unavailableContextualToolCalls(ctx, calls)
	if len(unavailable) == 0 || state.contextToolRepairs == 0 {
		return unavailable, false, false, nil
	}
	msg := fmt.Sprintf("blocked: context-unavailable tools were called again after the repair instruction: %s", strings.Join(unavailable, ", "))
	for _, call := range calls {
		a.session.Add(provider.Message{Role: provider.RoleTool, Content: msg, ToolCallID: call.ID, Name: call.Name})
	}
	if hasVisibleFinalAnswer(text) {
		cont, err = a.handleFinalResponse(ctx, state, text, reasoning, usage)
		return unavailable, true, cont, err
	}
	if len(unavailable) == 1 && unavailable[0] == "update_goal" {
		return unavailable, true, false, fmt.Errorf("model repeatedly called update_goal outside Goal mode without a visible answer")
	}
	return unavailable, true, false, fmt.Errorf("model repeatedly called context-unavailable tools without a visible answer: %s", strings.Join(unavailable, ", "))
}
