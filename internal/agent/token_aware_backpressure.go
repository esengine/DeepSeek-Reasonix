package agent
import "sync"

// ── OPT-201: TokenAwareBackpressure (Token感知背压控制器 / Token-Aware Backpressure Controller) ──
// 在 token 消耗过快时施加背压，限制 token 流量以保护系统。
// 通过追踪当前 token 消耗速率，当速率超过阈值时激活背压，
// 限制通过的 token 数量，避免突发流量导致系统过载。
//
// 原理：类似水压控制系统，当管道内流量过大时自动减少通过量。
// 背压激活后，ApplyBackpressure 会按剩余预算截断请求的 token 数，
// 使实际通过的 token 数不超过当前允许的速率上限。
//
// 效果：平滑 token 消耗速率，防止过载，
// 统计背压激活次数和被限流的 token 总数，为流量管理提供数据支撑。

// TokenAwareBackpressure Token感知背压控制器
type TokenAwareBackpressure struct {
	mu                 sync.RWMutex
	maxRate            int  // 最大允许速率
	currentRate        int  // 当前速率
	backpressureActive bool // 背压是否激活
	activations        int  // 背压激活次数
	totalThrottled     int  // 被限流的总 token 数
}

// NewTokenAwareBackpressure 创建 Token 感知背压控制器。
// maxRate 指定最大允许的 token 速率，若 <= 0 则默认 10000。
func NewTokenAwareBackpressure(maxRate int) *TokenAwareBackpressure {
	if maxRate <= 0 {
		maxRate = 10000
	}
	return &TokenAwareBackpressure{
		maxRate: maxRate,
	}
}

// CheckRate 检查当前速率是否在允许范围内。
// 若当前速率加上请求的 token 数不超过 maxRate，则返回 true；否则返回 false。
func (b *TokenAwareBackpressure) CheckRate(tokens int) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.currentRate+tokens <= b.maxRate
}

// ApplyBackpressure 施加背压，返回实际允许通过的 token 数。
// 若请求的 token 数导致速率超限，则激活背压并按剩余预算截断；
// 被截断的 token 数计入 totalThrottled。
func (b *TokenAwareBackpressure) ApplyBackpressure(tokens int) int {
	b.mu.Lock()
	defer b.mu.Unlock()

	allowed := tokens
	if b.currentRate+tokens > b.maxRate {
		// 激活背压
		if !b.backpressureActive {
			b.backpressureActive = true
			b.activations++
		}
		// 计算允许通过的 token 数：剩余预算与请求数取较小值
		remaining := b.maxRate - b.currentRate
		if remaining < 0 {
			remaining = 0
		}
		allowed = tabpMinInt(tokens, remaining)
		b.totalThrottled += tokens - allowed
	}
	b.currentRate += allowed
	return allowed
}

// ReleasePressure 释放背压，将当前速率归零并关闭背压状态。
func (b *TokenAwareBackpressure) ReleasePressure() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.currentRate = 0
	b.backpressureActive = false
}

// IsBackpressureActive 返回背压是否处于激活状态。
func (b *TokenAwareBackpressure) IsBackpressureActive() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.backpressureActive
}

// GetStats 返回背压控制器的统计信息。
// 包含 maxRate、currentRate、backpressureActive、activations 和 totalThrottled。
func (b *TokenAwareBackpressure) GetStats() map[string]interface{} {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return map[string]interface{}{
		"maxRate":            b.maxRate,
		"currentRate":        b.currentRate,
		"backpressureActive": b.backpressureActive,
		"activations":        b.activations,
		"totalThrottled":     b.totalThrottled,
	}
}

// Reset 重置背压控制器的速率和统计信息（不重置 maxRate）。
func (b *TokenAwareBackpressure) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.currentRate = 0
	b.backpressureActive = false
	b.activations = 0
	b.totalThrottled = 0
}

// tabpMinInt 返回两个整数中的较小值。
func tabpMinInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
