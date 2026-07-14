package agent

import "sync"

// ── OPT-130: TokenBudgetNegotiator (Token 预算协商器) ──
// 在多个消费者之间协商 token 预算分配。当各消费者请求之和超出总预算时，
// 按优先级比例进行分配，高优先级消费者获得更大份额，但不超过其请求量。
//
// 原理：token 预算有限，多个消费者（如系统提示、工具、历史、响应）
// 竞争预算。TokenBudgetNegotiator 按各消费者的优先级权重比例分配，
// 采用水位填充（water-filling）策略：先满足被比例分配后达到请求上限的
// 消费者，再将剩余预算重新分配给未满足的消费者。
//
// 效果：在预算紧张时公平且高效地分配 token，优先保障高优先级消费者。

// BudgetConsumer 表示一个 token 预算消费者。
type BudgetConsumer struct {
	Name      string
	Requested int
	Allocated int
	Priority  int
}

// TokenBudgetNegotiator 在多个消费者间协商 token 预算分配。
type TokenBudgetNegotiator struct {
	mu                sync.RWMutex
	consumers         map[string]*BudgetConsumer
	totalBudget       int
	totalAllocated    int
	totalNegotiations int
}

// NewTokenBudgetNegotiator 创建一个新的 TokenBudgetNegotiator。
// totalBudget 指定可分配的 token 预算总量。
func NewTokenBudgetNegotiator(totalBudget int) *TokenBudgetNegotiator {
	return &TokenBudgetNegotiator{
		consumers:   make(map[string]*BudgetConsumer),
		totalBudget: totalBudget,
	}
}

// RegisterConsumer 注册一个消费者，指定其名称和优先级。
// 若同名消费者已存在，则更新其优先级。
func (n *TokenBudgetNegotiator) RegisterConsumer(name string, priority int) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if c, exists := n.consumers[name]; exists {
		c.Priority = priority
		return
	}
	n.consumers[name] = &BudgetConsumer{
		Name:     name,
		Priority: priority,
	}
}

// RequestBudget 为指定消费者设置请求的预算量。
// 若消费者未注册，则忽略。
func (n *TokenBudgetNegotiator) RequestBudget(name string, amount int) {
	n.mu.Lock()
	defer n.mu.Unlock()
	c, exists := n.consumers[name]
	if !exists {
		return
	}
	if amount < 0 {
		amount = 0
	}
	c.Requested = amount
}

// Negotiate 按优先级比例分配预算，返回每个消费者分配到的量。
// 若各消费者请求之和不超过总预算，则全额满足；否则按优先级比例
// 采用水位填充策略分配，确保不超过各消费者的请求量。
func (n *TokenBudgetNegotiator) Negotiate() map[string]int {
	n.mu.Lock()
	defer n.mu.Unlock()

	n.totalNegotiations++

	result := make(map[string]int)
	if len(n.consumers) == 0 {
		n.totalAllocated = 0
		return result
	}

	// 收集消费者列表
	var list []*BudgetConsumer
	for _, c := range n.consumers {
		list = append(list, c)
	}

	available := n.totalBudget
	if available < 0 {
		available = 0
	}

	allocated := tbnAllocate(list, available)

	totalAlloc := 0
	for _, c := range list {
		c.Allocated = allocated[c.Name]
		result[c.Name] = allocated[c.Name]
		totalAlloc += allocated[c.Name]
	}
	n.totalAllocated = totalAlloc
	return result
}

// GetAllocation 获取指定消费者已分配的预算量。
// 若消费者未注册，返回 0。
func (n *TokenBudgetNegotiator) GetAllocation(name string) int {
	n.mu.RLock()
	defer n.mu.RUnlock()
	if c, exists := n.consumers[name]; exists {
		return c.Allocated
	}
	return 0
}

// GetStats 返回协商器的统计信息。
func (n *TokenBudgetNegotiator) GetStats() map[string]interface{} {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return map[string]interface{}{
		"totalBudget":       n.totalBudget,
		"totalAllocated":    n.totalAllocated,
		"totalNegotiations": n.totalNegotiations,
		"consumerCount":     len(n.consumers),
		"remainingBudget":   n.totalBudget - n.totalAllocated,
	}
}

// Reset 清除所有消费者和统计信息，保留总预算配置。
func (n *TokenBudgetNegotiator) Reset() {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.consumers = make(map[string]*BudgetConsumer)
	n.totalAllocated = 0
	n.totalNegotiations = 0
}

// tbnAllocate 按优先级比例分配预算，采用水位填充策略。
// 每个消费者分配量不超过其请求量，总和不超过 available。
func tbnAllocate(list []*BudgetConsumer, available int) map[string]int {
	result := make(map[string]int)
	if len(list) == 0 || available <= 0 {
		return result
	}

	totalRequested := 0
	totalPriority := 0
	for _, c := range list {
		req := c.Requested
		if req < 0 {
			req = 0
		}
		totalRequested += req
		totalPriority += c.Priority
	}

	// 预算充足，全额满足
	if totalRequested <= available {
		for _, c := range list {
			req := c.Requested
			if req < 0 {
				req = 0
			}
			result[c.Name] = req
		}
		return result
	}

	// 预先处理无请求的消费者
	settled := make(map[string]bool)
	for _, c := range list {
		if c.Requested <= 0 {
			settled[c.Name] = true
			result[c.Name] = 0
		}
	}

	remaining := available

	for len(settled) < len(list) && remaining > 0 {
		// 统计未满足消费者的优先级和数量
		unsettledPriority := 0
		unsettledCount := 0
		for _, c := range list {
			if !settled[c.Name] {
				unsettledPriority += c.Priority
				unsettledCount++
			}
		}
		if unsettledCount == 0 {
			break
		}

		// 若无优先级权重，平均分配
		if unsettledPriority <= 0 {
			per := remaining / unsettledCount
			leftover := remaining - per*unsettledCount
			for _, c := range list {
				if settled[c.Name] {
					continue
				}
				alloc := per
				if leftover > 0 {
					alloc++
					leftover--
				}
				cur := result[c.Name]
				if cur+alloc > c.Requested {
					alloc = c.Requested - cur
				}
				result[c.Name] = cur + alloc
				remaining -= alloc
				if result[c.Name] >= c.Requested {
					settled[c.Name] = true
				}
			}
			break
		}

		// 检查按比例分配是否有消费者达到请求上限
		progress := false
		for _, c := range list {
			if settled[c.Name] {
				continue
			}
			share := remaining * c.Priority / unsettledPriority
			cur := result[c.Name]
			if cur+share >= c.Requested {
				result[c.Name] = c.Requested
				settled[c.Name] = true
				progress = true
			}
		}

		if progress {
			// 重新计算剩余预算
			remaining = available
			for _, v := range result {
				remaining -= v
			}
			if remaining < 0 {
				remaining = 0
			}
		} else {
			// 无人达到上限，按比例分配剩余预算
			for _, c := range list {
				if settled[c.Name] {
					continue
				}
				share := remaining * c.Priority / unsettledPriority
				result[c.Name] += share
			}
			break
		}
	}

	return result
}
