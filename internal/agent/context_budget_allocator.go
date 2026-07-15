package agent

import "sync"

// ── OPT-213: ContextBudgetAllocator (上下文预算分配器) ──
// 将上下文token预算分配给不同部分（如系统提示、工具描述、
// 历史消息、响应空间等）。支持按section分配预算，确保总分配
// 不超过totalBudget，并支持按比例重新平衡分配。
//
// 核心能力：
//   - Allocate: 为指定section分配token预算，超额时拒绝分配
//   - Rebalance: 按当前分配比例重新分配总预算
//   - GetAllocation: 查询指定section的当前分配
//   - GetTotalAllocated: 查询所有section已分配的token总数

// ContextBudgetAllocator 上下文预算分配器。
type ContextBudgetAllocator struct {
	mu             sync.RWMutex
	budget         map[string]int // section → tokens
	totalBudget    int
	allocatedCount int
	rebalanceCount int
}

// NewContextBudgetAllocator 创建一个新的上下文预算分配器实例。
// totalBudget 指定可分配的总token预算。
func NewContextBudgetAllocator(totalBudget int) *ContextBudgetAllocator {
	return &ContextBudgetAllocator{
		budget:      make(map[string]int),
		totalBudget: totalBudget,
	}
}

// Allocate 为指定section分配token预算。
// 若分配后总分配超过totalBudget则返回false，分配不生效。
// 若section已存在则覆盖其分配值。
func (a *ContextBudgetAllocator) Allocate(section string, tokens int) bool {
	a.mu.Lock()
	defer a.mu.Unlock()

	currentTotal := cbaSumValues(a.budget)
	existing := a.budget[section]
	// 新的总额 = 当前总额 - 已有分配 + 新分配
	newTotal := currentTotal - existing + tokens
	if newTotal > a.totalBudget {
		return false
	}
	a.budget[section] = tokens
	a.allocatedCount++
	return true
}

// GetAllocation 获取指定section当前分配的token数。
// 若section未分配则返回0。
func (a *ContextBudgetAllocator) GetAllocation(section string) int {
	a.mu.RLock()
	defer a.mu.RUnlock()

	return a.budget[section]
}

// Rebalance 按比例重新平衡所有section的分配。
// 根据各section当前分配占总分配的比例，将totalBudget重新分配。
// 若当前无分配则按section数均分。
func (a *ContextBudgetAllocator) Rebalance() {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.rebalanceCount++

	if len(a.budget) == 0 {
		return
	}

	totalAllocated := cbaSumValues(a.budget)
	if totalAllocated == 0 {
		// 当前无分配，均分总预算
		perSection := a.totalBudget / len(a.budget)
		for k := range a.budget {
			a.budget[k] = perSection
		}
		return
	}

	// 按当前分配比例重新分配总预算
	allocated := 0
	for k, v := range a.budget {
		share := int(float64(v) / float64(totalAllocated) * float64(a.totalBudget))
		a.budget[k] = share
		allocated += share
	}

	// 将余数分配给任意一个section，保证总和等于totalBudget
	remainder := a.totalBudget - allocated
	if remainder != 0 {
		for k := range a.budget {
			a.budget[k] += remainder
			break
		}
	}
}

// GetTotalAllocated 返回当前所有section已分配的token总数。
func (a *ContextBudgetAllocator) GetTotalAllocated() int {
	a.mu.RLock()
	defer a.mu.RUnlock()

	return cbaSumValues(a.budget)
}

// GetStats 返回分配器的统计信息。
func (a *ContextBudgetAllocator) GetStats() map[string]interface{} {
	a.mu.RLock()
	defer a.mu.RUnlock()

	return map[string]interface{}{
		"totalBudget":    a.totalBudget,
		"sectionCount":   len(a.budget),
		"totalAllocated": cbaSumValues(a.budget),
		"allocatedCount": a.allocatedCount,
		"rebalanceCount": a.rebalanceCount,
	}
}

// Reset 重置分配器为初始状态。
func (a *ContextBudgetAllocator) Reset() {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.budget = make(map[string]int)
	a.allocatedCount = 0
	a.rebalanceCount = 0
}

// cbaSumValues 计算map中所有int值的总和。
func cbaSumValues(m map[string]int) int {
	total := 0
	for _, v := range m {
		total += v
	}
	return total
}
