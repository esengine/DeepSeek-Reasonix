package agent
import "sync"

// ── OPT-209: TokenAwareConcurrencyLimiter (Token 感知并发限制器 / Token-Aware Concurrency Limiter) ──
// 基于 token 消耗动态调整并发度。通过追踪活跃并发数和 token 成本，
// 在并发度过高时拒绝新请求，并支持根据负载动态调整最大并发度。
//
// 原理：LLM 调用的 token 成本差异巨大（短问答 vs 长文档生成）。固定并发度
// 可能导致高 token 成本请求堆积时系统过载。通过记录每次请求的 token 成本
// 并计算平均成本，为并发度调整提供依据。
//
// 效果：限制活跃并发数，统计准入/拒绝次数和平均 token 成本，
// 支持动态调整最大并发度，为并发管理提供数据支撑。

// TokenAwareConcurrencyLimiter Token 感知并发限制器
type TokenAwareConcurrencyLimiter struct {
	mu             sync.RWMutex
	maxConcurrent  int // 最大并发度 maximum concurrency
	active         int // 当前活跃并发数 current active count
	totalAdmitted  int // 累计准入次数 total admitted count
	totalRejected  int // 累计拒绝次数 total rejected count
	avgTokenCost   int // 平均 token 成本 average token cost
	totalTokenCost int // 累计 token 成本 total token cost
}

// NewTokenAwareConcurrencyLimiter 创建 Token 感知并发限制器。
// maxConcurrent 指定最大并发度，若 <= 0 则默认 4。
func NewTokenAwareConcurrencyLimiter(maxConcurrent int) *TokenAwareConcurrencyLimiter {
	if maxConcurrent <= 0 {
		maxConcurrent = 4
	}
	return &TokenAwareConcurrencyLimiter{
		maxConcurrent: maxConcurrent,
	}
}

// Acquire 尝试获取一个并发槽。
// 若当前活跃数 < 最大并发度则准入（记录 token 成本），返回 true；
// 否则拒绝，返回 false。
// tokenCost 为本次请求的预估 token 成本。
func (t *TokenAwareConcurrencyLimiter) Acquire(tokenCost int) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.active >= t.maxConcurrent {
		t.totalRejected++
		return false
	}

	t.active++
	t.totalAdmitted++
	t.totalTokenCost += tokenCost
	t.avgTokenCost = taclComputeAvg(t.totalTokenCost, t.totalAdmitted)
	return true
}

// Release 释放一个并发槽。
// 活跃数不会降至 0 以下。
func (t *TokenAwareConcurrencyLimiter) Release() {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.active > 0 {
		t.active--
	}
}

// GetActiveCount 获取当前活跃并发数。
func (t *TokenAwareConcurrencyLimiter) GetActiveCount() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.active
}

// AdjustConcurrency 动态调整最大并发度。
// target 为目标并发度，若 <= 0 则设为 1。
func (t *TokenAwareConcurrencyLimiter) AdjustConcurrency(target int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if target <= 0 {
		target = 1
	}
	t.maxConcurrent = target
}

// GetStats 返回并发限制器的统计信息。
// 包含 maxConcurrent、active、totalAdmitted、totalRejected 和 avgTokenCost。
func (t *TokenAwareConcurrencyLimiter) GetStats() map[string]interface{} {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return map[string]interface{}{
		"maxConcurrent": t.maxConcurrent,
		"active":        t.active,
		"totalAdmitted": t.totalAdmitted,
		"totalRejected": t.totalRejected,
		"avgTokenCost":  t.avgTokenCost,
	}
}

// Reset 重置并发限制器的所有计数（保留 maxConcurrent 配置）。
func (t *TokenAwareConcurrencyLimiter) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.active = 0
	t.totalAdmitted = 0
	t.totalRejected = 0
	t.avgTokenCost = 0
	t.totalTokenCost = 0
}

// taclComputeAvg 计算平均 token 成本。
// 平均值 = 总 token 成本 / 准入次数。若准入次数为 0 则返回 0。
func taclComputeAvg(totalTokenCost, admittedCount int) int {
	if admittedCount == 0 {
		return 0
	}
	return totalTokenCost / admittedCount
}
