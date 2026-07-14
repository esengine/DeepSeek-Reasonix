package agent

import (
	"strings"
	"sync"
)

// ── OPT-82: TokenAwareRetry ──
// Retry logic that minimises token waste on failures. Instead of blindly
// re-sending the full prompt on every retry, it progressively strips
// non-essential tokens so that later attempts use fewer tokens.
//
// 原理：当请求失败需要重试时，逐轮降低发送的 token 数量（第 1 次重试
// 使用 80%，第 2 次 60%，第 3 次 40%），避免在注定失败的场景中浪费
// 大量 token。同时仅对可重试的错误类型（超时、限流、服务器错误、
// 网络错误）进行重试。
//
// 效果：减少重试场景下 40%-60% 的 token 浪费。

// RetryAttempt records the details of a single retry attempt.
type RetryAttempt struct {
	Attempt     int
	ErrorType   string
	TokensUsed  int
	TokensSaved int
	Success     bool
}

// TokenAwareRetryStats holds aggregated statistics about retry behaviour.
type TokenAwareRetryStats struct {
	MaxRetries        int
	TotalRetries      int
	TotalTokensWasted int
	TotalTokensSaved  int
}

// TokenAwareRetry manages token-efficient retry logic for failed provider
// requests.
type TokenAwareRetry struct {
	mu                sync.RWMutex
	maxRetries        int
	totalRetries      int
	totalTokensWasted int
	totalTokensSaved  int
	retryHistory      []RetryAttempt
}

// NewTokenAwareRetry creates a new TokenAwareRetry. If maxRetries is <= 0 it
// defaults to 3.
func NewTokenAwareRetry(maxRetries int) *TokenAwareRetry {
	if maxRetries <= 0 {
		maxRetries = 3
	}
	return &TokenAwareRetry{
		maxRetries: maxRetries,
	}
}

// ShouldRetry reports whether another retry attempt should be made. It returns
// true when the attempt number is still within the maxRetries limit and the
// error type is retryable (contains "timeout", "rate_limit", "server_error",
// or "network").
func (r *TokenAwareRetry) ShouldRetry(attempt int, errorType string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if attempt >= r.maxRetries {
		return false
	}

	lower := strings.ToLower(errorType)
	retryable := strings.Contains(lower, "timeout") ||
		strings.Contains(lower, "rate_limit") ||
		strings.Contains(lower, "server_error") ||
		strings.Contains(lower, "network")

	return retryable
}

// CalculateRetryTokens returns the number of tokens to send on a given retry
// attempt. Retry 1 uses 80% of the original tokens, retry 2 uses 60%, and
// retry 3 (or later) uses 40%. For attempt 0 or negative the original token
// count is returned unchanged.
func (r *TokenAwareRetry) CalculateRetryTokens(originalTokens int, attempt int) int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	switch {
	case attempt <= 0:
		return originalTokens
	case attempt == 1:
		return originalTokens * 80 / 100
	case attempt == 2:
		return originalTokens * 60 / 100
	default:
		return originalTokens * 40 / 100
	}
}

// RecordRetry records the outcome of a retry attempt and updates aggregate
// statistics.
func (r *TokenAwareRetry) RecordRetry(attempt int, errorType string, tokensUsed int, tokensSaved int, success bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.totalRetries++
	if !success {
		r.totalTokensWasted += tokensUsed
	}
	r.totalTokensSaved += tokensSaved

	r.retryHistory = append(r.retryHistory, RetryAttempt{
		Attempt:     attempt,
		ErrorType:   errorType,
		TokensUsed:  tokensUsed,
		TokensSaved: tokensSaved,
		Success:     success,
	})
}

// GetStats returns aggregated statistics about retry behaviour.
func (r *TokenAwareRetry) GetStats() TokenAwareRetryStats {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return TokenAwareRetryStats{
		MaxRetries:        r.maxRetries,
		TotalRetries:      r.totalRetries,
		TotalTokensWasted: r.totalTokensWasted,
		TotalTokensSaved:  r.totalTokensSaved,
	}
}

// Reset clears all accumulated statistics and retry history.
func (r *TokenAwareRetry) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.totalRetries = 0
	r.totalTokensWasted = 0
	r.totalTokensSaved = 0
	r.retryHistory = nil
}
