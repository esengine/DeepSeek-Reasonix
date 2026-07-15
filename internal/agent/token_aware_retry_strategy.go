package agent
import "sync"

// ── OPT-219: TokenAwareRetryStrategy (Token感知重试策略器) ──
// 基于 token 成本决定是否重试：仅当尝试次数未超上限且 token 成本处于合理范围时才重试。
// 采用指数退避（backoffBase * 2^attempt）控制重试间隔，累计重试次数与重试消耗的 token 总量。

// TokenAwareRetryStrategy Token感知重试策略器，结合 token 成本与指数退避决定重试策略。
type TokenAwareRetryStrategy struct {
	mu                 sync.RWMutex
	maxRetries         int // 最大重试次数
	retryCount         int // 已重试次数
	totalRetriedTokens int // 重试累计消耗的 token 总量
	backoffBase        int // 退避基数
	lastBackoff        int // 最近一次退避时间
}

// NewTokenAwareRetryStrategy 创建一个新的 Token 感知重试策略器。
// maxRetries 为最大重试次数，backoffBase 为指数退避基数。
func NewTokenAwareRetryStrategy(maxRetries int, backoffBase int) *TokenAwareRetryStrategy {
	return &TokenAwareRetryStrategy{
		maxRetries:  maxRetries,
		backoffBase: backoffBase,
	}
}

// ShouldRetry 判断是否应该重试。
// 当 attempt < maxRetries 且 tokenCost 处于合理范围（>0 且不超过上限）时返回 true。
func (t *TokenAwareRetryStrategy) ShouldRetry(attempt int, tokenCost int) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()

	const reasonableMaxTokenCost = 100000
	if attempt >= t.maxRetries {
		return false
	}
	if tokenCost <= 0 || tokenCost > reasonableMaxTokenCost {
		return false
	}
	return true
}

// GetBackoff 获取指定尝试次数的退避时间（指数退避: backoffBase * 2^attempt）。
// 为避免整数溢出，attempt 被钳制在 [0, 30] 范围内。
func (t *TokenAwareRetryStrategy) GetBackoff(attempt int) int {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return tarsComputeBackoff(t.backoffBase, attempt)
}

// RecordRetry 记录一次重试，递增 retryCount，累加 tokenCost 到 totalRetriedTokens，
// 并更新最近一次退避时间 lastBackoff。
func (t *TokenAwareRetryStrategy) RecordRetry(tokenCost int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.retryCount++
	t.totalRetriedTokens += tokenCost
	t.lastBackoff = tarsComputeBackoff(t.backoffBase, t.retryCount)
}

// GetStats 返回策略器的统计信息。
// 包含: maxRetries, retryCount, totalRetriedTokens, lastBackoff。
func (t *TokenAwareRetryStrategy) GetStats() map[string]interface{} {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return map[string]interface{}{
		"maxRetries":         t.maxRetries,
		"retryCount":         t.retryCount,
		"totalRetriedTokens": t.totalRetriedTokens,
		"lastBackoff":        t.lastBackoff,
	}
}

// Reset 重置策略器，清空重试计数与退避记录，保留 maxRetries 与 backoffBase 配置。
func (t *TokenAwareRetryStrategy) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.retryCount = 0
	t.totalRetriedTokens = 0
	t.lastBackoff = 0
}

// ---------------------------------------------------------------------------
// 辅助函数（以 tars 为前缀，避免命名冲突）
// ---------------------------------------------------------------------------

// tarsComputeBackoff 计算指数退避时间: base * 2^attempt。
// attempt 被钳制在 [0, 30] 以避免整数溢出；base 为负时返回 0。
func tarsComputeBackoff(base int, attempt int) int {
	if base < 0 {
		return 0
	}
	if attempt < 0 {
		attempt = 0
	}
	if attempt > 30 {
		attempt = 30
	}
	return base * (1 << uint(attempt))
}
