package agent

import (
	"fmt"
	"strings"

	"reasonix/internal/provider"
)

// summaryFoldPlan selects the cache-aligned summary request shape for the
// fold msgs[head:start]: the frozen main-request bytes when they cover the
// head, otherwise the live view (when it fits the admissible ceiling),
// otherwise the verbatim head + fold. The anchors name the fold region inside
// the replayed bytes so the summarizer summarizes only that segment.
func (a *Agent) summaryFoldPlan(msgs []provider.Message, head, start int) (prefix, extra []provider.Message, anchors string) {
	fold := msgs[head:start]
	if saved := a.savedMainRequest(); saved != nil && len(saved.messages) > 0 {
		region := fold
		if start > len(saved.messages) {
			extra = msgs[max(head, len(saved.messages)):start]
			region = append(append([]provider.Message(nil), fold...), extra...)
		}
		return saved.messages, extra, foldAnchorInstruction(region)
	}
	if a.summaryViewReplayFits(msgs) {
		return msgs, nil, foldAnchorInstruction(fold)
	}
	return msgs[:head], msgs[head:start], ""
}

func (a *Agent) summaryFoldEstimate(msgs []provider.Message, head, candidate int, instructions string) provider.Request {
	if saved := a.savedMainRequest(); saved != nil && len(saved.messages) > 0 {
		var extra []provider.Message
		if start := max(head, len(saved.messages)); start < candidate && candidate <= len(msgs) {
			extra = msgs[start:candidate]
		}
		anchors := ""
		if region := msgs[head:candidate]; len(region) > 0 && candidate <= len(msgs) {
			anchors = foldAnchorInstruction(append(append([]provider.Message(nil), region...), extra...))
		}
		return a.summaryRequest(saved.messages, extra, instructions+anchors)
	}
	if a.summaryViewReplayFits(msgs) {
		anchors := ""
		if region := msgs[head:candidate]; len(region) > 0 {
			anchors = foldAnchorInstruction(region)
		}
		// For estimation, use only the fold region as prefix (matching v1.36.0).
		// The actual summaryFoldPlan may use all visible messages for cache
		// alignment, but the estimate should reflect the minimal request shape.
		return a.summaryRequest(msgs[head:candidate], nil, instructions+anchors)
	}
	return a.summaryRequest(msgs[:head], msgs[head:candidate], instructions)
}

// summaryViewReplayFits reports whether the whole live view can be replayed
// as the summarizer prefix (view + instruction within the admissible input
// ceiling). Over-ceiling views must crop instead, at the cost of the prefix
// cache match.
func (a *Agent) summaryViewReplayFits(msgs []provider.Message) bool {
	// The safe ceiling already subtracts the (window-scaled) summary output
	// budget: a view that leaves no room for the digest must crop to the
	// frozen-prefix branch instead of replaying past the window.
	maxPromptTokens, enforce := a.safeSummaryPromptTokenLimit()
	if !enforce {
		return true
	}
	if maxPromptTokens <= 0 {
		return false
	}
	return a.estimatedRequestTokens(a.summaryRequest(msgs, nil, "")) <= maxPromptTokens
}

// foldAnchorInstruction names the fold region by verbatim excerpts so the
// summarizer can locate it inside the already-sent main-request bytes. The end
// excerpt is taken from the whole region (fold + extras) so new turns beyond
// the frozen prefix stay inside the summarized range.
func foldAnchorInstruction(region []provider.Message) string {
	startAnchor, endAnchor := "", ""
	for _, m := range region {
		if text := strings.TrimSpace(m.Content); text != "" {
			if startAnchor == "" {
				startAnchor = foldAnchor(text)
			}
			endAnchor = foldAnchor(text)
		}
	}
	if startAnchor == "" {
		return ""
	}
	return fmt.Sprintf("\n\nThe conversation above contains a segment to summarize. It starts with the excerpt %q and ends with the excerpt %q (both appear verbatim in the conversation above). Summarize ONLY that segment; leave everything else untouched.", startAnchor, endAnchor)
}

// foldAnchor truncates a message's content to a stable locating excerpt.
func foldAnchor(text string) string {
	const maxAnchor = 160
	text = strings.Join(strings.Fields(text), " ")
	if len(text) <= maxAnchor {
		return text
	}
	return text[:maxAnchor]
}
