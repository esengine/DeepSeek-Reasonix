package agent

import (
	"context"
	"fmt"
	"reasonix/internal/state/sessionstore"
	"strings"

	"reasonix/internal/contract/ablation"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
)

// A fold's index gives an address; recall is what reads it. The budget is per
// generation so the fold that freed the window cannot be undone by the turns
// after it: a recall loop runs out of budget instead of running forever.
const recallBudgetRatio = 0.10

const minRecallBudgetTokens = 2000

// recallBudget is one generation's ceiling on pulling folded content back.
func (a *contextWindow) recallBudget() int {
	window := a.effectiveContextWindow()
	if window <= 0 {
		return minRecallBudgetTokens
	}
	return max(minRecallBudgetTokens, int(float64(window)*recallBudgetRatio))
}

// RecallContext reads folded canonical positions, or searches for them. What
// comes back re-enters context as an ordinary tool result, never a projection.
// Both operations spend one generation budget: two would let a search refill
// what a read was refused.
func (a *Agent) RecallContext(ctx context.Context, req tool.RecallRequest) (tool.RecallResult, error) {
	return a.window().recallContext(ctx, req)
}

func (a *contextWindow) recallContext(_ context.Context, req tool.RecallRequest) (tool.RecallResult, error) {
	query := strings.TrimSpace(req.Query)
	if query != "" && a.ablation.Off(ablation.RecallSearch) {
		return tool.RecallResult{}, fmt.Errorf("recall: searching the folded region is not available in this configuration; read a #n address from the folded work index")
	}
	switch {
	case query != "" && len(req.Positions) > 0:
		return tool.RecallResult{}, fmt.Errorf("recall: give positions to read or a query to search, not both")
	case query == "" && len(req.Positions) == 0:
		return tool.RecallResult{}, fmt.Errorf("recall: no positions given")
	}
	canonical, _ := a.sess.conversation.SnapshotMessagesVersion()

	a.sess.win.compactionMu.Lock()
	defer a.sess.win.compactionMu.Unlock()
	state := &a.sess.win.compactionState
	covered := min(state.Projection.CoveredCount, len(canonical))
	if covered <= 0 {
		return tool.RecallResult{}, fmt.Errorf("recall: nothing has been folded in this session yet, so every position is still in your context")
	}
	if state.Recall.Generation != state.Generation {
		state.Recall = sessionstore.RecallLedger{Generation: state.Generation}
	}
	budget := a.recallBudget()
	left := budget - state.Recall.SpentTokens
	if query != "" {
		return a.searchRecallLocked(canonical[:covered], query, req, budget, left)
	}

	var body strings.Builder
	var recalled, missing []int
	for _, pos := range req.Positions {
		if pos < 0 || pos >= len(canonical) {
			missing = append(missing, pos)
			continue
		}
		if pos >= covered {
			return tool.RecallResult{}, fmt.Errorf("recall: #%d is not folded — it is still in your context, so read it there", pos)
		}
		rendered := renderTranscriptVerbatim(a.recallSpan(canonical, pos))
		if strings.TrimSpace(rendered) == "" {
			missing = append(missing, pos)
			continue
		}
		fmt.Fprintf(&body, "#%d\n%s\n", pos, strings.TrimRight(rendered, "\n"))
		recalled = append(recalled, pos)
	}
	if len(recalled) == 0 {
		return tool.RecallResult{BudgetLeft: left, Missing: missing},
			fmt.Errorf("recall: %s named nothing the transcript still holds", positionsText(missing))
	}
	// Refused whole rather than truncated: a half-recalled span reads as the
	// whole of what was there, which is the failure recall exists to prevent.
	cost := a.textTokens(body.String())
	if cost > left {
		return tool.RecallResult{BudgetLeft: left},
			fmt.Errorf("recall: %d tokens exceeds the %d left in this generation's recall budget — ask for fewer positions", cost, left)
	}
	state.Recall.SpentTokens += cost
	return tool.RecallResult{
		Text:       strings.TrimRight(body.String(), "\n"),
		Recalled:   recalled,
		Missing:    missing,
		Tokens:     cost,
		BudgetLeft: budget - state.Recall.SpentTokens,
	}, nil
}

// recallSpan returns the message at pos plus the tool results that answer it.
// An index line addresses the call, and a call without its result is the half
// that says least.
func (a *contextWindow) recallSpan(canonical []provider.Message, pos int) []provider.Message {
	span := []provider.Message{canonical[pos]}
	wanted := map[string]bool{}
	for _, tc := range canonical[pos].ToolCalls {
		wanted[tc.ID] = true
	}
	for i := pos + 1; i < len(canonical) && len(wanted) > 0; i++ {
		m := canonical[i]
		if m.Role != provider.RoleTool {
			break
		}
		if wanted[m.ToolCallID] {
			span = append(span, m)
			delete(wanted, m.ToolCallID)
		}
	}
	return span
}

func positionsText(positions []int) string {
	if len(positions) == 0 {
		return "the request"
	}
	parts := make([]string, len(positions))
	for i, p := range positions {
		parts[i] = fmt.Sprintf("#%d", p)
	}
	return strings.Join(parts, ", ")
}

var _ tool.ContextRecaller = (*Agent)(nil)
