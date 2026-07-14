package agent
import "sync"

// ── OPT-204: TokenAwareThrottleV2 (Token感知限流器V2 / Token-Aware Throttle V2) ──
// 结合令牌桶和滑动窗口限流，提供更精细的 token 消耗控制。
// 令牌桶以固定速率补充令牌，消耗时从桶中取令牌；
// 滑动窗口追踪窗口内的使用量，防止短时间内的突发消耗。
//
// 原理：令牌桶算法通过持续补充令牌来控制平均速率，
// 桶容量限制突发流量上限；滑动窗口追踪窗口起止时间和累计使用量。
// 两者结合提供双重保障，既允许合理的突发流量又防止持续过载。
//
// 效果：平滑 token 消耗，统计允许和限流的次数，
// 为流量管理提供数据支撑。

// TokenAwareThrottleV2 Token感知限流器V2
type TokenAwareThrottleV2 struct {
	mu             sync.RWMutex
	bucketCapacity int // 令牌桶容量
	tokensInBucket int // 桶中当前令牌数
	refillRate     int // 令牌补充速率（每时间单位补充的令牌数）
	windowStart    int // 滑动窗口起始时间
	windowUsage    int // 当前窗口已使用量
	allowedCount   int // 允许通过的次数
	throttledCount int // 被限流的次数
}

// NewTokenAwareThrottleV2 创建 Token 感知限流器V2。
// bucketCapacity 指定令牌桶容量，refillRate 指定令牌补充速率。
// 若 bucketCapacity <= 0 则默认 10000，若 refillRate <= 0 则默认 1000。
// 初始时令牌桶为满状态。
func NewTokenAwareThrottleV2(bucketCapacity int, refillRate int) *TokenAwareThrottleV2 {
	if bucketCapacity <= 0 {
		bucketCapacity = 10000
	}
	if refillRate <= 0 {
		refillRate = 1000
	}
	return &TokenAwareThrottleV2{
		bucketCapacity: bucketCapacity,
		tokensInBucket: bucketCapacity, // 初始满桶
		refillRate:     refillRate,
	}
}

// TryConsume 尝试消耗指定数量的令牌。
// 先根据当前时间补充令牌桶，再检查桶中令牌是否充足。
// 若足够则消耗令牌、记录窗口使用量并返回 true；否则递增限流计数并返回 false。
// tokens 为请求消耗的令牌数，currentTime 为当前时间戳。
func (t *TokenAwareThrottleV2) TryConsume(tokens int, currentTime int) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	// 先补充令牌
	t.refillLocked(currentTime)

	if t.tokensInBucket >= tokens {
		t.tokensInBucket -= tokens
		t.windowUsage += tokens
		t.allowedCount++
		return true
	}
	t.throttledCount++
	return false
}

// Refill 根据当前时间补充令牌桶。
// 补充量 = (currentTime - windowStart) * refillRate，但不超过桶容量。
// 若为首次调用则仅初始化窗口起始时间。
func (t *TokenAwareThrottleV2) Refill(currentTime int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.refillLocked(currentTime)
}

// refillLocked 在已持锁的情况下补充令牌桶。
// 根据经过的时间计算补充量，并更新窗口起始时间。
func (t *TokenAwareThrottleV2) refillLocked(currentTime int) {
	if t.windowStart <= 0 {
		t.windowStart = currentTime
		return
	}
	elapsed := currentTime - t.windowStart
	if elapsed <= 0 {
		return
	}
	refill := elapsed * t.refillRate
	t.tokensInBucket = tat2MinInt(t.tokensInBucket+refill, t.bucketCapacity)
	t.windowStart = currentTime
}

// GetBucketLevel 获取令牌桶中当前的令牌数。
func (t *TokenAwareThrottleV2) GetBucketLevel() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.tokensInBucket
}

// GetStats 返回限流器V2的统计信息。
// 包含 bucketCapacity、tokensInBucket、refillRate、allowedCount 和 throttledCount。
func (t *TokenAwareThrottleV2) GetStats() map[string]interface{} {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return map[string]interface{}{
		"bucketCapacity": t.bucketCapacity,
		"tokensInBucket": t.tokensInBucket,
		"refillRate":     t.refillRate,
		"allowedCount":   t.allowedCount,
		"throttledCount": t.throttledCount,
	}
}

// Reset 重置限流器V2的所有状态和计数。
// 令牌桶恢复为满状态，窗口和计数归零。
func (t *TokenAwareThrottleV2) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.tokensInBucket = t.bucketCapacity
	t.windowStart = 0
	t.windowUsage = 0
	t.allowedCount = 0
	t.throttledCount = 0
}

// tat2MinInt 返回两个整数中的较小值。
func tat2MinInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
