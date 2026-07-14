package agent

import "sync"

// TokenFlowRegulator (OPT-105) Token 流量调节器。
// 通过预算管理和速率限制来控制 token 消耗，防止过载和资源滥用。
type TokenFlowRegulator struct {
	mu             sync.RWMutex
	budget         int
	consumed       int
	rateLimit      int
	burstAllowance int
	currentBurst   int
	regulatedCount int
	totalRegulated int
	flowHistory    []int
}

// NewTokenFlowRegulator 创建一个新的 TokenFlowRegulator 实例。
// budget 指定 token 总预算，rateLimit 指定速率限制，突发额度设为 rateLimit 的 2 倍。
func NewTokenFlowRegulator(budget int, rateLimit int) *TokenFlowRegulator {
	return &TokenFlowRegulator{
		budget:         budget,
		rateLimit:      rateLimit,
		burstAllowance: rateLimit * 2,
	}
}

// Consume 尝试消费指定数量的 token。
// 如果消费后超过预算或超过速率限制（突发额度），则拒绝消费并返回 false。
// 否则更新消费计数并返回 true。
func (r *TokenFlowRegulator) Consume(tokens int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	// 检查预算
	if r.consumed+tokens > r.budget {
		r.regulatedCount++
		r.totalRegulated++
		return false
	}

	// 检查速率限制（突发额度）
	if r.currentBurst+tokens > r.burstAllowance {
		r.regulatedCount++
		r.totalRegulated++
		return false
	}

	r.consumed += tokens
	r.currentBurst += tokens
	r.flowHistory = append(r.flowHistory, tokens)

	// 达到速率限制时重置突发窗口
	if r.currentBurst >= r.rateLimit {
		r.currentBurst = 0
		r.regulatedCount = 0
	}

	return true
}

// CheckRate 检查当前是否在速率限制范围内。
// 如果当前突发量未超过突发额度，返回 true。
func (r *TokenFlowRegulator) CheckRate() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.currentBurst < r.burstAllowance
}

// AdjustRate 调整速率限制，同时更新突发额度。
func (r *TokenFlowRegulator) AdjustRate(newLimit int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rateLimit = newLimit
	r.burstAllowance = newLimit * 2
}

// GetRemainingBudget 返回剩余的 token 预算。
func (r *TokenFlowRegulator) GetRemainingBudget() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	remaining := r.budget - r.consumed
	if remaining < 0 {
		return 0
	}
	return remaining
}

// GetStats 返回调节器的统计信息。
func (r *TokenFlowRegulator) GetStats() map[string]interface{} {
	r.mu.RLock()
	defer r.mu.RUnlock()
	remaining := r.budget - r.consumed
	if remaining < 0 {
		remaining = 0
	}
	return map[string]interface{}{
		"budget":          r.budget,
		"consumed":        r.consumed,
		"rateLimit":       r.rateLimit,
		"burstAllowance":  r.burstAllowance,
		"regulatedCount":  r.regulatedCount,
		"remainingBudget": remaining,
	}
}

// Reset 重置调节器的运行时状态，但保留 budget、rateLimit 和 burstAllowance 配置。
func (r *TokenFlowRegulator) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.consumed = 0
	r.currentBurst = 0
	r.regulatedCount = 0
	r.totalRegulated = 0
	r.flowHistory = nil
}
