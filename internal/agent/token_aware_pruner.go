package agent

import "sync"

// ── OPT-132: TokenAwarePruner (Token 感知修剪器) ──
// 根据 token 预算修剪消息列表，支持三种修剪策略:
//   - "fifo": 先进先出，移除最早加入的消息
//   - "lowest-value": 最低价值优先，移除 Value 最小的消息
//   - "oldest": 最老消息优先，移除 Turn 最小的消息
//
// 原理：当消息列表的总 token 超过预算时，按策略逐条移除消息，
// 直到总 token 降至预算以内。移除顺序由策略决定，但保留消息保持原始顺序。

// PrunableMessage 表示一个可被修剪的消息条目。
type PrunableMessage struct {
	Content string
	Value   int
	Turn    int
}

// TokenAwarePruner Token 感知修剪器，根据 token 预算修剪消息列表。
type TokenAwarePruner struct {
	mu                 sync.RWMutex
	totalPruned        int
	totalTokensRemoved int
	totalMessagesKept  int
	pruningStrategy    string
}

// NewTokenAwarePruner 创建一个新的 Token 感知修剪器实例。
// strategy 可选值: "fifo" (先进先出), "lowest-value" (最低价值优先), "oldest" (最老消息优先)。
func NewTokenAwarePruner(strategy string) *TokenAwarePruner {
	return &TokenAwarePruner{
		pruningStrategy: strategy,
	}
}

// Prune 修剪消息列表以使其总 token 适应预算。
// 按策略移除消息，直到总 token 在预算内。保留的消息保持原始顺序。
func (p *TokenAwarePruner) Prune(messages []PrunableMessage, budget int) []PrunableMessage {
	p.mu.Lock()
	defer p.mu.Unlock()

	n := len(messages)
	if n == 0 {
		p.totalPruned++
		return []PrunableMessage{}
	}

	// Calculate total tokens
	totalTokens := 0
	for _, m := range messages {
		totalTokens += tap2EstimateTokens(m.Content)
	}

	// If within budget, keep all
	if totalTokens <= budget {
		p.totalPruned++
		p.totalMessagesKept += n
		result := make([]PrunableMessage, n)
		copy(result, messages)
		return result
	}

	// Determine removal order based on strategy
	removalOrder := tap2RemovalOrder(messages, p.pruningStrategy)

	// Remove messages until within budget
	removed := make([]bool, n)
	tokensRemoved := 0
	removedCount := 0
	for _, idx := range removalOrder {
		if totalTokens-tokensRemoved <= budget {
			break
		}
		msgTokens := tap2EstimateTokens(messages[idx].Content)
		tokensRemoved += msgTokens
		removed[idx] = true
		removedCount++
	}

	// Build result from non-removed messages in original order
	kept := 0
	result := make([]PrunableMessage, 0, n-removedCount)
	for i, m := range messages {
		if !removed[i] {
			result = append(result, m)
			kept++
		}
	}

	p.totalPruned++
	p.totalTokensRemoved += tokensRemoved
	p.totalMessagesKept += kept

	return result
}

// EstimateTokens 估算消息的 token 数量，采用 len(message)/4 的近似算法。
func (p *TokenAwarePruner) EstimateTokens(message string) int {
	return tap2EstimateTokens(message)
}

// GetStats 返回修剪器的统计信息。
func (p *TokenAwarePruner) GetStats() map[string]interface{} {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return map[string]interface{}{
		"totalPruned":        p.totalPruned,
		"totalTokensRemoved": p.totalTokensRemoved,
		"totalMessagesKept":  p.totalMessagesKept,
		"strategy":           p.pruningStrategy,
	}
}

// Reset 重置修剪器的所有统计数据。
func (p *TokenAwarePruner) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.totalPruned = 0
	p.totalTokensRemoved = 0
	p.totalMessagesKept = 0
}

// ---------------------------------------------------------------------------
// 辅助函数 (tap2 前缀，避免与 tap 冲突)
// ---------------------------------------------------------------------------

// tap2EstimateTokens 估算消息的 token 数量: len(message) / 4。
func tap2EstimateTokens(message string) int {
	return len(message) / 4
}

// tap2RemovalOrder 根据修剪策略返回消息索引的移除顺序。
// "fifo": 按原始顺序移除 (索引 0, 1, 2, ...)。
// "lowest-value": 按 Value 升序移除 (最低价值优先)。
// "oldest": 按 Turn 升序移除 (最老消息优先)。
// 使用选择排序实现，避免引入 sort 包。
func tap2RemovalOrder(messages []PrunableMessage, strategy string) []int {
	n := len(messages)
	indices := make([]int, n)
	for i := range indices {
		indices[i] = i
	}

	switch strategy {
	case "lowest-value":
		// Selection sort by Value ascending
		for i := 0; i < n-1; i++ {
			minIdx := i
			for j := i + 1; j < n; j++ {
				if messages[indices[j]].Value < messages[indices[minIdx]].Value {
					minIdx = j
				}
			}
			indices[i], indices[minIdx] = indices[minIdx], indices[i]
		}
	case "oldest":
		// Selection sort by Turn ascending
		for i := 0; i < n-1; i++ {
			minIdx := i
			for j := i + 1; j < n; j++ {
				if messages[indices[j]].Turn < messages[indices[minIdx]].Turn {
					minIdx = j
				}
			}
			indices[i], indices[minIdx] = indices[minIdx], indices[i]
		}
	default:
		// "fifo": keep original order (index 0, 1, 2, ...)
	}

	return indices
}
