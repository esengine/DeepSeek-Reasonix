package agent

import "sync"

// ── OPT-264: TokenAwareAdmissionGatekeeper (Token 感知准入守门员) ──
// 基于剩余 token 容量对进入系统的请求进行准入控制。Admit 时检查
// 当前已准入 token 加上本次请求 token 是否超过容量上限，超过则
// 拒绝并累计拒绝次数；Release 时释放对应 token 预算。
//
// 原理：在突发流量下，若不限流地接纳所有请求，token 预算会被
// 迅速耗尽并引发排队与超时。基于 token 体量而非请求数进行准入，
// 可以更准确地反映真实资源消耗，避免少量大请求拖垮整体。
//
// 效果：保护下游 token 预算不被击穿，平滑突发流量，提升稳定性。

// TokenAwareAdmissionGatekeeper Token 感知准入守门员。
type TokenAwareAdmissionGatekeeper struct {
	mu                sync.RWMutex
	capacity          int
	currentAdmissions int
	admittedCount     int
	rejectedCount     int
	maxWaitTime       int64
}

// NewTokenAwareAdmissionGatekeeper 创建一个新的 Token 感知准入守门员。
// capacity 为允许同时准入的最大 token 容量。
func NewTokenAwareAdmissionGatekeeper(capacity int) *TokenAwareAdmissionGatekeeper {
	return &TokenAwareAdmissionGatekeeper{
		capacity: capacity,
	}
}

// Admit 执行准入检查。
// 若 currentAdmissions + tokens 不超过 capacity，则准入并返回 true；
// 否则拒绝并返回 false。同时累计 admittedCount 或 rejectedCount。
func (g *TokenAwareAdmissionGatekeeper) Admit(tokens int) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if tokens < 0 {
		tokens = 0
	}
	if g.currentAdmissions+tokens <= g.capacity {
		g.currentAdmissions += tokens
		g.admittedCount++
		return true
	}
	g.rejectedCount++
	if int64(tokens) > g.maxWaitTime {
		g.maxWaitTime = int64(tokens)
	}
	return false
}

// Release 释放已准入的 token 预算。
// currentAdmissions 不会降至负数。
func (g *TokenAwareAdmissionGatekeeper) Release(tokens int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.currentAdmissions -= tokens
	if g.currentAdmissions < 0 {
		g.currentAdmissions = 0
	}
}

// GetAdmissionRate 返回准入成功率（admittedCount 占总尝试次数的比例）。
// 若总尝试次数为 0 则返回 0。
func (g *TokenAwareAdmissionGatekeeper) GetAdmissionRate() float64 {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return taagComputeRate(g.admittedCount, g.rejectedCount)
}

// IsFull 返回当前准入是否已达容量上限。
func (g *TokenAwareAdmissionGatekeeper) IsFull() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.currentAdmissions >= g.capacity
}

// GetStats 返回统计信息，包含 capacity、currentAdmissions、admittedCount、
// rejectedCount 和 admissionRate。
func (g *TokenAwareAdmissionGatekeeper) GetStats() map[string]interface{} {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return map[string]interface{}{
		"capacity":          g.capacity,
		"currentAdmissions": g.currentAdmissions,
		"admittedCount":     g.admittedCount,
		"rejectedCount":     g.rejectedCount,
		"admissionRate":     taagComputeRate(g.admittedCount, g.rejectedCount),
	}
}

// Reset 重置守门员状态，清空当前准入与计数，但保留 capacity 配置。
func (g *TokenAwareAdmissionGatekeeper) Reset() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.currentAdmissions = 0
	g.admittedCount = 0
	g.rejectedCount = 0
	g.maxWaitTime = 0
}

// taagComputeRate 计算准入成功率（辅助函数）。
// admitted 为成功次数，rejected 为拒绝次数；若两者之和为 0 则返回 0。
func taagComputeRate(admitted, rejected int) float64 {
	total := admitted + rejected
	if total == 0 {
		return 0
	}
	return float64(admitted) / float64(total)
}
