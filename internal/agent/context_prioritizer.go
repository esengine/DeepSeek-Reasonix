package agent

import (
	"sync"

	"reasonix/internal/provider"
)

// ContextPrioritizerStats holds statistics about context-prioritization
// operations.
type ContextPrioritizerStats struct {
	TotalPrioritized int
	TokensReordered  int
}

// ContextPrioritizer reorders messages so that the most important context
// (system prompts, recent exchanges) is placed first while preserving the
// validity of the conversation structure. OPT-57.
type ContextPrioritizer struct {
	mu               sync.RWMutex
	totalPrioritized int
	tokensReordered  int
	priorityWeights  map[string]float64
}

// NewContextPrioritizer creates a new ContextPrioritizer with the default
// priority weights.
func NewContextPrioritizer() *ContextPrioritizer {
	return &ContextPrioritizer{
		priorityWeights: map[string]float64{
			"system":   1.0,
			"tools":    0.9,
			"recent":   0.8,
			"history":  0.5,
			"examples": 0.3,
		},
	}
}

// PrioritizeMessages reorders the provided message slice so that system
// prompts appear first, followed by the remaining messages in their original
// relative order. Message content and roles are never modified; only the
// ordering is adjusted where it is valid to do so.
func (c *ContextPrioritizer) PrioritizeMessages(messages []provider.Message) []provider.Message {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.totalPrioritized++

	if len(messages) <= 1 {
		return messages
	}

	var system, rest []provider.Message
	for _, msg := range messages {
		if msg.Role == provider.RoleSystem {
			system = append(system, msg)
		} else {
			rest = append(rest, msg)
		}
	}

	// If no system messages were out of order, nothing changes.
	if len(system) == 0 || len(system) == len(messages) {
		return messages
	}

	// Count reordered messages for statistics (approximate token impact).
	reordered := 0
	systemStart := 0
	for i, msg := range messages {
		if msg.Role == provider.RoleSystem {
			if i != systemStart {
				reordered++
			}
			systemStart++
		}
	}
	c.tokensReordered += reordered * 200

	result := make([]provider.Message, 0, len(messages))
	result = append(result, system...)
	result = append(result, rest...)
	return result
}

// ScoreMessage assigns a priority score to a message based on its type and
// position within the conversation. Higher scores indicate greater importance.
func (c *ContextPrioritizer) ScoreMessage(msg provider.Message, index int, total int) float64 {
	c.mu.RLock()
	weights := c.priorityWeights
	c.mu.RUnlock()

	var base float64
	switch msg.Role {
	case provider.RoleSystem:
		base = weights["system"]
	case provider.RoleTool:
		base = weights["tools"]
	default:
		if total > 0 && index >= total*7/10 {
			base = weights["recent"]
		} else {
			base = weights["history"]
		}
	}

	// Position boost: more recent messages (higher index) receive a small
	// additional score so that ties break toward recency.
	posBoost := 0.0
	if total > 0 {
		posBoost = float64(index) / float64(total) * 0.1
	}
	return base + posBoost
}

// GetStats returns a snapshot of the current prioritization statistics.
func (c *ContextPrioritizer) GetStats() ContextPrioritizerStats {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return ContextPrioritizerStats{
		TotalPrioritized: c.totalPrioritized,
		TokensReordered:  c.tokensReordered,
	}
}

// Reset clears all accumulated statistics.
func (c *ContextPrioritizer) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.totalPrioritized = 0
	c.tokensReordered = 0
}
