package agent

import "sync"

// ── OPT-259: TokenAwareResourceArbiter (Token 感知资源仲裁器) ──
// 在多个请求者之间仲裁有限 Token 资源的分配与释放，确保总分配不超过上限。

// TokenAwareResourceArbiter Token 感知资源仲裁器。
type TokenAwareResourceArbiter struct {
	mu                sync.RWMutex
	allocations       map[string]int // requester → 已分配资源量
	totalResource     int            // 资源总量上限
	allocatedResource int            // 已分配资源量
	arbitratedCount   int            // 累计仲裁次数（含批准与拒绝）
	deniedCount       int            // 累计拒绝次数
}

// NewTokenAwareResourceArbiter 创建一个新的 Token 感知资源仲裁器。
// totalResource 指定资源总量上限，< 0 时视为 0。
func NewTokenAwareResourceArbiter(totalResource int) *TokenAwareResourceArbiter {
	if totalResource < 0 {
		totalResource = 0
	}
	return &TokenAwareResourceArbiter{
		allocations:       make(map[string]int),
		totalResource:     totalResource,
		allocatedResource: 0,
		arbitratedCount:   0,
		deniedCount:       0,
	}
}

// Request 为 requester 请求 amount 数量的资源。
// 若资源充足则批准分配，返回 true；否则拒绝并返回 false。
// amount < 0 一律拒绝。
func (a *TokenAwareResourceArbiter) Request(requester string, amount int) bool {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.arbitratedCount++
	if amount < 0 {
		a.deniedCount++
		return false
	}
	if a.allocatedResource+amount > a.totalResource {
		a.deniedCount++
		return false
	}
	a.allocations[requester] += amount
	a.allocatedResource += amount
	return true
}

// Release 释放 requester 之前分配的 amount 数量资源。
// 释放量超过已分配量时按已分配量钳制；amount < 0 时忽略。
func (a *TokenAwareResourceArbiter) Release(requester string, amount int) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if amount < 0 {
		return
	}
	current := a.allocations[requester]
	if amount > current {
		amount = current
	}
	a.allocations[requester] -= amount
	a.allocatedResource -= amount
	if a.allocations[requester] == 0 {
		delete(a.allocations, requester)
	}
}

// GetAllocation 返回 requester 当前已分配的资源量。
func (a *TokenAwareResourceArbiter) GetAllocation(requester string) int {
	a.mu.RLock()
	defer a.mu.RUnlock()

	return a.allocations[requester]
}

// GetAvailableResource 返回当前可用资源量。
func (a *TokenAwareResourceArbiter) GetAvailableResource() int {
	a.mu.RLock()
	defer a.mu.RUnlock()

	return a.totalResource - a.allocatedResource
}

// GetStats 返回仲裁器的统计信息。
// 包含: totalResource, allocatedResource, availableResource, requesterCount, arbitratedCount, deniedCount。
func (a *TokenAwareResourceArbiter) GetStats() map[string]interface{} {
	a.mu.RLock()
	defer a.mu.RUnlock()

	return map[string]interface{}{
		"totalResource":     a.totalResource,
		"allocatedResource": a.allocatedResource,
		"availableResource": a.totalResource - a.allocatedResource,
		"requesterCount":    taraCountAllocated(a.allocations),
		"arbitratedCount":   a.arbitratedCount,
		"deniedCount":       a.deniedCount,
	}
}

// Reset 重置仲裁器，清空分配与计数，保留 totalResource 配置。
func (a *TokenAwareResourceArbiter) Reset() {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.allocations = make(map[string]int)
	a.allocatedResource = 0
	a.arbitratedCount = 0
	a.deniedCount = 0
}

// ---------------------------------------------------------------------------
// 辅助函数（以 tara 为前缀，避免命名冲突）
// ---------------------------------------------------------------------------

// taraCountAllocated 返回当前有活跃分配的请求者数量。
func taraCountAllocated(allocations map[string]int) int {
	return len(allocations)
}
