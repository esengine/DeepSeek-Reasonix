package agent

import "sync"

// ── OPT-44: TokenBudgetAllocator (上下文窗口预算分配器) ──
// 将上下文窗口的 token 预算分配到不同组件：SystemPrompt、Tools、History、Response。
// 根据对话状态动态调整分配，确保每个组件获得合理预算，
// 同时在历史过长时触发压缩，保证响应空间不被挤占。
//
// 原理：LLM 上下文窗口是固定大小的，需要合理分配给各个组件。
// SystemPrompt 和 Tools 通常是固定的，History 随对话增长，
// Response 需要预留足够空间。通过动态分配：
//   - 当 SystemPrompt+Tools 超过 25% 时发出警告并调整
//   - 当 History 超过 70% 时触发压缩
//   - Response 始终保留至少 10%
//   - 5% 作为安全余量
//
// 效果：避免历史膨胀挤占响应空间，减少因上下文溢出导致的截断，
// 在保证质量的前提下最大化上下文利用率。

// BudgetAllocation 表示各组件的 token 预算分配
type BudgetAllocation struct {
	SystemPrompt int
	Tools        int
	History      int
	Response     int
	Reserved     int
}

// BudgetAllocatorStats 预算分配器统计信息
type BudgetAllocatorStats struct {
	WindowSize          int
	CurrentAllocation   BudgetAllocation
	TotalAllocations    int
	CompactionTriggered int
}

// 分配百分比常量（占上下文窗口的比例）
const (
	allocPctSystemPrompt = 5  // SystemPrompt 默认 5%
	allocPctTools        = 15 // Tools 默认 15%
	allocPctHistory      = 60 // History 默认 60%
	allocPctResponse     = 15 // Response 默认 15%
	allocPctReserved     = 5  // Reserved 安全余量 5%

	allocPctPromptToolsWarning = 25 // SystemPrompt+Tools 警告阈值 25%
	allocPctHistoryCompaction  = 70 // History 压缩触发阈值 70%
	allocPctMinResponse        = 10 // Response 最低保障 10%
)

// TokenBudgetAllocator 上下文窗口预算分配器
// 根据对话状态动态分配 token 预算到各组件。
type TokenBudgetAllocator struct {
	mu                  sync.RWMutex
	windowSize          int
	allocations         BudgetAllocation
	history             int
	totalAllocations    int
	compactionTriggered int
}

// NewTokenBudgetAllocator 创建预算分配器
//
// 默认分配比例：
//   - SystemPrompt: 5%
//   - Tools: 15%
//   - History: 60%
//   - Response: 15%
//   - Reserved: 5%
func NewTokenBudgetAllocator(windowSize int) *TokenBudgetAllocator {
	return &TokenBudgetAllocator{
		windowSize: windowSize,
		allocations: BudgetAllocation{
			SystemPrompt: windowSize * allocPctSystemPrompt / 100,
			Tools:        windowSize * allocPctTools / 100,
			History:      windowSize * allocPctHistory / 100,
			Response:     windowSize * allocPctResponse / 100,
			Reserved:     windowSize * allocPctReserved / 100,
		},
	}
}

// Allocate 根据各组件当前 token 使用量动态调整预算分配
//
// 动态调整规则：
//   a. 若 SystemPrompt + Tools 超过窗口 25%，标记警告并调整分配
//   b. 若 History 超过窗口 70%，减少 History 分配以触发压缩
//   c. 确保 Response 始终至少占窗口 10%
//   d. 保留 5% 作为安全余量
//
// 返回调整后的分配方案。
func (a *TokenBudgetAllocator) Allocate(systemPromptTokens, toolsTokens, historyTokens int) BudgetAllocation {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.totalAllocations++

	// 基于当前分配进行调整
	alloc := a.allocations

	// 预计算阈值
	promptToolsThreshold := a.windowSize * allocPctPromptToolsWarning / 100
	historyThreshold := a.windowSize * allocPctHistoryCompaction / 100
	minResponse := a.windowSize * allocPctMinResponse / 100
	reservedMargin := a.windowSize * allocPctReserved / 100

	promptToolsSum := systemPromptTokens + toolsTokens

	// (a) 若 SystemPrompt + Tools 超过窗口 25%，标记警告并调整
	if promptToolsSum > promptToolsThreshold {
		// 超出合并阈值：按实际使用量分配给 SystemPrompt 和 Tools
		alloc.SystemPrompt = systemPromptTokens
		alloc.Tools = toolsTokens
		// 从 History 中扣除超出部分，但保留 Response 和 Reserved 的最低保障
		remaining := a.windowSize - alloc.SystemPrompt - alloc.Tools - minResponse - reservedMargin
		if remaining > 0 {
			alloc.History = remaining
		} else {
			alloc.History = 0
		}
	} else {
		// 未超出阈值：使用实际值与默认值中较大者
		defaultPrompt := a.windowSize * allocPctSystemPrompt / 100
		defaultTools := a.windowSize * allocPctTools / 100
		alloc.SystemPrompt = max(systemPromptTokens, defaultPrompt)
		alloc.Tools = max(toolsTokens, defaultTools)
	}

	// (b) 若 History 超过窗口 70%，减少 History 分配以触发压缩
	if historyTokens > historyThreshold {
		a.compactionTriggered++
		alloc.History = historyThreshold
	}

	// (c) 确保 Response 始终至少占窗口 10%
	if alloc.Response < minResponse {
		alloc.Response = minResponse
	}

	// (d) 保留 5% 作为安全余量
	alloc.Reserved = reservedMargin

	// 确保总分配不超过窗口大小
	total := alloc.SystemPrompt + alloc.Tools + alloc.History + alloc.Response + alloc.Reserved
	if total > a.windowSize {
		overflow := total - a.windowSize
		alloc.History -= overflow
		if alloc.History < 0 {
			alloc.History = 0
		}
	}

	a.allocations = alloc
	a.history = historyTokens

	return alloc
}

// GetOptimalHistoryLimit 返回触发压缩前 History 的最大 token 数（窗口的 70%）
func (a *TokenBudgetAllocator) GetOptimalHistoryLimit() int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.windowSize * allocPctHistoryCompaction / 100
}

// ShouldCompact 判断当前 History token 数是否超过最优限制，需要触发压缩
func (a *TokenBudgetAllocator) ShouldCompact(currentHistoryTokens int) bool {
	return currentHistoryTokens > a.GetOptimalHistoryLimit()
}

// GetStats 返回预算分配器的统计信息
func (a *TokenBudgetAllocator) GetStats() BudgetAllocatorStats {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return BudgetAllocatorStats{
		WindowSize:          a.windowSize,
		CurrentAllocation:   a.allocations,
		TotalAllocations:    a.totalAllocations,
		CompactionTriggered: a.compactionTriggered,
	}
}

// Reset 重置分配器到默认状态，清除所有统计信息并恢复默认预算分配
func (a *TokenBudgetAllocator) Reset() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.allocations = BudgetAllocation{
		SystemPrompt: a.windowSize * allocPctSystemPrompt / 100,
		Tools:        a.windowSize * allocPctTools / 100,
		History:      a.windowSize * allocPctHistory / 100,
		Response:     a.windowSize * allocPctResponse / 100,
		Reserved:     a.windowSize * allocPctReserved / 100,
	}
	a.history = 0
	a.totalAllocations = 0
	a.compactionTriggered = 0
}
