package agent

import "sync"

// ── OPT-159: TokenAwareThrottler (Token 感知限流器 / Token-Aware Throttler) ──
// 基于 token 消耗速率进行限流。在窗口内累计 token 消耗量，当消耗量超过
// 速率限制时拒绝新的请求，防止 token 消耗过快导致系统过载。
//
// 原理：LLM API 通常按 token 计费且有速率限制。通过追踪每个窗口内的
// token 消耗量，在接近或达到限制时进行限流，避免触发 API 限流或超额消耗。
//
// 效果：平滑 token 消耗速率，避免突发流量导致 API 限流，
// 统计限流次数和允许次数，计算限流率，为流量管理提供数据支撑。

// TokenAwareThrottler Token 感知限流器
type TokenAwareThrottler struct {
	mu             sync.RWMutex
	rateLimit      int // 窗口内最大 token 消耗量
	windowSize     int // 窗口大小
	currentWindow  int // 当前窗口编号
	tokensInWindow int // 当前窗口已消耗的 token 数
	throttledCount int // 被限流的次数
	allowedCount   int // 被允许的次数
}

// NewTokenAwareThrottler 创建 Token 感知限流器。
// rateLimit 指定窗口内最大 token 消耗量，windowSize 指定窗口大小。
// 若 rateLimit <= 0 则默认 100000，若 windowSize <= 0 则默认 1。
func NewTokenAwareThrottler(rateLimit int, windowSize int) *TokenAwareThrottler {
	if rateLimit <= 0 {
		rateLimit = 100000
	}
	if windowSize <= 0 {
		windowSize = 1
	}
	return &TokenAwareThrottler{
		rateLimit:  rateLimit,
		windowSize: windowSize,
	}
}

// Allow 检查是否允许消耗指定数量的 token。
// 若当前窗口剩余预算足够，则更新窗口计数并返回 true；否则递增限流计数并返回 false。
// tokens 为请求消耗的 token 数。
func (t *TokenAwareThrottler) Allow(tokens int) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.tokensInWindow+tokens <= t.rateLimit {
		t.tokensInWindow += tokens
		t.allowedCount++
		return true
	}
	t.throttledCount++
	return false
}

// GetRemainingBudget 获取当前窗口的剩余 token 预算。
// 若已超限则返回 0。
func (t *TokenAwareThrottler) GetRemainingBudget() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	remaining := t.rateLimit - t.tokensInWindow
	if remaining < 0 {
		return 0
	}
	return remaining
}

// GetThrottleRate 获取限流率，即被限流次数占总请求次数的比例。
// 若总请求次数为 0 则返回 0。
func (t *TokenAwareThrottler) GetThrottleRate() float64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return tatComputeThrottleRate(t.throttledCount, t.allowedCount)
}

// GetStats 返回限流器的统计信息。
// 包含 rateLimit、windowSize、tokensInWindow、throttledCount、allowedCount 和 throttleRate。
func (t *TokenAwareThrottler) GetStats() map[string]interface{} {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return map[string]interface{}{
		"rateLimit":      t.rateLimit,
		"windowSize":     t.windowSize,
		"tokensInWindow": t.tokensInWindow,
		"throttledCount": t.throttledCount,
		"allowedCount":   t.allowedCount,
		"throttleRate":   tatComputeThrottleRate(t.throttledCount, t.allowedCount),
	}
}

// Reset 重置限流器的所有计数和窗口状态。
func (t *TokenAwareThrottler) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.currentWindow = 0
	t.tokensInWindow = 0
	t.throttledCount = 0
	t.allowedCount = 0
}

// tatComputeThrottleRate 计算限流率。
// 限流率 = 被限流次数 / (被限流次数 + 允许次数)。
// 若总次数为 0 则返回 0。
func tatComputeThrottleRate(throttledCount, allowedCount int) float64 {
	total := throttledCount + allowedCount
	if total == 0 {
		return 0
	}
	return float64(throttledCount) / float64(total)
}
