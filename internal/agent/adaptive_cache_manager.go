package agent

import "sync"

// AdaptiveCacheStats holds statistics about adaptive cache management.
type AdaptiveCacheStats struct {
	Strategy         string
	TotalAdaptations int
	TotalTokensSaved int
	CurrentHitRate   float64
}

// AdaptiveCacheManager dynamically adjusts the caching strategy based on
// observed hit-rate performance, deciding when to add or remove cache
// breakpoints. OPT-60.
type AdaptiveCacheManager struct {
	mu                sync.RWMutex
	strategy          string
	totalAdaptations  int
	totalTokensSaved  int
	hitRateHistory    []float64
	currentHitRate    float64
}

// NewAdaptiveCacheManager creates a new AdaptiveCacheManager with the default
// "balanced" strategy.
func NewAdaptiveCacheManager() *AdaptiveCacheManager {
	return &AdaptiveCacheManager{
		strategy: "balanced",
	}
}

// AdaptStrategy evaluates the current hit rate and miss count and returns the
// recommended strategy. When the hit rate is low (<0.3) the strategy becomes
// "aggressive" (more cache breakpoints); when it is high (>0.8) the strategy
// becomes "minimal" (fewer cache breakpoints); otherwise it stays "balanced".
// The internal strategy is updated when it changes.
func (a *AdaptiveCacheManager) AdaptStrategy(hitRate float64, missCount int) string {
	a.mu.Lock()
	defer a.mu.Unlock()

	var newStrategy string
	switch {
	case hitRate < 0.3:
		newStrategy = "aggressive"
	case hitRate > 0.8:
		newStrategy = "minimal"
	default:
		newStrategy = "balanced"
	}

	if newStrategy != a.strategy {
		a.strategy = newStrategy
		a.totalAdaptations++
	}

	return newStrategy
}

// RecordCachePerformance records the tokens served from cache (hitTokens) and
// the tokens that had to be reprocessed (missTokens), updating the running hit
// rate and history.
func (a *AdaptiveCacheManager) RecordCachePerformance(hitTokens int, missTokens int) {
	a.mu.Lock()
	defer a.mu.Unlock()

	total := hitTokens + missTokens
	if total <= 0 {
		return
	}

	rate := float64(hitTokens) / float64(total)
	a.hitRateHistory = append(a.hitRateHistory, rate)
	a.currentHitRate = rate
	a.totalTokensSaved += hitTokens

	// Keep the history bounded.
	if len(a.hitRateHistory) > 256 {
		a.hitRateHistory = a.hitRateHistory[len(a.hitRateHistory)-256:]
	}
}

// GetStrategy returns the currently active cache strategy.
func (a *AdaptiveCacheManager) GetStrategy() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.strategy
}

// ShouldAddBreakpoint reports whether an additional cache breakpoint should be
// inserted. A breakpoint is recommended when the miss count exceeds 1000 and
// the hit rate is below 0.5.
func (a *AdaptiveCacheManager) ShouldAddBreakpoint(missCount int, hitRate float64) bool {
	return missCount > 1000 && hitRate < 0.5
}

// GetStats returns a snapshot of the current adaptive-cache statistics.
func (a *AdaptiveCacheManager) GetStats() AdaptiveCacheStats {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return AdaptiveCacheStats{
		Strategy:         a.strategy,
		TotalAdaptations: a.totalAdaptations,
		TotalTokensSaved: a.totalTokensSaved,
		CurrentHitRate:   a.currentHitRate,
	}
}

// Reset clears all accumulated statistics and history, restoring the strategy
// to "balanced".
func (a *AdaptiveCacheManager) Reset() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.strategy = "balanced"
	a.totalAdaptations = 0
	a.totalTokensSaved = 0
	a.hitRateHistory = nil
	a.currentHitRate = 0
}
