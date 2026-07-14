package agent

import "sync"

// ── OPT-196: TokenAwareAdmissionController (Token感知准入控制器) ──
// 控制请求是否允许进入处理流水线，通过并发数限制保护系统资源。
// 当并发请求数达到上限时拒绝新请求，防止 token 消耗过载。

// TokenAwareAdmissionController Token感知准入控制器
type TokenAwareAdmissionController struct {
	mu                sync.RWMutex
	maxConcurrent     int
	currentConcurrent int
	admittedCount     int
	rejectedCount     int
	totalWaitTime     int
}

// NewTokenAwareAdmissionController 创建一个新的Token感知准入控制器。
// maxConcurrent 为允许的最大并发请求数。
func NewTokenAwareAdmissionController(maxConcurrent int) *TokenAwareAdmissionController {
	return &TokenAwareAdmissionController{
		maxConcurrent: maxConcurrent,
	}
}

// TryAdmit 尝试准入请求。
// 若当前并发数未达到上限，递增 currentConcurrent 和 admittedCount，返回 true。
// 否则递增 rejectedCount，返回 false。
func (t *TokenAwareAdmissionController) TryAdmit() bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.currentConcurrent >= t.maxConcurrent {
		t.rejectedCount++
		return false
	}

	t.currentConcurrent++
	t.admittedCount++
	return true
}

// Release 释放一个并发槽。
// 若 currentConcurrent 大于 0 则递减。
func (t *TokenAwareAdmissionController) Release() {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.currentConcurrent > 0 {
		t.currentConcurrent--
	}
}

// GetConcurrentCount 获取当前并发请求数。
func (t *TokenAwareAdmissionController) GetConcurrentCount() int {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return t.currentConcurrent
}

// GetUtilization 获取当前并发利用率（currentConcurrent / maxConcurrent）。
// 若 maxConcurrent 为 0 则返回 0。
func (t *TokenAwareAdmissionController) GetUtilization() float64 {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return tacComputeUtilization(t.currentConcurrent, t.maxConcurrent)
}

// GetStats 返回控制器的统计信息。
// 包含: maxConcurrent, currentConcurrent, admittedCount, rejectedCount, utilization。
func (t *TokenAwareAdmissionController) GetStats() map[string]interface{} {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return map[string]interface{}{
		"maxConcurrent":     t.maxConcurrent,
		"currentConcurrent": t.currentConcurrent,
		"admittedCount":     t.admittedCount,
		"rejectedCount":     t.rejectedCount,
		"utilization":       tacComputeUtilization(t.currentConcurrent, t.maxConcurrent),
	}
}

// Reset 重置控制器，清空所有计数与并发状态。
func (t *TokenAwareAdmissionController) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.currentConcurrent = 0
	t.admittedCount = 0
	t.rejectedCount = 0
	t.totalWaitTime = 0
}

// ---------------------------------------------------------------------------
// 辅助函数（以 tac 为前缀，避免命名冲突）
// ---------------------------------------------------------------------------

// tacComputeUtilization 计算并发利用率: current / max。
// 若 max <= 0，返回 0。
func tacComputeUtilization(current, max int) float64 {
	if max <= 0 {
		return 0
	}
	return float64(current) / float64(max)
}
