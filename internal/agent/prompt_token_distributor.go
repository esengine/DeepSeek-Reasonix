package agent

import "sync"

// ── OPT-200: PromptTokenDistributor (提示Token分配器) ──
// 将提示的 token 预算分配给不同的组件，支持分配、重新平衡和查询操作。
// Allocate 在不超过总预算的前提下为组件分配 token；Rebalance 按比例
// 缩放所有组件的分配以适应 totalBudget，防止预算超支或利用不足。

// PromptTokenDistributor 提示Token分配器
type PromptTokenDistributor struct {
	mu               sync.RWMutex
	allocations      map[string]int // component -> tokens
	totalBudget      int
	distributedCount int
	rebalancedCount  int
}

// NewPromptTokenDistributor 创建一个新的提示Token分配器。
// totalBudget 为可分配的 token 总预算。
func NewPromptTokenDistributor(totalBudget int) *PromptTokenDistributor {
	return &PromptTokenDistributor{
		allocations: make(map[string]int),
		totalBudget: totalBudget,
	}
}

// Allocate 为指定组件分配 token 预算。
// 若分配后总分配量超过 totalBudget 则拒绝分配，返回 false。
// 若该组件已有分配，则更新为新值（更新时不重复计入 distributedCount）。
// 成功分配时递增 distributedCount 并返回 true。
func (p *PromptTokenDistributor) Allocate(component string, tokens int) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	currentTotal := ptdSumAllocations(p.allocations)

	// 减去该组件已有的分配
	if existing, ok := p.allocations[component]; ok {
		currentTotal -= existing
	}

	if currentTotal+tokens > p.totalBudget {
		return false
	}

	isNew := true
	if _, ok := p.allocations[component]; ok {
		isNew = false
	}

	p.allocations[component] = tokens
	if isNew {
		p.distributedCount++
	}
	return true
}

// Rebalance 重新平衡所有组件的分配。
// 按比例缩放所有分配以使总和等于 totalBudget。
// 若无分配或总和为 0 则不做操作。递增 rebalancedCount。
func (p *PromptTokenDistributor) Rebalance() {
	p.mu.Lock()
	defer p.mu.Unlock()

	total := ptdSumAllocations(p.allocations)
	if total == 0 || len(p.allocations) == 0 {
		return
	}

	// 若总和已等于预算则无需调整
	if total == p.totalBudget {
		return
	}

	// 按比例缩放
	scale := float64(p.totalBudget) / float64(total)
	for component, tokens := range p.allocations {
		p.allocations[component] = int(float64(tokens) * scale)
	}

	p.rebalancedCount++
}

// GetAllocation 获取指定组件的 token 分配量。
// 若组件不存在返回 0。
func (p *PromptTokenDistributor) GetAllocation(component string) int {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if tokens, ok := p.allocations[component]; ok {
		return tokens
	}
	return 0
}

// GetTotalAllocated 获取所有组件已分配的 token 总和。
func (p *PromptTokenDistributor) GetTotalAllocated() int {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return ptdSumAllocations(p.allocations)
}

// GetStats 返回分配器的统计信息。
// 包含: totalBudget, componentCount, totalAllocated, distributedCount, rebalancedCount。
func (p *PromptTokenDistributor) GetStats() map[string]interface{} {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return map[string]interface{}{
		"totalBudget":      p.totalBudget,
		"componentCount":   len(p.allocations),
		"totalAllocated":   ptdSumAllocations(p.allocations),
		"distributedCount": p.distributedCount,
		"rebalancedCount":  p.rebalancedCount,
	}
}

// Reset 重置分配器，清空所有分配与统计信息。
func (p *PromptTokenDistributor) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.allocations = make(map[string]int)
	p.distributedCount = 0
	p.rebalancedCount = 0
}

// ---------------------------------------------------------------------------
// 辅助函数（以 ptd 为前缀，避免命名冲突）
// ---------------------------------------------------------------------------

// ptdSumAllocations 计算所有分配的 token 总和。
func ptdSumAllocations(allocations map[string]int) int {
	total := 0
	for _, tokens := range allocations {
		total += tokens
	}
	return total
}
