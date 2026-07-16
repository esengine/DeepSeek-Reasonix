package agent

import "sync"

// ── OPT-251: TokenAwareThrottleController (Token感知节流控制器 / Token-Aware Throttle Controller) ──
// 基于令牌桶算法的节流控制器。令牌以固定速率补充，桶容量限制突发上限；
// 请求通过时消耗令牌，令牌不足时拒绝请求，从而平滑 token 消耗速率。
//
// 原理：令牌桶以固定速率补充令牌，桶容量（burst）限制瞬时突发流量。
// Allow 消耗一个令牌，Refill 手动补充令牌（不超过桶容量）。
//
// 效果：平滑 token 消耗，统计允许通过和被节流的次数，
// 为流量管理提供数据支撑。

// TokenAwareThrottleController Token感知节流控制器
type TokenAwareThrottleController struct {
	mu             sync.RWMutex
	rate           int // 令牌补充速率
	burst          int // 桶容量（突发上限）
	tokens         int // 当前可用令牌数
	throttledCount int // 被节流的次数
	passedCount    int // 允许通过的次数
}

// NewTokenAwareThrottleController 创建 Token 感知节流控制器。
// rate 指定令牌补充速率，burst 指定桶容量。
// 若 rate <= 0 则默认 1000，若 burst <= 0 则默认 100。
// 初始时令牌桶为满状态。
func NewTokenAwareThrottleController(rate int, burst int) *TokenAwareThrottleController {
	if rate <= 0 {
		rate = 1000
	}
	if burst <= 0 {
		burst = 100
	}
	return &TokenAwareThrottleController{
		rate:   rate,
		burst:  burst,
		tokens: burst, // 初始满桶
	}
}

// Allow 检查是否允许通过（令牌桶算法）。
// 若桶中令牌充足则消耗一个令牌、递增 passedCount 并返回 true；
// 否则递增 throttledCount 并返回 false。
func (t *TokenAwareThrottleController) Allow() bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.tokens > 0 {
		t.tokens--
		t.passedCount++
		return true
	}
	t.throttledCount++
	return false
}

// Refill 补充令牌，补充量不超过桶容量。
func (t *TokenAwareThrottleController) Refill(n int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if n <= 0 {
		return
	}
	t.tokens = tatcMin(t.tokens+n, t.burst)
}

// GetAvailableTokens 获取当前可用令牌数。
func (t *TokenAwareThrottleController) GetAvailableTokens() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.tokens
}

// GetStats 返回节流控制器的统计信息。
// 包含 rate、burst、availableTokens、throttledCount 和 passedCount。
func (t *TokenAwareThrottleController) GetStats() map[string]interface{} {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return map[string]interface{}{
		"rate":            t.rate,
		"burst":           t.burst,
		"availableTokens": t.tokens,
		"throttledCount":  t.throttledCount,
		"passedCount":     t.passedCount,
	}
}

// Reset 重置节流控制器的令牌和计数（保留 rate 和 burst 配置）。
func (t *TokenAwareThrottleController) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.tokens = t.burst
	t.throttledCount = 0
	t.passedCount = 0
}

// tatcMin 返回两个整数中的较小值。
func tatcMin(a, b int) int {
	if a < b {
		return a
	}
	return b
}
