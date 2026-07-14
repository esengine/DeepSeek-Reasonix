package agent

import "sync"

// ── OPT-92: ContextBudgetNegotiator (上下文预算协商器) ──
// 在多个竞争组件（系统提示词、工具、历史、响应）之间协商 token
// 预算分配。当各组件请求之和超出可用预算时，按优先级进行削减：
// System 优先，其次 Tools，再次 Response，History 获得剩余部分。
// 始终预留 5% 作为安全余量。
//
// 原理：上下文窗口有限，组件之间对 token 的需求常存在竞争。
// 与其简单按比例分配，不如按重要性优先保障关键组件，让可压缩
// 的 History 承担削减压力，从而在有限预算下最大化关键信息的保留。
//
// 效果：在预算紧张时避免关键组件（系统提示、工具、响应空间）
// 被挤占，将削减集中到可压缩的历史记录上。

// BudgetAllocationV2 协商后的 token 预算分配方案（V2）
type BudgetAllocationV2 struct {
	System   int
	Tools    int
	History  int
	Response int
	Reserved int
	Total    int
}

// BudgetNegotiatorStats 预算协商器统计信息
type BudgetNegotiatorStats struct {
	TotalNegotiations int // 协商总次数
	TotalCompromises  int // 发生妥协（请求超出可用预算）的次数
}

// 协商保留比例常量
const negotiatorReservePct = 5 // 安全余量占比 5%

// ContextBudgetNegotiator 上下文预算协商器
// 在竞争组件之间协商 token 预算分配，按优先级保障关键组件。
type ContextBudgetNegotiator struct {
	mu                sync.RWMutex
	totalNegotiations int
	totalCompromises  int
	lastAllocation    BudgetAllocationV2
}

// NewContextBudgetNegotiator 创建上下文预算协商器
func NewContextBudgetNegotiator() *ContextBudgetNegotiator {
	return &ContextBudgetNegotiator{}
}

// Negotiate 根据各组件请求与总预算协商分配方案。
//
// 分配规则：
//  1. 预留总预算的 5% 作为安全余量。
//  2. 若各组件请求之和不超过可用预算，则全额满足各请求。
//  3. 若超出可用预算（发生妥协），按优先级顺序分配：
//     System -> Tools -> Response 依次满足，History 获得剩余部分。
//
// 返回协商后的分配方案。
func (n *ContextBudgetNegotiator) Negotiate(totalTokens int, systemRequest int, toolsRequest int, historyRequest int, responseRequest int) BudgetAllocationV2 {
	n.mu.Lock()
	defer n.mu.Unlock()

	n.totalNegotiations++

	// 负数请求视为 0
	systemRequest = max(0, systemRequest)
	toolsRequest = max(0, toolsRequest)
	historyRequest = max(0, historyRequest)
	responseRequest = max(0, responseRequest)

	// 预留 5% 安全余量
	reserved := totalTokens * negotiatorReservePct / 100
	if reserved < 0 {
		reserved = 0
	}
	available := totalTokens - reserved
	if available < 0 {
		available = 0
	}

	totalRequested := systemRequest + toolsRequest + historyRequest + responseRequest

	alloc := BudgetAllocationV2{
		Reserved: reserved,
		Total:    totalTokens,
	}

	if totalRequested <= available {
		// 预算充足，全额满足各请求
		alloc.System = systemRequest
		alloc.Tools = toolsRequest
		alloc.History = historyRequest
		alloc.Response = responseRequest
	} else {
		// 预算不足，发生妥协：按优先级顺序分配，History 获得剩余
		n.totalCompromises++
		rem := available

		alloc.System = min(systemRequest, rem)
		rem -= alloc.System
		if rem < 0 {
			rem = 0
		}

		alloc.Tools = min(toolsRequest, rem)
		rem -= alloc.Tools
		if rem < 0 {
			rem = 0
		}

		alloc.Response = min(responseRequest, rem)
		rem -= alloc.Response
		if rem < 0 {
			rem = 0
		}

		alloc.History = rem
	}

	n.lastAllocation = alloc
	return alloc
}

// GetLastAllocation 返回最近一次协商的分配方案
func (n *ContextBudgetNegotiator) GetLastAllocation() BudgetAllocationV2 {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.lastAllocation
}

// GetStats 返回协商器统计信息
func (n *ContextBudgetNegotiator) GetStats() BudgetNegotiatorStats {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return BudgetNegotiatorStats{
		TotalNegotiations: n.totalNegotiations,
		TotalCompromises:  n.totalCompromises,
	}
}

// Reset 重置协商器，清除所有统计与最近分配记录
func (n *ContextBudgetNegotiator) Reset() {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.totalNegotiations = 0
	n.totalCompromises = 0
	n.lastAllocation = BudgetAllocationV2{}
}
