package agent

import (
	"sync"
	"time"
)

// QueryPattern represents a recorded query pattern with frequency and prediction info.
type QueryPattern struct {
	Query      string
	Frequency  int
	LastSeen   int64
	NextLikely string
}

// CacheWarmingStats holds statistics about cache warming predictions.
type CacheWarmingStats struct {
	TotalPredictions   int
	CorrectPredictions int
	AccuracyRate       float64
	CacheWarmed        int
	PatternsTracked    int
}

// CacheWarmingScheduler predicts follow-up queries and pre-warms the cache.
type CacheWarmingScheduler struct {
	mu                  sync.RWMutex
	patterns            map[string]*QueryPattern
	totalPredictions    int
	correctPredictions  int
	warmedCache         int
}

// NewCacheWarmingScheduler creates a new CacheWarmingScheduler instance.
func NewCacheWarmingScheduler() *CacheWarmingScheduler {
	return &CacheWarmingScheduler{
		patterns: make(map[string]*QueryPattern),
	}
}

// RecordQuery records a user query, updating frequency and last seen timestamp.
// If the pattern exists, frequency is incremented; otherwise a new pattern is created.
func (c *CacheWarmingScheduler) RecordQuery(query string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now().Unix()

	if p, exists := c.patterns[query]; exists {
		p.Frequency++
		p.LastSeen = now
	} else {
		c.patterns[query] = &QueryPattern{
			Query:     query,
			Frequency: 1,
			LastSeen:  now,
		}
	}
}

// PredictNext finds the most common follow-up query based on recorded patterns.
// If no pattern matches, returns an empty string.
func (c *CacheWarmingScheduler) PredictNext(currentQuery string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Look for a pattern where the current query matches and a NextLikely is set.
	if p, exists := c.patterns[currentQuery]; exists {
		if p.NextLikely != "" {
			return p.NextLikely
		}
	}

	// Find the most frequent pattern as a general fallback.
	var best *QueryPattern
	for _, p := range c.patterns {
		if best == nil || p.Frequency > best.Frequency {
			best = p
		}
	}

	if best != nil && best.Query != currentQuery {
		return best.Query
	}

	return ""
}

// ShouldWarmup returns true if the query matches a known pattern with frequency > 2,
// indicating the cache should be pre-warmed for the predicted next query.
func (c *CacheWarmingScheduler) ShouldWarmup(query string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if p, exists := c.patterns[query]; exists {
		return p.Frequency > 2
	}
	return false
}

// RecordPrediction records whether a prediction was correct.
func (c *CacheWarmingScheduler) RecordPrediction(correct bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.totalPredictions++
	if correct {
		c.correctPredictions++
	}
}

// GetStats returns statistics about cache warming predictions.
func (c *CacheWarmingScheduler) GetStats() CacheWarmingStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var accuracy float64
	if c.totalPredictions > 0 {
		accuracy = float64(c.correctPredictions) / float64(c.totalPredictions)
	}

	return CacheWarmingStats{
		TotalPredictions:   c.totalPredictions,
		CorrectPredictions: c.correctPredictions,
		AccuracyRate:      accuracy,
		CacheWarmed:        c.warmedCache,
		PatternsTracked:    len(c.patterns),
	}
}

// Reset clears all recorded patterns and statistics.
func (c *CacheWarmingScheduler) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.patterns = make(map[string]*QueryPattern)
	c.totalPredictions = 0
	c.correctPredictions = 0
	c.warmedCache = 0
}
