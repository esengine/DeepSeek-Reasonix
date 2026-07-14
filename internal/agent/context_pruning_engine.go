package agent

import "sync"

// ── OPT-158: ContextPruningEngine (上下文修剪引擎 / Context Pruning Engine) ──
// 基于多层策略修剪上下文消息，使总 token 数不超过预算上限。
// 支持多种修剪策略：低相关性、冗余、过时、超大消息等，
// 按策略依次修剪消息直到总 token 数降至预算范围内。
//
// 原理：LLM 上下文窗口有限，当对话历史过长时需要修剪。
// 不同的修剪策略针对不同类型的无用消息：低相关性消息对当前任务帮助不大，
// 冗余消息重复了已有信息，过时消息已不具参考价值，超大消息消耗过多 token。
//
// 效果：在保持上下文核心信息的前提下减少 token 消耗，
// 统计修剪的消息数量和节省的 token 数，为上下文管理提供反馈。

// ContextPruningEngine 上下文修剪引擎
type ContextPruningEngine struct {
	mu              sync.RWMutex
	maxTokens       int       // 最大 token 预算
	pruneStrategies []string  // 修剪策略列表
	prunedCount     int       // 已修剪的消息数
	tokensSaved     int       // 节省的 token 数
}

// NewContextPruningEngine 创建上下文修剪引擎。
// maxTokens 指定最大 token 预算，若 <= 0 则默认 8192。
// 默认修剪策略为 low_relevance、redundant、outdated、oversized。
func NewContextPruningEngine(maxTokens int) *ContextPruningEngine {
	if maxTokens <= 0 {
		maxTokens = 8192
	}
	return &ContextPruningEngine{
		maxTokens:       maxTokens,
		pruneStrategies: []string{"low_relevance", "redundant", "outdated", "oversized"},
	}
}

// Prune 按策略修剪消息列表，直到总 token 数不超过 maxTokens。
// messages 为待修剪的消息列表，estimatedTokens 为消息列表的预估总 token 数（若 <= 0 则自动估算）。
// 返回修剪后的消息列表。修剪时循环应用各策略，每次移除一条消息，直到总 token 数降至预算范围内。
func (e *ContextPruningEngine) Prune(messages []string, estimatedTokens int) []string {
	e.mu.Lock()
	defer e.mu.Unlock()

	if estimatedTokens <= 0 {
		estimatedTokens = cpeEstimateTokens(messages)
	}

	if estimatedTokens <= e.maxTokens {
		return messages
	}

	result, pruned, saved := cpePruneByStrategy(messages, estimatedTokens, e.maxTokens, e.pruneStrategies)
	e.prunedCount += pruned
	e.tokensSaved += saved
	return result
}

// AddStrategy 添加一个修剪策略。
// strategy 为策略名称，若已存在则不重复添加。
func (e *ContextPruningEngine) AddStrategy(strategy string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, s := range e.pruneStrategies {
		if s == strategy {
			return
		}
	}
	e.pruneStrategies = append(e.pruneStrategies, strategy)
}

// GetStrategies 获取当前配置的修剪策略列表。
// 返回策略列表的副本，避免外部修改影响内部状态。
func (e *ContextPruningEngine) GetStrategies() []string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	result := make([]string, len(e.pruneStrategies))
	copy(result, e.pruneStrategies)
	return result
}

// GetStats 返回修剪引擎的统计信息。
// 包含 maxTokens（最大 token 预算）、strategyCount（策略数）、
// prunedCount（已修剪消息数）和 tokensSaved（节省的 token 数）。
func (e *ContextPruningEngine) GetStats() map[string]interface{} {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return map[string]interface{}{
		"maxTokens":     e.maxTokens,
		"strategyCount": len(e.pruneStrategies),
		"prunedCount":   e.prunedCount,
		"tokensSaved":   e.tokensSaved,
	}
}

// Reset 重置修剪引擎的统计信息（不重置策略列表和 maxTokens）。
func (e *ContextPruningEngine) Reset() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.prunedCount = 0
	e.tokensSaved = 0
}

// cpeEstimateTokens 估算消息列表的总 token 数，采用字符长度 / 4 的简单估算方式。
func cpeEstimateTokens(messages []string) int {
	total := 0
	for _, msg := range messages {
		total += len(msg) / 4
	}
	return total
}

// cpePruneByStrategy 按策略修剪消息列表。
// 循环应用各策略选择要移除的消息，直到总 token 数 <= maxTokens 或仅剩一条消息。
// 策略说明：
//   - oversized: 移除最长的消息（消耗 token 最多）
//   - low_relevance / outdated: 移除最早的消息（头部，优先级最低）
//   - redundant: 移除最短的消息（可能是冗余的）
//
// 返回修剪后的消息列表、被修剪的消息数和节省的 token 数。
func cpePruneByStrategy(messages []string, estimatedTokens, maxTokens int, strategies []string) ([]string, int, int) {
	pruned := 0
	saved := 0
	result := make([]string, len(messages))
	copy(result, messages)

	for estimatedTokens > maxTokens && len(result) > 1 {
		// 按策略选择要移除的消息索引，循环使用各策略
		removeIdx := 0
		if len(strategies) > 0 {
			strategy := strategies[pruned%len(strategies)]
			switch strategy {
			case "oversized":
				// 移除最长的消息
				maxLen := 0
				for i, msg := range result {
					if len(msg) > maxLen {
						maxLen = len(msg)
						removeIdx = i
					}
				}
			case "redundant":
				// 移除最短的消息（可能是冗余的）
				minLen := len(result[0])
				for i, msg := range result {
					if len(msg) < minLen {
						minLen = len(msg)
						removeIdx = i
					}
				}
			case "low_relevance", "outdated":
				// 移除最早的消息（头部）
				removeIdx = 0
			default:
				removeIdx = 0
			}
		}

		removed := result[removeIdx]
		result = append(result[:removeIdx], result[removeIdx+1:]...)
		tokenRemoved := len(removed) / 4
		estimatedTokens -= tokenRemoved
		saved += tokenRemoved
		pruned++
	}

	return result, pruned, saved
}
