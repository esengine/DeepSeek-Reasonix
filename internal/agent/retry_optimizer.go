package agent

import "sync"

// RetryStrategy defines how a particular provider error type should be retried.
type RetryStrategy struct {
	ErrorType        string
	MaxAttempts      int
	UseCachedContext bool
	BackoffMs        int
	StripTools       bool
}

// RetryOptimizerStats holds aggregated statistics about retry behaviour.
type RetryOptimizerStats struct {
	TotalRetries      int
	TokenSavedRetries int
	StrategiesTracked int
	AvgTokensSaved    float64
}

// ProviderRetryOptimizer manages provider retry strategies and tracks the
// token savings achieved by leveraging cached context on retries.
type ProviderRetryOptimizer struct {
	mu                sync.RWMutex
	totalRetries      int
	tokenSavedRetries int
	totalTokensSaved  int
	retryStrategies   map[string]*RetryStrategy
	maxRetries        int
}

// NewProviderRetryOptimizer creates a new optimizer preloaded with default
// retry strategies for common provider error categories.
func NewProviderRetryOptimizer() *ProviderRetryOptimizer {
	o := &ProviderRetryOptimizer{
		maxRetries:      3,
		retryStrategies: make(map[string]*RetryStrategy),
	}

	o.retryStrategies["rate_limit"] = &RetryStrategy{
		ErrorType:        "rate_limit",
		MaxAttempts:      3,
		UseCachedContext: true,
		BackoffMs:        1000,
		StripTools:       false,
	}
	o.retryStrategies["timeout"] = &RetryStrategy{
		ErrorType:        "timeout",
		MaxAttempts:      2,
		UseCachedContext: true,
		BackoffMs:        500,
		StripTools:       true,
	}
	o.retryStrategies["server_error"] = &RetryStrategy{
		ErrorType:        "server_error",
		MaxAttempts:      2,
		UseCachedContext: true,
		BackoffMs:        2000,
		StripTools:       false,
	}
	o.retryStrategies["network"] = &RetryStrategy{
		ErrorType:        "network",
		MaxAttempts:      3,
		UseCachedContext: true,
		BackoffMs:        500,
		StripTools:       false,
	}

	return o
}

// ShouldRetry reports whether another retry attempt is permitted for the
// given error type at the supplied (1-based) attempt number.
func (o *ProviderRetryOptimizer) ShouldRetry(errorType string, attempt int) bool {
	o.mu.RLock()
	defer o.mu.RUnlock()

	strategy, ok := o.retryStrategies[errorType]
	if !ok {
		// Unknown error types fall back to the global maxRetries cap.
		return attempt <= o.maxRetries
	}
	if attempt > strategy.MaxAttempts {
		return false
	}
	if attempt > o.maxRetries {
		return false
	}
	return true
}

// GetRetryStrategy returns the configured strategy for the given error
// type. When no specific strategy exists, a sensible default is returned.
func (o *ProviderRetryOptimizer) GetRetryStrategy(errorType string) *RetryStrategy {
	o.mu.RLock()
	defer o.mu.RUnlock()

	if strategy, ok := o.retryStrategies[errorType]; ok {
		return strategy
	}
	return &RetryStrategy{
		ErrorType:        errorType,
		MaxAttempts:      o.maxRetries,
		UseCachedContext: true,
		BackoffMs:        1000,
		StripTools:       false,
	}
}

// EstimateTokenSavings estimates the number of tokens saved on a retry by
// leveraging cached context (and optionally stripping tools) instead of
// resending the full prompt. When UseCachedContext is false no savings are
// possible and zero is returned.
func (o *ProviderRetryOptimizer) EstimateTokenSavings(errorType string, promptTokens int) int {
	strategy := o.GetRetryStrategy(errorType)
	if strategy == nil || !strategy.UseCachedContext {
		return 0
	}

	savings := float64(promptTokens) * 0.8
	if strategy.StripTools {
		toolsTokens := float64(promptTokens) * 0.15
		savings += toolsTokens
	}
	return int(savings)
}

// RecordRetry logs a retry event, updating aggregate counters and the
// running total of tokens saved.
func (o *ProviderRetryOptimizer) RecordRetry(errorType string, savedTokens int) {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.totalRetries++
	if savedTokens > 0 {
		o.tokenSavedRetries++
		o.totalTokensSaved += savedTokens
	}
}

// GetStats returns a snapshot of the optimizer's current statistics.
func (o *ProviderRetryOptimizer) GetStats() RetryOptimizerStats {
	o.mu.RLock()
	defer o.mu.RUnlock()

	stats := RetryOptimizerStats{
		TotalRetries:      o.totalRetries,
		TokenSavedRetries: o.tokenSavedRetries,
		StrategiesTracked: len(o.retryStrategies),
	}
	if o.tokenSavedRetries > 0 {
		stats.AvgTokensSaved = float64(o.totalTokensSaved) / float64(o.tokenSavedRetries)
	}
	return stats
}

// Reset clears all accumulated statistics and counters while keeping the
// configured retry strategies intact.
func (o *ProviderRetryOptimizer) Reset() {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.totalRetries = 0
	o.tokenSavedRetries = 0
	o.totalTokensSaved = 0
}
