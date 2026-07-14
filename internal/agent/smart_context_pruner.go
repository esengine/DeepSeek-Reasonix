package agent

import (
	"sort"
	"sync"

	"reasonix/internal/provider"
)

// ── OPT-99: SmartContextPruner (智能上下文裁剪器) ──
// 利用多种信号（消息年龄、重要性、相关性、冗余度）智能裁剪上下文，
// 将消息列表压缩到 token 预算内，同时保留对模型决策最关键的信息。
//
// 原理：
//   - ScorePruneCandidate 对每条消息计算 0-1 的裁剪分数（越高越应裁剪），
//     综合考虑：
//     · 消息年龄：越老越可能被裁剪
//     · 消息重要性：system/带工具调用的 assistant 消息保留
//     · 冗余度：重复内容的消息优先裁剪
//     · 近因性：最近 3 条消息保留
//   - PruneContext 按裁剪分数从高到低依次裁剪，直到满足 token 预算
//   - 系统消息和最近 3 条消息始终保留
//
// 效果：在保留关键上下文的前提下显著减少 token 消耗。

// PruningDecision 记录一次裁剪决策的详情。
type PruningDecision struct {
	MessageIndex int     // 被裁剪消息在原始列表中的索引
	Reason       string  // 裁剪原因
	TokensSaved  int     // 节省的 token 数
	Score        float64 // 裁剪分数（0-1，越高越应裁剪）
}

// SmartPrunerStats 智能裁剪器统计信息。
type SmartPrunerStats struct {
	TotalPruned     int // 累计裁剪的消息数
	TokensSaved     int // 累计节省的 token 数
	DecisionsCount  int // 累计裁剪决策数
}

// SmartContextPruner 智能上下文裁剪器。
type SmartContextPruner struct {
	mu               sync.RWMutex
	totalPruned      int
	tokensSaved      int
	pruningDecisions []PruningDecision
}

// NewSmartContextPruner 创建一个新的 SmartContextPruner 实例。
func NewSmartContextPruner() *SmartContextPruner {
	return &SmartContextPruner{}
}

// PruneContext 将消息列表裁剪到 token 预算内。
// 综合考虑消息年龄、重要性、冗余度和近因性，按裁剪分数从高到低
// 依次裁剪，直到总 token 数不超过 tokenBudget。
// 系统消息和最近 3 条消息始终保留。
// 返回裁剪后的消息列表和裁剪决策。
func (s *SmartContextPruner) PruneContext(messages []provider.Message, tokenBudget int) ([]provider.Message, []PruningDecision) {
	s.mu.Lock()
	defer s.mu.Unlock()

	total := len(messages)
	if total == 0 {
		return messages, nil
	}

	// 估算总 token 数
	totalTokens := 0
	msgTokens := make([]int, total)
	for i, msg := range messages {
		msgTokens[i] = estimateMessageTokens(msg)
		totalTokens += msgTokens[i]
	}

	// 在预算内，无需裁剪
	if totalTokens <= tokenBudget {
		return messages, nil
	}

	// 构建裁剪候选列表
	type candidate struct {
		index  int
		score  float64
		tokens int
	}

	// 冗余检测：记录已见内容
	contentSeen := make(map[string]int)

	candidates := make([]candidate, 0, total)
	for i, msg := range messages {
		age := total - i // 年龄：越靠前越大
		score := s.ScorePruneCandidate(msg, i, total, age)

		// 冗余检查：如果内容之前出现过，增加裁剪分数
		if msg.Content != "" {
			if contentSeen[msg.Content] > 0 {
				score += 0.3
				if score > 1.0 {
					score = 1.0
				}
			}
			contentSeen[msg.Content]++
		}

		candidates = append(candidates, candidate{
			index:  i,
			score:  score,
			tokens: msgTokens[i],
		})
	}

	// 按裁剪分数从高到低排序
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	// 依次裁剪直到满足预算
	prunedIndices := make(map[int]bool)
	currentTokens := totalTokens
	decisions := []PruningDecision{}

	for _, cand := range candidates {
		if currentTokens <= tokenBudget {
			break
		}

		msg := messages[cand.index]

		// 始终保留系统消息
		if msg.Role == provider.RoleSystem {
			continue
		}

		// 始终保留最近 3 条消息
		if cand.index >= total-3 {
			continue
		}

		prunedIndices[cand.index] = true
		currentTokens -= cand.tokens

		reason := "low importance"
		if cand.score > 0.7 {
			reason = "high prune score (old/redundant)"
		} else if cand.score > 0.4 {
			reason = "moderate prune score"
		}

		decisions = append(decisions, PruningDecision{
			MessageIndex: cand.index,
			Reason:       reason,
			TokensSaved:  cand.tokens,
			Score:        cand.score,
		})

		s.totalPruned++
		s.tokensSaved += cand.tokens
	}

	// 构建裁剪后的消息列表（保持原始顺序）
	result := make([]provider.Message, 0, total-len(prunedIndices))
	for i, msg := range messages {
		if !prunedIndices[i] {
			result = append(result, msg)
		}
	}

	s.pruningDecisions = append(s.pruningDecisions, decisions...)

	return result, decisions
}

// ScorePruneCandidate 对单条消息计算裁剪分数（0-1，越高越应裁剪）。
// 综合考虑消息年龄、重要性和近因性：
//   - 年龄：越老的消息分数越高
//   - 重要性：system 消息和带工具调用的 assistant 消息分数降低
//   - 近因性：最近 3 条消息分数降低
func (s *SmartContextPruner) ScorePruneCandidate(msg provider.Message, index int, total int, age int) float64 {
	score := 0.0

	// 近因性奖励：最近 3 条消息降低裁剪分数
	if total > 0 && index >= total-3 {
		score -= 0.4
	}

	// 重要性：系统消息始终保留
	if msg.Role == provider.RoleSystem {
		score -= 0.6
	}

	// 重要性：带工具调用的 assistant 消息应保留
	if msg.Role == provider.RoleAssistant && len(msg.ToolCalls) > 0 {
		score -= 0.3
	}

	// 重要性：工具结果消息应保留（与工具调用配对）
	if msg.Role == provider.RoleTool {
		score -= 0.2
	}

	// 年龄因素：越老的消息越可能被裁剪
	if total > 0 {
		ageFactor := float64(age) / float64(total)
		score += ageFactor * 0.4
	}

	// 限制在 [0, 1] 范围内
	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}

	return score
}

// GetStats 返回智能裁剪器的统计信息。
func (s *SmartContextPruner) GetStats() SmartPrunerStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return SmartPrunerStats{
		TotalPruned:    s.totalPruned,
		TokensSaved:    s.tokensSaved,
		DecisionsCount: len(s.pruningDecisions),
	}
}

// Reset 清除所有裁剪统计和历史决策。
func (s *SmartContextPruner) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.totalPruned = 0
	s.tokensSaved = 0
	s.pruningDecisions = nil
}

// estimateMessageTokens 估算单条消息的 token 数。
// 基于内容长度和工具调用参数的字符数估算（约 4 字符 = 1 token）。
func estimateMessageTokens(msg provider.Message) int {
	tokens := estimateTokens(msg.Content)
	for _, tc := range msg.ToolCalls {
		tokens += estimateTokens(tc.Name)
		tokens += estimateTokens(tc.Arguments)
	}
	if tokens == 0 {
		tokens = 1 // 每条消息至少 1 token
	}
	return tokens
}
