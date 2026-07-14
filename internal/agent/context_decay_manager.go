package agent

import (
	"sync"
)

// ContextDecayStats holds statistics about context decay management.
type ContextDecayStats struct {
	DecayRate       float64
	TotalDecayed    int
	TokensSaved     int
	MessagesTracked int
}

// ContextDecayManager manages gradual decay of old context to reduce
// token usage over time. Messages that have been in the context for many
// turns are candidates for decay (summarization, truncation, or removal)
// to keep the context window manageable.
type ContextDecayManager struct {
	mu           sync.RWMutex
	decayRate    float64
	totalDecayed int
	tokensSaved  int
	messageAges  map[int]int // messageIndex -> age (in turns)
}

// NewContextDecayManager creates a new ContextDecayManager with a
// decay rate of 0.1 (10% per turn).
func NewContextDecayManager() *ContextDecayManager {
	return &ContextDecayManager{
		decayRate:   0.1,
		messageAges: make(map[int]int),
	}
}

// AgeMessages increments the age of all tracked messages and records the
// current turn's message at age 0 if it is not already tracked.
func (c *ContextDecayManager) AgeMessages(currentTurn int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Increment age of all existing tracked messages
	for idx := range c.messageAges {
		c.messageAges[idx]++
	}

	// Track the current turn's message at age 0 if not already present
	if _, exists := c.messageAges[currentTurn]; !exists {
		c.messageAges[currentTurn] = 0
	}
}

// ShouldDecay returns true if the message at the given index has aged
// beyond the decay threshold (age > 5).
func (c *ContextDecayManager) ShouldDecay(messageIndex int) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	age, exists := c.messageAges[messageIndex]
	if !exists {
		return false
	}
	return age > 5
}

// GetDecayPriority returns a priority score (0.0-1.0) for the message.
// Higher values indicate the message is more likely to be decayed.
// The priority is calculated as age * decayRate, clamped to [0.0, 1.0].
func (c *ContextDecayManager) GetDecayPriority(messageIndex int) float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	age, exists := c.messageAges[messageIndex]
	if !exists {
		return 0.0
	}

	priority := float64(age) * c.decayRate
	if priority > 1.0 {
		priority = 1.0
	}
	return priority
}

// RecordDecay records that a message was decayed and the number of tokens
// saved by the decay operation.
func (c *ContextDecayManager) RecordDecay(tokensSaved int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.totalDecayed++
	c.tokensSaved += tokensSaved
}

// GetStats returns statistics about the context decay manager.
func (c *ContextDecayManager) GetStats() ContextDecayStats {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return ContextDecayStats{
		DecayRate:       c.decayRate,
		TotalDecayed:    c.totalDecayed,
		TokensSaved:     c.tokensSaved,
		MessagesTracked: len(c.messageAges),
	}
}

// Reset resets all statistics and state.
func (c *ContextDecayManager) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.totalDecayed = 0
	c.tokensSaved = 0
	c.messageAges = make(map[int]int)
}
