package agent

import (
	"fmt"

	"reasonix/internal/provider"
)

// summaryPlanMarginRatio is the planning headroom left under the window for
// estimator error. The fixed protocol reserve alone is under 1% of a 1M
// window, thinner than the tokenizer drift real sessions show (#9818).
const summaryPlanMarginRatio = 0.05

func summaryPlanReserve(window int) int {
	return max(protocolReserveTokens, int(float64(window)*summaryPlanMarginRatio))
}

// maximumSafeSummaryPrefixEnd returns the largest balanced contiguous prefix
// whose exact summary request leaves the collector's minimum output budget.
// The remaining middle and tail stay verbatim in the projection.
func (a *Agent) maximumSafeSummaryPrefixEnd(msgs []provider.Message, head, end int, instructions string) int {
	if head < 0 || end <= head || end > len(msgs) {
		return end
	}
	maxPromptTokens, enforce := a.safeSummaryPromptTokenLimit()
	if !enforce {
		return end
	}
	if maxPromptTokens <= 0 {
		return head
	}
	fits := func(candidate int) bool {
		fold, _ := withoutPinnedContextRevisions(msgs[head:candidate])
		request := a.summaryRequest(fold, instructions)
		return a.estimatedRequestTokens(request) <= maxPromptTokens
	}
	if fits(end) {
		return end
	}

	low, high, best := head+1, end-1, head
	for low <= high {
		mid := low + (high-low)/2
		if fits(mid) {
			best = mid
			low = mid + 1
		} else {
			high = mid - 1
		}
	}
	// A tail beginning with a tool result would split it from the assistant
	// tool-call message. Move the fold boundary back across the whole result
	// group; the assistant call and all of its results then remain together.
	for best > head && best < len(msgs) && msgs[best].Role == provider.RoleTool {
		best--
	}
	return best
}

// safeSummaryPromptTokenLimit is shared by prefix planning and the final
// post-extension guard. Unknown gateways conservatively honor the configured
// or learned window; explicitly independent providers retain the full fold.
func (a *Agent) safeSummaryPromptTokenLimit() (int, bool) {
	window := a.effectiveContextWindow()
	if window <= 0 || contextBudgetPolicyOf(a.svc.prov).WindowMode == provider.ContextWindowIndependent {
		return 0, false
	}
	return window - a.summaryOutputBudget() - summaryPlanReserve(window), true
}

// validateSafeSummaryRequest guards the final fold in the request form that
// will actually be sent.
func (a *Agent) validateSafeSummaryRequest(fold []provider.Message, instructions string, slim bool) error {
	maxPromptTokens, enforce := a.safeSummaryPromptTokenLimit()
	if !enforce {
		return nil
	}
	request := a.summaryRequest(fold, instructions)
	if slim {
		request = a.slimSummaryRequest(fold, instructions)
	}
	requestTokens := a.estimatedRequestTokens(request)
	if maxPromptTokens <= 0 || requestTokens > maxPromptTokens {
		return fmt.Errorf("%w: prepared summary request (%d tokens) exceeds safe prompt budget (%d)",
			errCheckpointRejected, requestTokens, maxPromptTokens)
	}
	return nil
}
