package agent

import (
	"strings"
	"sync"
)

// ReasoningOptStats holds statistics about reasoning-token optimization.
type ReasoningOptStats struct {
	TotalOptimized int
	TokensSaved    int
}

// ReasoningTokenOptimizer trims and controls the chain-of-thought reasoning
// content that is round-tripped across turns so that it does not consume an
// excessive portion of the context window. OPT-56.
type ReasoningTokenOptimizer struct {
	mu                  sync.RWMutex
	totalOptimized      int
	tokensSaved         int
	maxReasoningTokens  int
	reasoningHistory    []int
}

// NewReasoningTokenOptimizer creates a new ReasoningTokenOptimizer with a
// default maximum of 4096 reasoning tokens.
func NewReasoningTokenOptimizer() *ReasoningTokenOptimizer {
	return &ReasoningTokenOptimizer{
		maxReasoningTokens: 4096,
	}
}

// OptimizeReasoning truncates the reasoning content when it exceeds the
// configured token budget. One token is approximated as four characters, so
// the character budget is maxReasoningTokens*4. When the content is too long
// it is cut (preferably at a word boundary) and a "[reasoning truncated]"
// marker is appended. Statistics are updated to reflect the savings.
func (r *ReasoningTokenOptimizer) OptimizeReasoning(reasoningContent string, currentTurn int) string {
	r.mu.Lock()
	defer r.mu.Unlock()

	maxChars := r.maxReasoningTokens * 4

	// Record the estimated token count for this turn in the history.
	estimatedTokens := len(reasoningContent) / 4
	r.reasoningHistory = append(r.reasoningHistory, estimatedTokens)

	// Trim history to a rolling window aligned with the current turn so that
	// it does not grow without bound.
	_ = currentTurn // reserved for future per-turn policy adjustments
	if len(r.reasoningHistory) > 64 {
		r.reasoningHistory = r.reasoningHistory[len(r.reasoningHistory)-64:]
	}

	if len(reasoningContent) <= maxChars {
		return reasoningContent
	}

	r.totalOptimized++
	saved := (len(reasoningContent) - maxChars) / 4
	if saved < 0 {
		saved = 0
	}
	r.tokensSaved += saved

	// Truncate, preferring a clean break at whitespace when possible.
	cut := maxChars
	if idx := strings.LastIndexAny(reasoningContent[:maxChars], " \n\t"); idx > maxChars/2 {
		cut = idx
	}

	var sb strings.Builder
	sb.Grow(cut + 24)
	sb.WriteString(reasoningContent[:cut])
	sb.WriteString("\n[reasoning truncated]")
	return sb.String()
}

// ShouldIncludeReasoning reports whether reasoning content should be included
// in the outgoing payload for the given turn. Reasoning is included during the
// first three turns or whenever tool calls are present (which may need the
// signed thinking block to be replayed).
func (r *ReasoningTokenOptimizer) ShouldIncludeReasoning(currentTurn int, hasToolCalls bool) bool {
	return currentTurn <= 3 || hasToolCalls
}

// GetStats returns a snapshot of the current optimization statistics.
func (r *ReasoningTokenOptimizer) GetStats() ReasoningOptStats {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return ReasoningOptStats{
		TotalOptimized: r.totalOptimized,
		TokensSaved:    r.tokensSaved,
	}
}

// Reset clears all accumulated statistics and history.
func (r *ReasoningTokenOptimizer) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.totalOptimized = 0
	r.tokensSaved = 0
	r.reasoningHistory = nil
}
