package agent

import "sync"

// ── OPT-154: WeightedBudgetAllocator (加权预算分配器) ──
// 在竞争消费者间动态分配 token 预算。每个消费者注册时指定权重，
// Allocate 按权重比例分配总预算。支持通过 Adjust 动态调整某个消费者
// 的分配额度（从其他消费者按比例扣除或增加），适应运行时优先级变化。
//
// 原理：LLM 上下文窗口的 token 预算需要在多个消费者（如系统提示、
// 工具描述、历史消息、响应空间等）间分配。不同场景下各消费者的
// 优先级不同，通过权重机制实现按比例分配，并支持运行时动态调整：
//   - Allocate: 按权重比例将总预算分配给所有已注册消费者
//   - Adjust: 增减某个消费者的分配，差额从其他消费者按比例分摊
//
// 效果：在有限预算下公平且灵活地分配 token 资源，支持动态优先级
// 调整，避免某些消费者过度占用预算导致其他消费者饥饿。

// WeightedBudgetAllocator 加权预算分配器
type WeightedBudgetAllocator struct {
	mu              sync.RWMutex
	totalBudget     int
	allocations     map[string]int     // consumer -> allocated tokens
	consumerWeights map[string]float64 // consumer -> weight
	adjustments     int
}

// NewWeightedBudgetAllocator 创建加权预算分配器。
// totalBudget 指定可分配的总 token 预算。
func NewWeightedBudgetAllocator(totalBudget int) *WeightedBudgetAllocator {
	return &WeightedBudgetAllocator{
		totalBudget:     totalBudget,
		allocations:     make(map[string]int),
		consumerWeights: make(map[string]float64),
	}
}

// RegisterConsumer 注册一个消费者及其权重。
// 权重用于在 Allocate 时按比例分配预算。若消费者已存在则更新其权重。
func (a *WeightedBudgetAllocator) RegisterConsumer(name string, weight float64) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.consumerWeights[name] = weight
}

// Allocate 按权重比例分配预算，返回每个消费者分配的 token 数。
// 分配结果同时更新内部 allocations 记录。返回的 map 为副本，外部修改不影响内部状态。
func (a *WeightedBudgetAllocator) Allocate() map[string]int {
	a.mu.Lock()
	defer a.mu.Unlock()

	result := tbaAllocateByWeight(a.totalBudget, a.consumerWeights)

	// 更新内部记录
	a.allocations = make(map[string]int, len(result))
	for k, v := range result {
		a.allocations[k] = v
	}

	return result
}

// Adjust 调整特定消费者的分配额度。
// delta > 0 时增加该消费者的分配，从其他消费者按比例扣除；
// delta < 0 时减少该消费者的分配，按比例增加给其他消费者。
// 若消费者未注册则不做任何操作。每次调用递增 adjustments 计数。
func (a *WeightedBudgetAllocator) Adjust(name string, delta int) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.adjustments++

	current, ok := a.allocations[name]
	if !ok {
		return
	}

	if delta == 0 {
		return
	}

	// 收集其他消费者及其当前分配总额
	var others []string
	otherTotal := 0
	for n := range a.allocations {
		if n != name {
			others = append(others, n)
			otherTotal += a.allocations[n]
		}
	}

	if delta > 0 {
		// 增加 name 的分配，从其他消费者按比例扣除
		a.allocations[name] = current + delta

		if len(others) > 0 && otherTotal > 0 {
			for _, n := range others {
				original := a.allocations[n]
				proportion := float64(original) / float64(otherTotal)
				reduction := int(float64(delta) * proportion)
				a.allocations[n] = original - reduction
				if a.allocations[n] < 0 {
					a.allocations[n] = 0
				}
			}
		}
	} else {
		// 减少 name 的分配，按比例增加给其他消费者
		absDelta := -delta
		newAlloc := current - absDelta
		if newAlloc < 0 {
			newAlloc = 0
		}
		actualDecrease := current - newAlloc
		a.allocations[name] = newAlloc

		if len(others) > 0 && actualDecrease > 0 {
			if otherTotal > 0 {
				for _, n := range others {
					original := a.allocations[n]
					proportion := float64(original) / float64(otherTotal)
					increase := int(float64(actualDecrease) * proportion)
					a.allocations[n] = original + increase
				}
			} else {
				// 其他消费者当前分配为 0 时，均分
				perConsumer := actualDecrease / len(others)
				for _, n := range others {
					a.allocations[n] += perConsumer
				}
			}
		}
	}
}

// GetAllocation 获取指定消费者当前分配的 token 数。
// 若消费者未注册或未分配，返回 0。
func (a *WeightedBudgetAllocator) GetAllocation(name string) int {
	a.mu.RLock()
	defer a.mu.RUnlock()

	return a.allocations[name]
}

// GetStats 返回分配器的统计信息，包括 totalBudget、consumerCount、adjustments、allocations。
func (a *WeightedBudgetAllocator) GetStats() map[string]interface{} {
	a.mu.RLock()
	defer a.mu.RUnlock()

	// 复制 allocations map 避免外部修改
	allocationsCopy := make(map[string]int, len(a.allocations))
	for k, v := range a.allocations {
		allocationsCopy[k] = v
	}

	return map[string]interface{}{
		"totalBudget":   a.totalBudget,
		"consumerCount": len(a.consumerWeights),
		"adjustments":   a.adjustments,
		"allocations":   allocationsCopy,
	}
}

// Reset 重置分配器，清除所有消费者注册与分配记录。
func (a *WeightedBudgetAllocator) Reset() {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.allocations = make(map[string]int)
	a.consumerWeights = make(map[string]float64)
	a.adjustments = 0
}

// ---------------------------------------------------------------------------
// 辅助函数（以 tba 为前缀，避免命名冲突）
// ---------------------------------------------------------------------------

// tbaAllocateByWeight 按权重比例分配总预算。
// 返回每个消费者分配的 token 数。由于整除可能产生余数，
// 余数分配给第一个消费者以保证总和等于 totalBudget。
func tbaAllocateByWeight(totalBudget int, weights map[string]float64) map[string]int {
	result := make(map[string]int)

	totalWeight := 0.0
	for _, w := range weights {
		totalWeight += w
	}

	if totalWeight <= 0 {
		return result
	}

	allocated := 0
	// 第一轮：按比例分配
	for name, w := range weights {
		share := int(float64(totalBudget) * w / totalWeight)
		result[name] = share
		allocated += share
	}

	// 分配余数给第一个消费者，保证总和等于 totalBudget
	remainder := totalBudget - allocated
	if remainder != 0 {
		for name := range result {
			result[name] += remainder
			break
		}
	}

	return result
}
